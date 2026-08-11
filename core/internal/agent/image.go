package agent

// Shrinking the picture a slot is filled from, and growing the answer back.
//
// A vision model reads a screenshot as a grid of patches, so what it spends on
// one is set by the picture's pixels rather than its bytes: re-encoding the
// same grid smaller — PNG to JPEG — changes the transfer and nothing the model
// does. Fewer pixels is the only thing that makes it cheaper, and it is a trade,
// since the coordinate a slot is usually asked for is read off the pixels that
// were thrown away. Where the trade stops being worth taking is measured in
// config.DefaultImageScale.
//
// Pob shrinks the picture here rather than asking the shell for a smaller one.
// The shells do take a maxWidth — it is what the view page's frames use — but
// the width to ask for is a fraction of one Pob does not know until a frame
// comes back, and a scale is one setting for three shells instead of three.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"
	"strings"
)

// shrinkPNG scales a PNG down by scale and returns it re-encoded, with the
// size it came out at. A scale of 1 or more is returned untouched: this is only
// ever asked to make a picture smaller.
func shrinkPNG(data []byte, scale float64) ([]byte, int, int, error) {
	if scale >= 1 {
		return data, 0, 0, nil
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	w := max(1, int(math.Round(float64(b.Dx())*scale)))
	h := max(1, int(math.Round(float64(b.Dy())*scale)))
	if w >= b.Dx() || h >= b.Dy() {
		return data, 0, 0, nil
	}

	dst := boxDownscale(src, w, h)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), w, h, nil
}

// boxDownscale averages each destination pixel over the source pixels it
// covers. It is the plain answer to shrinking and the right one here: a
// screenshot is text and one-pixel rules, and picking every nth pixel instead
// drops half the strokes and aliases the rest into noise the model then reads.
func boxDownscale(src image.Image, w, h int) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0 := b.Min.Y + y*b.Dy()/h
		y1 := max(y0+1, b.Min.Y+(y+1)*b.Dy()/h)
		for x := 0; x < w; x++ {
			x0 := b.Min.X + x*b.Dx()/w
			x1 := max(x0+1, b.Min.X+(x+1)*b.Dx()/w)
			var r, g, bl, a uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					sr, sg, sb, sa := src.At(sx, sy).RGBA()
					r += uint64(sr)
					g += uint64(sg)
					bl += uint64(sb)
					a += uint64(sa)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			i := dst.PixOffset(x, y)
			dst.Pix[i+0] = uint8(r / n >> 8)
			dst.Pix[i+1] = uint8(g / n >> 8)
			dst.Pix[i+2] = uint8(bl / n >> 8)
			dst.Pix[i+3] = uint8(a / n >> 8)
		}
	}
	return dst
}

// scaledCalls are the statements whose numbers are measured on the picture the
// model was shown, and so the ones an answer has to be scaled back out of. A
// distance across a half-size picture and a position on one are both half of
// what they mean on the screen, so the relative calls and the absolute ones
// scale by the same factor and are in here together.
//
// Every other call is left alone because its numbers are not pixels: sleep is a
// length of time, typeText and keyPress are text. scroll is the near miss — the
// macro vocabulary describes it in the same breath as move — and it is left out
// on purpose: what a shell records there is a wheel delta, which is not a
// distance across the image and does not scale with one.
var scaledCalls = map[string]bool{
	"move":           true,
	"moveTo":         true,
	"drag":           true,
	"dragTo":         true,
	"click":          true,
	"rightClick":     true,
	"doubleClick":    true,
	"takeScreenshot": true,
}

// scaleMacroCoordinates makes the temporary copy of a macro sent beside a
// shrunken screenshot use that screenshot's pixel grid too. The real source is
// never changed: after psl fills the slot, restoreFilledSurroundings puts its
// answer back between the original source text before Pob grows that answer.
//
// Only existing, plain numeric arguments of calls measured on the image are
// changed. Numbers in slot instructions, strings and comments are prose rather
// than coordinates; loop counts, times and scroll deltas use other units.
func scaleMacroCoordinates(source string, scale float64) string {
	if scale >= 1 || scale <= 0 {
		return source
	}

	lines := strings.Split(source, "\n")
	inBlock := false
	for i, line := range lines {
		var comments [][2]int
		comments, inBlock = commentSpans(line, inBlock)
		lines[i] = scaleLineCoordinates(line, comments, scale)
	}
	return strings.Join(lines, "\n")
}

// scaleLineCoordinates rewrites the numeric arguments of one coordinate call
// without disturbing its whitespace, slots or comments. Comment bytes are
// blanked only in a same-length copy used to find the call and its arguments,
// which keeps every replacement offset valid in the original line.
func scaleLineCoordinates(line string, comments [][2]int, scale float64) string {
	codeBytes := []byte(line)
	for _, span := range comments {
		for i := span[0]; i < span[1]; i++ {
			codeBytes[i] = ' '
		}
	}
	code := string(codeBytes)
	start := len(code) - len(strings.TrimLeft(code, " \t"))
	end := len(strings.TrimRight(code, " \t"))
	if start >= end {
		return line
	}

	statement := code[start:end]
	name, _, ok := macroArguments(statement)
	if !ok || !scaledCalls[name] {
		return line
	}
	open := strings.IndexByte(statement, '(')
	if open < 0 {
		return line
	}
	argsStart := start + open + 1
	argsEnd := end - 1 // macroArguments established that the last byte is ')'.
	if argsStart >= argsEnd {
		return line
	}

	type replacement struct {
		start, end int
		text       string
	}
	var replacements []replacement
	for _, span := range macroArgumentSpans(code[argsStart:argsEnd]) {
		raw := code[argsStart+span[0] : argsStart+span[1]]
		number := strings.TrimSpace(raw)
		n, err := strconv.ParseFloat(number, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			continue
		}
		offset := strings.Index(raw, number)
		replacements = append(replacements, replacement{
			start: argsStart + span[0] + offset,
			end:   argsStart + span[0] + offset + len(number),
			text:  strconv.Itoa(int(math.Round(n * scale))),
		})
	}

	// Right to left keeps offsets found in the original line valid when a
	// replacement has fewer digits than the full-size coordinate it replaces.
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		line = line[:r.start] + r.text + line[r.end:]
	}
	return line
}

// macroArgumentSpans is splitMacroArgs with offsets retained. Commas inside a
// slot instruction or quoted string belong to that value rather than to the
// surrounding call, so both are stepped over whole.
func macroArgumentSpans(args string) [][2]int {
	if strings.TrimSpace(args) == "" {
		return nil
	}
	slots := slotStarts(args)
	var spans [][2]int
	start, i := 0, 0
	for i < len(args) {
		if args[i] == '"' {
			i = endOfString(args, i)
			continue
		}
		if end, ok := slots[i]; ok {
			i = end
			continue
		}
		if args[i] == ',' {
			spans = append(spans, [2]int{start, i})
			start, i = i+1, i+1
			continue
		}
		i++
	}
	return append(spans, [2]int{start, len(args)})
}

// restoreFilledSurroundings takes the answer out of the scaled, model-facing
// statement and puts it between the text surrounding the slot in the real
// statement. Existing coordinates therefore remain byte-for-byte as written;
// only the answer goes on to rescaleFilled.
func restoreFilledSurroundings(statement string, start, end int, modelStatement string,
	modelStart, modelEnd int, filled string) (string, bool) {
	if start < 0 || end > len(statement) || start > end ||
		modelStart < 0 || modelEnd > len(modelStatement) || modelStart > modelEnd {
		return filled, false
	}
	modelPrefix, modelSuffix := modelStatement[:modelStart], modelStatement[modelEnd:]
	if !strings.HasPrefix(filled, modelPrefix) || !strings.HasSuffix(filled, modelSuffix) ||
		len(filled) < len(modelPrefix)+len(modelSuffix) {
		return filled, false
	}
	answer := filled[len(modelPrefix) : len(filled)-len(modelSuffix)]
	return statement[:start] + answer + statement[end:], true
}

// rescaleFilled turns a statement filled from a shrunken picture back into one
// written in screen pixels.
//
// Only what the model wrote is touched in the real macro. A statement can hold
// a recorded number beside an asked-for one — move(40, :: … ::) — and although
// the temporary model copy showed that number scaled, its original text has
// already been restored by restoreFilledSurroundings. The answer is cut back
// out by the text around the slot rather than the whole line being re-read.
//
// Anything that does not fit that shape comes back unchanged: a filled line
// whose surroundings moved is one psl rewrote further than the slot, and a
// guess at which of its numbers were the model's would be a click somewhere
// nobody asked for.
func rescaleFilled(statement string, start, end int, filled string, scale float64) (string, bool) {
	if scale >= 1 || scale <= 0 || start < 0 || end > len(statement) || start > end {
		return filled, false
	}
	// A slot that is the whole line filled to statements rather than to a value,
	// so there is no statement around it to read a call name off and nothing to
	// cut the answer back out of. What came back is read as the lines it is.
	if statementSlot(stripLine(statement)) {
		return rescaleBlock(filled, scale)
	}
	if !scaledCalls[callName(statement)] {
		return filled, false
	}
	prefix, suffix := statement[:start], statement[end:]
	if !strings.HasPrefix(filled, prefix) || !strings.HasSuffix(filled, suffix) ||
		len(filled) < len(prefix)+len(suffix) {
		return filled, false
	}
	answer := filled[len(prefix) : len(filled)-len(suffix)]
	scaled, ok := rescaleNumbers(answer, scale)
	if !ok {
		return filled, false
	}
	return prefix + scaled + suffix, true
}

// rescaleBlock grows the numbers in a block a statement slot filled to back to
// the picture's full size — a statement at a time, since a block is statements
// and each says for itself whether its numbers were measured on the picture.
//
// A line that is not one of those is handed back exactly as it came: a sleep is
// a time, a typeText is text, a block header holds no numbers of its own, and a
// statement the model left a slot in has not been answered yet — that slot is
// the generated file's own, filled off a screenshot of its own and scaled then.
func rescaleBlock(filled string, scale float64) (string, bool) {
	lines := strings.Split(filled, "\n")
	grown := false
	for i, line := range lines {
		scaled, ok := rescaleStatement(line, scale)
		if !ok {
			continue
		}
		lines[i], grown = scaled, true
	}
	if !grown {
		return filled, false
	}
	return strings.Join(lines, "\n"), true
}

// rescaleStatement grows one generated statement's numbers back. Only what is
// between the parentheses is rewritten, so the indentation the model wrote and
// any note it put on the end of the line are still there afterwards.
func rescaleStatement(line string, scale float64) (string, bool) {
	code, comment := splitComment(line)
	name, args, ok := macroArguments(strings.TrimSpace(code))
	if !ok || !scaledCalls[name] || len(args) == 0 {
		return line, false
	}
	scaled, ok := rescaleNumbers(strings.Join(args, ","), scale)
	if !ok {
		return line, false
	}
	indent := code[:len(code)-len(strings.TrimLeft(code, " \t"))]
	return indent + name + "(" + scaled + ")" + comment, true
}

// splitComment cuts a line into the code on it and whatever comment follows, so
// the numbers can be rewritten without disturbing the note beside them. The two
// join back into the line they came from.
//
// A comment anywhere but the end — `click(/* here */ 1, 2)` — leaves the code
// half unreadable as a call, which is a line rescaleStatement then hands back
// untouched. That is the right way round: a number nobody could place is a
// number to leave alone rather than to guess at.
func splitComment(line string) (code, comment string) {
	spans, _ := commentSpans(line, false)
	if len(spans) == 0 {
		return line, ""
	}
	code = strings.TrimRight(line[:spans[0][0]], " \t")
	return code, line[len(code):]
}

// rescaleNumbers grows every number in a comma-separated answer back to the
// picture's full size. It fails rather than half-converting: an answer with a
// word in it is not a coordinate, and one number of a pair scaled without the
// other is worse than neither.
func rescaleNumbers(answer string, scale float64) (string, bool) {
	parts := strings.Split(answer, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return "", false
		}
		out[i] = strconv.Itoa(int(math.Round(n / scale)))
	}
	return strings.Join(out, ", "), true
}

// callName reads the name off the front of a statement — `move` out of
// `move(40, :: … ::)`. An if header has no call in it and comes back as the
// keyword, which is in no table of calls.
func callName(statement string) string {
	s := strings.TrimSpace(statement)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// scaleNote is what the log says about a picture that was shrunk before the
// model saw it, so a coordinate that came back wrong can be read against the
// size it was read off.
func scaleNote(w, h int, scale float64) string {
	return fmt.Sprintf("screenshot scaled to %d×%d for the model (image_scale %g)", w, h, scale)
}

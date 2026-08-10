package agent

// Shrinking the picture a slot is filled from, and growing the answer back.
//
// A vision model reads a screenshot as a grid of patches, so what it spends on
// one is set by the picture's pixels rather than its bytes: re-encoding the
// same grid smaller — PNG to JPEG — changes the transfer and nothing the model
// does. Fewer pixels is the only thing that makes it cheaper, and it is a trade
// rather than a saving, since the coordinate a slot is usually asked for is
// read off the pixels that were thrown away. That is why image_scale is 1 until
// someone sets it: see config.ImageScale.
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

// scaledCalls are the statements whose numbers are distances on the picture the
// model was shown, and so the ones an answer has to be scaled back out of.
//
// Every other call is left alone because its numbers are not pixels: sleep is
// milliseconds, typeText and keyPress are text. scroll is the near miss — the
// macro vocabulary describes it in the same breath as move — and it is left out
// on purpose: what a shell records there is a wheel delta, which is not a
// distance across the image and does not scale with one.
var scaledCalls = map[string]bool{
	"move":            true,
	"drag":            true,
	"take_screenshot": true,
}

// rescaleFilled turns a statement filled from a shrunken picture back into one
// written in screen pixels.
//
// Only what the model wrote is touched. A statement can hold a recorded number
// beside an asked-for one — move(40, :: … ::) — and the recorded one was never
// in the model's coordinates to begin with, so the answer is cut back out of
// the filled line by the text that surrounded the slot rather than the line
// being re-read as a call.
//
// Anything that does not fit that shape comes back unchanged: a filled line
// whose surroundings moved is one psl rewrote further than the slot, and a
// guess at which of its numbers were the model's would be a click somewhere
// nobody asked for.
func rescaleFilled(statement string, start, end int, filled string, scale float64) (string, bool) {
	if scale >= 1 || scale <= 0 || start < 0 || end > len(statement) || start > end {
		return filled, false
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

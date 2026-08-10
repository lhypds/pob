package agent

import (
	"strings"

	"pob/core/internal/psl"
)

// Comments in a macro are C's, because a macro is read by people who have read
// code before: `//` runs to the end of the line, and `/* … */` runs until it is
// closed, however many lines that takes.
//
// They are taken out of the line before it is parsed and never out of the file.
// Every statement is found by its line number — that is how an answer goes back
// where it came from, and how a loop puts its statements back — so a comment
// costs the line it is on its meaning and never its place. A file of forty lines
// stays a file of forty lines however much of it is commented out.
//
// The text stays for another reason too. psl is handed the file whole, and what
// a comment says about the screen is the sort of thing a model filling a slot
// two lines down has no other way of knowing.

const (
	macroLineComment = "//"
	macroBlockOpen   = "/*"
	macroBlockClose  = "*/"
)

// commentSpans returns the ranges of a line that are comment, given whether a
// block comment was open when the line began, and says whether one is still open
// when it ends.
//
// A `//` or a `/*` is only the start of a comment where it is not something else
// already. Two of those: a double-quoted string, since `typeText("http://x")`
// types a URL, and a `:: … ::` slot, since an instruction is a sentence and may
// well have a `//` in it. Both are skipped whole, and whichever begins first is
// the one that swallows the other — a slot inside a string is passed over with
// the string, and a quote inside an instruction with the slot.
func commentSpans(line string, inBlock bool) ([][2]int, bool) {
	var spans [][2]int
	slots := slotStarts(line)
	start, i := 0, 0

	for i < len(line) {
		if inBlock {
			j := strings.Index(line[i:], macroBlockClose)
			if j < 0 {
				break
			}
			i += j + len(macroBlockClose)
			spans = append(spans, [2]int{start, i})
			inBlock = false
			continue
		}

		if line[i] == '"' {
			i = endOfString(line, i)
			continue
		}
		if end, ok := slots[i]; ok {
			i = end
			continue
		}
		if strings.HasPrefix(line[i:], macroLineComment) {
			return append(spans, [2]int{i, len(line)}), false
		}
		if strings.HasPrefix(line[i:], macroBlockOpen) {
			start, i = i, i+len(macroBlockOpen)
			inBlock = true
			continue
		}
		i++
	}

	// A block comment nothing closed on this line takes the rest of it, and goes
	// on taking the lines under it until something does.
	if inBlock {
		spans = append(spans, [2]int{start, len(line)})
	}
	return spans, inBlock
}

// slotStarts maps the offset each slot in the line opens at to the offset just
// past its close, so the scan can step over a slot in one move. Read by psl's own
// rule, which is what makes "not a comment because it is inside a slot" mean the
// same thing to both.
func slotStarts(line string) map[int]int {
	starts := map[int]int{}
	for i := 0; ; {
		slot, found := psl.FindSlot(line, i)
		if !found {
			return starts
		}
		starts[slot.Start] = slot.End
		i = slot.End
	}
}

// endOfString returns the offset just past the double-quoted string opening at
// i, or the end of the line when nothing closes it. A backslash escapes whatever
// follows it, which is how a quote gets inside one.
func endOfString(line string, i int) int {
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return len(line)
}

// stripComments returns what is left of a line once its comments are taken out,
// and whether a block comment is open at the end of it.
func stripComments(line string, inBlock bool) (string, bool) {
	spans, stillInBlock := commentSpans(line, inBlock)
	for i := len(spans) - 1; i >= 0; i-- {
		line = line[:spans[i][0]] + line[spans[i][1]:]
	}
	return line, stillInBlock
}

// stripLine takes the comments out of a line known to begin outside a block
// comment — a statement psl has just filled, which is a line the parse read a
// statement out of and so a line no open block comment reached.
func stripLine(line string) string {
	code, _ := stripComments(line, false)
	return code
}

// codeLines is the file as the parse reads it: the same lines, each holding what
// is left of it once the comments are out.
func codeLines(text string) []string {
	lines := strings.Split(text, "\n")
	inBlock := false
	for n, line := range lines {
		lines[n], inBlock = stripComments(line, inBlock)
	}
	return lines
}

// neutralizeComments writes the slot markers in a file's comments out of it —
// `:: x ::` becomes `<x>` — and hands back the file otherwise as it was.
//
// psl is given the file whole and fills the first slot in it, and a comment is
// the one part of a file that is never a statement waiting on an answer. Left
// alone, a commented-out `move(:: the x offset ::, 0)` is what psl reaches for
// first, in place of the statement the replay is actually standing on.
//
// The half-written marker is the worse of the two and the reason this runs over
// the file rather than over the lines the parse dropped. `// TODO :: fix this`
// holds no slot of its own — nothing on that line closes it — but psl does not
// read a file a line at a time: the next real `::` below closes it, and what gets
// filled is the span from the middle of a comment down into a live statement.
// Neutralize takes every marker, paired or not, which is exactly what that needs.
func neutralizeComments(text string) string {
	lines := strings.Split(text, "\n")
	inBlock := false
	for n, line := range lines {
		var spans [][2]int
		spans, inBlock = commentSpans(line, inBlock)
		// Right to left: neutralizing changes the length of what it replaces, and
		// the spans left of it are the ones still to do.
		for i := len(spans) - 1; i >= 0; i-- {
			start, end := spans[i][0], spans[i][1]
			line = line[:start] + psl.Neutralize(line[start:end]) + line[end:]
		}
		lines[n] = line
	}
	return strings.Join(lines, "\n")
}

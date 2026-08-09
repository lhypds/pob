// Package psl finds the AI slots in a macro and has the psl compiler fill them.
//
// Pob does not talk to a model itself. A `:: instruction ::` is filled by
// running psl — the Prompt Script Language compiler — over the macro, so the
// API keys, the model names and the prompting live in one place, psl's .pslrc,
// rather than being configured once here and once there.
package psl

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Marker opens and closes a slot.
const Marker = "::"

// Two rules for the same markers, and the difference between them is the whole
// of what this file is for.
//
// psl is a language that lives inside files written in other languages, so it
// only reads `::` as a delimiter where it cannot be that language's own syntax:
// the opening `::` must be followed by whitespace and the closing one preceded
// by it, and neither may be glued to an identifier. That is what keeps C++'s
// std::cout from being a slot.
//
// A macro is not another language — it is PSL all the way down — so Pob is
// easier to write in: `::x::` and `:: x ::` are the same slot, and the spaces
// are yours to leave out. The identifier guards stay, since they are what keeps
// a `::` in text being typed from being read as a question.
//
// Every slot psl would fill is therefore a slot Pob sees, but not the other way
// round. Prepare is what closes that gap when a macro is handed over.

// Slot is one `:: instruction ::` found in a macro.
type Slot struct {
	Start       int    // byte offset of the opening "::"
	End         int    // byte offset just past the closing "::"
	Instruction string // what is written between the markers, trimmed
}

// FindSlot returns the first slot at or after `from`, by Pob's rule: the
// markers may be closed up or spaced out.
func FindSlot(src string, from int) (Slot, bool) { return findSlot(src, from, false) }

// HasSlot reports whether the text holds a slot Pob would have filled.
func HasSlot(src string) bool {
	_, found := FindSlot(src, 0)
	return found
}

// FindCompilerSlot returns the first slot psl itself would fill — the stricter
// rule, and the one that decides what a run of the compiler actually does.
// Prepare and Neutralize are written so that exactly one slot in a file passes
// it.
func FindCompilerSlot(src string, from int) (Slot, bool) { return findSlot(src, from, true) }

// CompilerHasSlot reports whether psl would find anything to fill here.
func CompilerHasSlot(src string) bool {
	_, found := FindCompilerSlot(src, 0)
	return found
}

// Prepare writes a statement as the compiler has to see it: the first slot
// spaced out into the form psl fills, and every slot after it closed up so psl
// passes over it. Together with Neutralize on every other line, that leaves the
// slot the replay is waiting on as the only one psl will touch.
func Prepare(src string) string {
	slot, found := FindSlot(src, 0)
	if !found {
		return Neutralize(src)
	}
	return src[:slot.Start] +
		Marker + " " + slot.Instruction + " " + Marker +
		Neutralize(src[slot.End:])
}

// Neutralize closes up every slot in the text — `:: x ::` becomes `::x::` —
// which psl passes over, since the markers are then glued to the instruction on
// both sides. Pob still reads it as a slot; this is about what the compiler
// does, not what a macro means.
func Neutralize(src string) string {
	var b strings.Builder
	rest := src
	for {
		slot, found := FindSlot(rest, 0)
		if !found {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:slot.Start])
		b.WriteString(Marker + slot.Instruction + Marker)
		rest = rest[slot.End:]
	}
}

func findSlot(src string, from int, strict bool) (Slot, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		if !opensSlot(src, i, strict) {
			continue
		}
		end, ok := findClose(src, i+2, strict)
		if !ok {
			continue
		}
		instruction := strings.TrimSpace(src[i+2 : end])
		if instruction == "" {
			// Nothing to ask. psl calls an empty slot an error rather than
			// passing over it, so it is skipped here before it gets that far.
			i = end + 1
			continue
		}
		return Slot{Start: i, End: end + len(Marker), Instruction: instruction}, true
	}
	return Slot{}, false
}

// opensSlot reports whether the "::" at i can start a slot. Glued to an
// identifier on its left it never can — that is `std::cout`, or a `::` in the
// middle of a word being typed. Under the compiler's rule it must also be
// followed by whitespace.
func opensSlot(src string, i int, strict bool) bool {
	if strict {
		next, size := utf8.DecodeRuneInString(src[i+2:])
		if size == 0 || !unicode.IsSpace(next) {
			return false
		}
	}
	prev, size := utf8.DecodeLastRuneInString(src[:i])
	if size == 0 {
		return true
	}
	return !isIdentRune(prev)
}

// findClose returns the offset of the "::" that closes a slot opened at from.
func findClose(src string, from int, strict bool) (int, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		if strict {
			prev, size := utf8.DecodeLastRuneInString(src[:i])
			if size == 0 || !unicode.IsSpace(prev) {
				continue
			}
		}
		next, size := utf8.DecodeRuneInString(src[i+2:])
		if size != 0 && isIdentRune(next) {
			continue
		}
		return i, true
	}
	return 0, false
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

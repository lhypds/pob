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

// The rules below mirror psl's own, in psl/internal/slot. They have to: Pob
// decides which statements hold a slot and which one psl will fill next, and a
// disagreement between the two would show up as a statement Pob waited on and
// psl never touched.
//
// PSL lives inside files written in other languages, so the delimiters only
// count where they cannot be that language's own syntax: the opening `::` is
// followed by whitespace and not glued to an identifier on its left, and the
// closing `::` is preceded by whitespace and not glued to an identifier on its
// right. That is what keeps C++'s std::cout from being a slot — and, in a
// macro, what tells `:: the x offset ::` apart from a `::` that is part of the
// text being typed.

// Slot is one `:: instruction ::` found in a macro.
type Slot struct {
	Start       int    // byte offset of the opening "::"
	End         int    // byte offset just past the closing "::"
	Instruction string // what is written between the markers, trimmed
}

// FindSlot returns the first slot at or after `from`.
func FindSlot(src string, from int) (Slot, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		if !opensSlot(src, i) {
			continue
		}
		end, ok := findClose(src, i+2)
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

// HasSlot reports whether the text holds a slot for psl to fill.
func HasSlot(src string) bool {
	_, found := FindSlot(src, 0)
	return found
}

// Neutralize closes up every slot in the text — `:: x ::` becomes `::x::` —
// which is inert to psl and to FindSlot above, since the markers are then glued
// to the instruction on both sides.
//
// It is how a macro is shown to psl with one slot live: everything psl must
// leave alone this run is closed up first, so the slot it finds is the slot the
// replay is waiting on rather than whichever comes first in the file.
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

func opensSlot(src string, i int) bool {
	next, size := utf8.DecodeRuneInString(src[i+2:])
	if size == 0 || !unicode.IsSpace(next) {
		return false
	}
	prev, size := utf8.DecodeLastRuneInString(src[:i])
	if size == 0 {
		return true
	}
	return !isIdentRune(prev)
}

func findClose(src string, from int) (int, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
			continue
		}
		prev, size := utf8.DecodeLastRuneInString(src[:i])
		if size == 0 || !unicode.IsSpace(prev) {
			continue
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

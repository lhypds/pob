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

// Pob and psl read the markers by one and the same rule, and that they do is
// what this file is written to keep true.
//
// The rule: `::` delimits a slot wherever it cannot be the surrounding
// language's own syntax. Scope resolution — C++'s std::cout, Rust's Foo::Bar,
// PHP's self::method — always glues `::` to an identifier, so the opening `::`
// may not be glued to one on its left, nor the closing `::` to one on its
// right. The spaces inside are optional: `::x::` and `:: x ::` are the same
// slot.
//
// The macro goes over to psl whole and as written, and psl fills the first slot
// in it. Which slot that is, Pob has to know exactly: it replays the file top to
// bottom, puts each answer back into it, and writes out the slots of statements
// that will never run — so that the first slot left is always the one the replay
// is waiting on. A slot Pob reads differently from psl is a slot it counts wrong
// somewhere in that. The two rules being the same rule is therefore not a
// tidiness, it is the whole mechanism: what reads `::` here is written against
// psl's own slot package, and a change there is a change to make here.

// Slot is one `:: instruction ::` found in a macro.
type Slot struct {
	Start       int    // byte offset of the opening "::"
	End         int    // byte offset just past the closing "::"
	Instruction string // what is written between the markers, trimmed
}

// FindSlot returns the first slot at or after `from`.
func FindSlot(src string, from int) (Slot, bool) {
	for i := from; i+1 < len(src); i++ {
		if !opensSlot(src, i) {
			continue
		}
		end, ok := findClose(src, i+2)
		if !ok {
			continue
		}
		instruction := strings.TrimSpace(src[i+2 : end])
		if instruction == "" {
			// Nothing to ask. psl stops with an error on an empty slot rather
			// than passing over it, so one written in a macro is skipped here
			// and taken apart by Neutralize before the file goes anywhere.
			i = end + 1
			continue
		}
		return Slot{Start: i, End: end + len(Marker), Instruction: instruction}, true
	}
	return Slot{}, false
}

// HasSlot reports whether the text holds a slot.
func HasSlot(src string) bool {
	_, found := FindSlot(src, 0)
	return found
}

// FindCompilerSlot returns the first slot psl itself would fill. It is FindSlot
// — the two read the markers by the same rule — and kept under its own name
// because it asks a different question: not what the macro says, but what the
// compiler will do with the file it is handed. It is the one place to change
// if psl's rule ever moves away from Pob's.
func FindCompilerSlot(src string, from int) (Slot, bool) { return FindSlot(src, from) }

// CompilerHasSlot reports whether psl would find anything to fill here.
func CompilerHasSlot(src string) bool {
	_, found := FindCompilerSlot(src, 0)
	return found
}

// dormantOpen and dormantClose wrap a slot Pob has written out of the macro.
// They are deliberately not `::`: psl reads `::` exactly as Pob does, so a slot
// left in any form Pob would recognise is a slot psl would fill.
const dormantOpen, dormantClose = "<", ">"

// Neutralize writes every slot out of the text — `:: x ::` becomes `<x>` —
// leaving nothing psl would fill while the instruction stays there to be read.
// It is how Pob says a statement is not going to be asked about: the body of a
// block that did not run, a line it could not read.
//
// Every `::` goes, not only the ones that pair up. A marker with nothing to
// close it on its own line — `typeText("a :: b")` types a `::`, it does not ask
// anything — is still an opening as far as the file is concerned, and the next
// live slot below it would close it: psl would fill the span from the typed text
// down to the statement the replay was actually waiting on. A neutralized line
// is one that will never run, so a colon fewer in it costs nothing.
func Neutralize(src string) string {
	for {
		if slot, found := FindSlot(src, 0); found {
			src = src[:slot.Start] + dormantOpen + slot.Instruction + dormantClose + src[slot.End:]
			continue
		}
		if i, found := findOpening(src, 0); found {
			src = src[:i] + ":" + src[i+len(Marker):]
			continue
		}
		return src
	}
}

// findOpening returns the offset of the first `::` that could open a slot,
// whether or not anything closes it.
func findOpening(src string, from int) (int, bool) {
	for i := from; i+1 < len(src); i++ {
		if opensSlot(src, i) {
			return i, true
		}
	}
	return 0, false
}

// opensSlot reports whether the "::" at i can start a slot. Glued to an
// identifier on its left it never can — that is `std::cout`, or a `::` in the
// middle of a word being typed.
func opensSlot(src string, i int) bool {
	if src[i] != ':' || i+1 >= len(src) || src[i+1] != ':' {
		return false
	}
	prev, size := utf8.DecodeLastRuneInString(src[:i])
	if size == 0 {
		return true
	}
	return !isIdentRune(prev)
}

// findClose returns the offset of the "::" that closes a slot opened at from.
// A "::" that runs straight into an identifier is scope resolution written
// inside the instruction, not the end of it.
func findClose(src string, from int) (int, bool) {
	for i := from; i+1 < len(src); i++ {
		if src[i] != ':' || src[i+1] != ':' {
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

package psl

import "strings"

// A macro says in its name whether the compiler has anything to do with it.
//
// MacroPSLExt is a macro written against psl: it may hold `:: … ::` slots, and
// running it means starting the compiler for each one. MacroExt is the same
// language with the slots left out — every statement in it already says what it
// does, so it is replayed as written and psl is never started at all.
//
// The distinction is in the name rather than in the contents because of when it
// has to be known. Whether psl is installed is checked before the cursor moves,
// and what a call() three files down will need is a question asked of a name on
// a line long before that file is read. A name answers it; a file's contents
// answer it only once something has gone looking.
//
// It also gives the two kinds different costs to write down. A `.macro` is a
// recording — what the record button appends, what replays the same way on a
// machine with no compiler and no API key behind it. A `.macro.psl` is a program
// with judgement in it, and the price of that judgement is psl on the PATH.
const (
	// MacroExt names a macro with no slots in it: run as written, psl untouched.
	MacroExt = ".macro"
	// MacroPSLExt names a macro whose slots psl fills as the replay reaches them.
	MacroPSLExt = ".macro.psl"
)

// Deterministic reports whether a file of this name is one psl is never run for.
//
// It reads the name and not the file, so it answers for a call() that has not
// been followed yet. `.macro.psl` ends in `.psl` and so is not deterministic —
// the two extensions are told apart by the end of the name, which is why the
// slotless one is the shorter of them rather than something like `.plain.macro`
// that would have to be matched in the middle.
//
// A slot written in a `.macro` anyway is a contradiction in the file rather than
// something to resolve here: the check names it before the run starts, and the
// replay logs and skips the statement if one somehow gets that far.
func Deterministic(name string) bool { return strings.HasSuffix(name, MacroExt) }

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pob/core/internal/psl"
)

// A macro is read twice: once here, before anything moves, and once during the
// replay where it always was.
//
// The second reading is forgiving on purpose. A line it cannot make sense of is
// logged and skipped, because a macro is half-recorded and half-typed and one
// bad line in the middle of it is a line to fix rather than a reason to refuse
// the other forty. What that leaves is a run that plays the thirty-nine and
// mentions the fortieth only in a log nobody has open — and the quietest of them
// says nothing even there. `move(1)` is a call whose numbers cannot be read, so
// the cursor stays where it was, and the `click()` under it lands wherever the
// statement before happened to leave it.
//
// So the first reading is the strict one. It goes over the whole macro and the
// files it calls, works out everything that is wrong with it, and puts the lot
// in front of the user at once with the line numbers to fix it by. Nothing runs
// until it comes back empty.
//
// What it cannot check is what is not written down yet: what a slot fills to,
// and whether psl could fill it at all. Those two stay where they were, found at
// the statement and logged and skipped, and they are why the replay goes on
// reading as forgivingly as it ever did.

// macroProblemsTitle heads the dialog. It is deliberately not the wording of
// **psl needed**: that one is about the machine and is fixed by installing
// something, and this one is about the file and is fixed by opening it.
const macroProblemsTitle = "Macro problems"

// macroProblemsShown is how many problems the dialog lists. A macro can be wrong
// in more ways than a dialog can hold — a `/*` nobody closed makes every line
// under it unreadable at once — and a list taller than the screen says less than
// a short one that names where the rest are.
const macroProblemsShown = 12

// macroProblemsMessage is the list put in front of the user, under a line saying
// what it means for the run.
//
// Line numbers are what makes it worth reading, so they lead every entry and are
// the numbers an editor counts to: a comment costs its line the meaning and never
// the place, and nothing here renumbers anything.
func macroProblemsMessage(problems []MacroProblem, lead string) string {
	var b strings.Builder
	b.WriteString(lead)
	b.WriteString("\n")
	for i, p := range problems {
		if i == macroProblemsShown {
			fmt.Fprintf(&b, "\n…and %d more — the log has all of them.", len(problems)-i)
			break
		}
		fmt.Fprintf(&b, "\n%s", p)
	}
	return b.String()
}

// MacroProblem is one thing wrong with a macro — the line it is on, the file
// that line is in, and what to fix.
type MacroProblem struct {
	// File names the called file the line is in, and is empty in the macro
	// itself. It is the same rule the log goes by: one file needs no naming, and
	// the numbers start again in each one that a call() brings in.
	File string
	Line int
	Text string
}

func (p MacroProblem) String() string {
	if p.File == "" {
		return fmt.Sprintf("line %d: %s", p.Line, p.Text)
	}
	return fmt.Sprintf("%s line %d: %s", p.File, p.Line, p.Text)
}

// macroProblem is a problem before it knows which file it was in. The walk over
// the called files is what fills that in, since a file does not know it was
// called.
type macroProblem struct {
	line int
	text string
}

func (p macroProblem) String() string { return fmt.Sprintf("line %d: %s", p.line, p.text) }

func problemf(line int, format string, a ...any) macroProblem {
	return macroProblem{line: line, text: fmt.Sprintf(format, a...)}
}

// CheckMacroFile reads a PSL file and reports everything wrong with it and with
// the files it calls, in the order the lines are written.
//
// It is the check Execute runs before the cursor moves, said in the terminal
// instead of in a dialog — one reading, so that what `pob macro --check` says
// about a macro is what would have stopped it from running.
func CheckMacroFile(path string) ([]MacroProblem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return checkMacroSource(string(data), path), nil
}

// checkMacroSource checks a macro that has already been read — the source
// Execute is about to replay, rather than the file it came from, since the two
// are the same only until someone edits one of them mid-run.
func checkMacroSource(source, path string) []MacroProblem {
	chain := []string{path}
	return checkFile(source, path, "", chain)
}

// checkFile reports what is wrong with one file and with everything it calls.
// name is what a problem in this file is labelled with — empty in the macro
// itself — and chain is the files above it, which is what says a call() has come
// back round to one already open.
func checkFile(source, path, name string, chain []string) []MacroProblem {
	nodes, probs := parseMacroProblems(source)
	problems := label(name, probs)
	problems = append(problems, label(name, checkComments(source))...)
	problems = append(problems, label(name, checkStatements(nodes))...)
	problems = append(problems, checkCalls(nodes, filepath.Dir(path), name, chain)...)
	return sortByLine(problems)
}

func label(name string, probs []macroProblem) []MacroProblem {
	out := make([]MacroProblem, 0, len(probs))
	for _, p := range probs {
		out = append(out, MacroProblem{File: name, Line: p.line, Text: p.text})
	}
	return out
}

// sortByLine puts a file's problems back into the order its lines are written.
// The passes each go over the file from the top, so what they hand back reads
// as three lists rather than as one — and a list of problems is read against the
// file it is about, from the top.
//
// Only within a file. A called file's problems arrive after the calling file's
// and stay there, since its line numbers are its own: a list that sorted `line
// 2` of one file in among the lines of another would be sorted by a number that
// does not mean the same thing twice.
func sortByLine(problems []MacroProblem) []MacroProblem {
	for i := 1; i < len(problems); i++ {
		for j := i; j > 0; j-- {
			a, b := problems[j-1], problems[j]
			if a.File != b.File || a.Line <= b.Line {
				break
			}
			problems[j-1], problems[j] = b, a
		}
	}
	return problems
}

// checkStatements reads every statement as the call it claims to be, at every
// depth. It is the half of the check the parse does not do: a line of the shape
// `name(argument, argument)` parses whatever name and however many arguments are
// written in it, and whether that is a statement Pob can carry out is a question
// about the vocabulary rather than about the shape.
func checkStatements(nodes []macroNode) []macroProblem {
	var probs []macroProblem
	for _, node := range nodes {
		if node.isIf || node.isLoop {
			probs = append(probs, checkStatements(node.body)...)
			continue
		}
		if p, bad := checkStatement(node.raw, node.line); bad {
			probs = append(probs, p)
		}
	}
	return probs
}

// checkStatement reads one statement as written, slots and all.
//
// A slot is a value that is not there yet, so what can be checked around one is
// everything the fill will not change: which call it is, and how many arguments
// it was written with. A slot answers with a value and never with more program —
// it cannot come back as two arguments, or as a different call — so those two
// are as true of a statement full of slots as of one with none. What the slot
// itself comes back as is the replay's question, and is the one thing left to
// the log.
func checkStatement(raw string, line int) (macroProblem, bool) {
	name, args, ok := macroArguments(raw)
	if !ok {
		switch {
		case strings.Contains(raw, macroBlockClose):
			return problemf(line, "%s closes a comment that was never opened, which leaves it in the line — the line is then not a statement", macroBlockClose), true
		case raw == macroStopKeyword:
			// The bare word ran once. A macro written then says what it means and is
			// one pair of parentheses away from saying it here too, so it is worth
			// naming rather than leaving to the line about calls in general.
			return problemf(line, "stop is written %s(), with the parentheses every other statement has", macroStopKeyword), true
		case strings.EqualFold(raw, macroStopKeyword):
			return problemf(line, "%q is not a statement — stop is written %s(), lowercase and with parentheses", raw, macroStopKeyword), true
		}
		return problemf(line, "%q is not a statement — a call is name(argument, argument), and nothing follows the closing parenthesis", truncate(raw, 60)), true
	}

	// A name that is itself a slot is a name nothing can be known about until psl
	// has answered, and psl answering with a call name is not something the
	// language promises. Left to the replay, which reads the filled line the way
	// it reads any other.
	if psl.HasSlot(name) {
		return macroProblem{}, false
	}

	shape, known := macroVocabulary[name]
	if !known {
		return problemf(line, "there is no statement called %q — see the Calls page in docs/Macro PSL/06_Calls.md%s", name, didYouMean(name)), true
	}

	// A slot written where a whole argument goes can come back as more than one of
	// them. `move(:: the profile icon ::)` is a macro asking for the offset rather
	// than for one half of it, and what psl answers — `-120, 40` — is the pair;
	// the statement reads as PSL once it is in, and the scaling grows both numbers
	// because it was always written to take a comma-separated answer.
	//
	// So a statement with one of those is wrong here only when what is already
	// written is more than the call can hold: a fill can add arguments and can
	// never take any away.
	switch {
	case wholeSlotArgs(args) > 0:
		if len(args) > shape.most() {
			return problemf(line, "%s takes %s, and %s already", name, shape.wants(), argsWritten(len(args))), true
		}
	case !shape.takes(len(args)):
		return problemf(line, "%s takes %s, and %s", name, shape.wants(), argsWritten(len(args))), true
	}

	for i, arg := range args {
		if psl.HasSlot(arg) {
			continue
		}
		switch {
		case shape.isTime:
			// The check reads the argument as it was written, quotes and all, which
			// is what lets it tell a time from a string that looks like one. The
			// replay is told the same thing by the parse — see parseMacroLine.
			if inner, quoted := strings.CutPrefix(arg, `"`); quoted {
				inner = strings.TrimSuffix(inner, `"`)
				return problemf(line, "%s was written with %s — a time is not a string, so it goes in without the quotes: %s", name, truncate(arg, 42), truncate(inner, 40)), true
			}
			if _, ok := macroTime(arg); !ok {
				return problemf(line, "%s was written with %q, which is not %s", name, truncate(arg, 40), macroTimeWants), true
			}
		case shape.numeric:
			if _, err := strconv.ParseFloat(arg, 64); err != nil {
				return problemf(line, "%s wants numbers, and its %s argument is %q — the statement would do nothing at all", name, ordinal(i), truncate(arg, 40)), true
			}
		}
	}
	return macroProblem{}, false
}

// macroCallShape is what a call has to be written like: how many arguments it
// takes, and what those arguments are.
//
// This is the Calls page of docs/Macro PSL/06_Calls.md — both tables, the
// machine's and the run's — the switch in runMacroAction and the vocabulary in
// macroPrompt said a fourth way, and moves when they do. A call missing from here
// is one the check calls unknown and refuses to run; one whose shape is wrong
// here is a working statement the check would not let past.
type macroCallShape struct {
	arity   []int // the argument counts it accepts
	numeric bool  // every argument is a number
	isTime  bool  // its one argument is a time — see macroTime
}

func (s macroCallShape) takes(n int) bool {
	for _, want := range s.arity {
		if n == want {
			return true
		}
	}
	return false
}

// most is the largest number of arguments a call will take, which is the bound
// on what a slot standing for a whole argument may fill to.
func (s macroCallShape) most() int {
	n := 0
	for _, want := range s.arity {
		if want > n {
			n = want
		}
	}
	return n
}

// wants says how many arguments a call takes, in the words the Calls page uses.
// takeScreenshot is the one that takes two different numbers of them, and the
// table's way of putting that — all four or none — is the way it reads here.
func (s macroCallShape) wants() string {
	if len(s.arity) > 1 {
		return fmt.Sprintf("all %d arguments or none at all", s.arity[len(s.arity)-1])
	}
	switch s.arity[0] {
	case 0:
		return "no arguments"
	case 1:
		return "1 argument"
	default:
		return fmt.Sprintf("%d arguments", s.arity[0])
	}
}

var macroVocabulary = map[string]macroCallShape{
	"move":           {arity: []int{2}, numeric: true},
	"drag":           {arity: []int{2}, numeric: true},
	"scroll":         {arity: []int{2}, numeric: true},
	"click":          {arity: []int{0}},
	"rightClick":     {arity: []int{0}},
	"doubleClick":    {arity: []int{0}},
	"typeText":       {arity: []int{1}},
	"keyPress":       {arity: []int{1}},
	"sleep":          {arity: []int{1}, isTime: true},
	"resetCursor":    {arity: []int{0}},
	"takeScreenshot": {arity: []int{0, 4}, numeric: true},
	macroStopKeyword: {arity: []int{0}},
	macroCallKeyword: {arity: []int{1}},
}

// macroArguments reads a statement as written — before any slot in it is filled,
// which is what separates it from parseMacroLine.
//
// The difference is the comma. parseMacroLine splits on every one of them, which
// is right for a line psl has finished with and wrong for one it has not: an
// instruction is a sentence and may well have a comma in it, so
// `move(:: left, or right ::, 0)` is two arguments and not three. Slots and
// double-quoted strings are stepped over whole, the way they are everywhere the
// language has to find the commas of its own.
func macroArguments(line string) (string, []string, bool) {
	openParen := strings.Index(line, "(")
	if openParen < 0 || !strings.HasSuffix(line, ")") {
		return "", nil, false
	}
	name := strings.TrimSpace(line[:openParen])
	if name == "" {
		return "", nil, false
	}
	return name, splitMacroArgs(line[openParen+1 : len(line)-1]), true
}

// wholeSlotArgs counts the arguments that are nothing but a slot, which are the
// ones a fill can turn into several.
//
// Nothing but: a slot that is part of an argument fills in place and leaves the
// commas where they were — `move(:: how far ::0, 1)` is two arguments before the
// fill and two after it. It is the argument that is a whole question that can be
// answered with a whole list.
func wholeSlotArgs(args []string) int {
	n := 0
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if slot, found := psl.FindSlot(arg, 0); found && slot.Start == 0 && slot.End == len(arg) {
			n++
		}
	}
	return n
}

// splitMacroArgs splits an argument list on the commas that are its own — the
// ones outside every slot and every double-quoted string.
func splitMacroArgs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	slots := slotStarts(s)
	var args []string
	start, i := 0, 0
	for i < len(s) {
		if s[i] == '"' {
			i = endOfString(s, i)
			continue
		}
		if end, ok := slots[i]; ok {
			i = end
			continue
		}
		if s[i] == ',' {
			args = append(args, strings.TrimSpace(s[start:i]))
			i++
			start = i
			continue
		}
		i++
	}
	return append(args, strings.TrimSpace(s[start:]))
}

// checkComments finds the block comment nothing closes.
//
// It is the one comment mistake worth its own line, because of how much of a
// file it takes: a `/*` with no `*/` under it runs to the end, so every
// statement below it stops being a statement. The macro still parses — there is
// simply almost nothing left of it — and a run that plays the four lines above
// the comment and none of the thirty-six under it is the sort of thing to be
// told about rather than to work out.
func checkComments(source string) []macroProblem {
	inBlock, openedAt := false, 0
	for n, line := range strings.Split(source, "\n") {
		wasOpen := inBlock
		_, inBlock = commentSpans(line, inBlock)
		if inBlock && !wasOpen {
			openedAt = n + 1
		}
	}
	if !inBlock {
		return nil
	}
	return []macroProblem{problemf(openedAt, "%s is never closed by a %s, so the comment runs to the end of the file and nothing under it runs", macroBlockOpen, macroBlockClose)}
}

// checkCalls follows every call() the way the replay would, and reports the ones
// that would not be made.
//
// Following them is the point. A macro whose second file is the broken one is a
// macro to refuse at the start, not thirty statements in with half of what it
// does already done — the same reason the psl check reads the called files
// rather than finding out at the call.
func checkCalls(nodes []macroNode, dir, name string, chain []string) []MacroProblem {
	var problems []MacroProblem
	for _, node := range nodes {
		if node.isIf || node.isLoop {
			problems = append(problems, checkCalls(node.body, dir, name, chain)...)
			continue
		}
		if node.action != macroCallKeyword || len(node.args) != 1 {
			// No path, or more arguments than a path: checkStatement has it already.
			continue
		}
		// A path that is itself a slot is not known until the replay reaches it, so
		// there is nothing here to follow or to refuse.
		if psl.HasSlot(node.args[0]) {
			continue
		}
		problems = append(problems, checkCall(node, dir, name, chain)...)
	}
	return problems
}

func checkCall(node macroNode, dir, name string, chain []string) []MacroProblem {
	arg := node.args[0]
	at := func(format string, a ...any) []MacroProblem {
		return []MacroProblem{{File: name, Line: node.line, Text: fmt.Sprintf(format, a...)}}
	}

	path, err := resolveCallPath(dir, arg)
	if err != nil {
		return at("call(%q) — %v", arg, err)
	}
	for _, open := range chain {
		if open == path {
			return at("call(%q) reaches %s, which is already running above it — a replay with no end in it", arg, filepath.Base(path))
		}
	}
	if len(chain) >= maxCallDepth {
		return at("call(%q) is %d files deep, which is as far as call goes", arg, len(chain))
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return at("call(%q) names %s, and there is no such file", arg, path)
	}
	if err != nil {
		return at("call(%q) — %s cannot be read: %v", arg, path, err)
	}
	return checkFile(string(data), path, filepath.Base(path), append(chain, path))
}

// didYouMean names the statement a misspelling was nearly, and says nothing when
// nothing is near enough. Case is most of what goes wrong — the vocabulary is
// camelCase and a recording is not what wrote the line — so it is the first
// thing looked at, together with the underscore a snake_case hand puts where the
// capital belongs. That second one is also what an old macro trips on:
// takeScreenshot was written take_screenshot once, and a file recorded then
// arrives here rather than at a name nothing recognises.
func didYouMean(name string) string {
	for known := range macroVocabulary {
		if strings.EqualFold(unseparated(known), unseparated(name)) {
			return fmt.Sprintf(". Did you mean %s? Names are camelCase and case-sensitive", known)
		}
	}
	return ""
}

// unseparated drops the underscores out of a name, which is what lets a
// snake_case spelling of a camelCase statement compare equal to it.
func unseparated(name string) string {
	return strings.ReplaceAll(name, "_", "")
}

func ordinal(i int) string {
	for n, word := range []string{"first", "second", "third", "fourth"} {
		if n == i {
			return word
		}
	}
	return fmt.Sprintf("%dth", i+1)
}

func argsWritten(n int) string {
	switch n {
	case 0:
		return "none was written"
	case 1:
		return "1 was written"
	default:
		return fmt.Sprintf("%d were written", n)
	}
}

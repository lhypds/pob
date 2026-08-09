package agent

import (
	"strings"
	"testing"

	"pob/core/internal/psl"
)

// nodeSummary renders a parsed macro as one line per statement, indented by
// depth, so a test can state the shape it expects. A statement still holding a
// slot is shown as it was written, since that is all that is known of it until
// the replay fills it.
func nodeSummary(nodes []macroNode, indent string) []string {
	var out []string
	for _, n := range nodes {
		if n.isIf {
			out = append(out, indent+"if ("+n.condition+")")
			out = append(out, nodeSummary(n.body, indent+"  ")...)
			continue
		}
		if n.slots {
			out = append(out, indent+"unfilled: "+n.raw)
			continue
		}
		line := indent + n.action + "("
		for i, a := range n.args {
			if i > 0 {
				line += ", "
			}
			line += a
		}
		out = append(out, line+")")
	}
	return out
}

func checkParse(t *testing.T, macro string, want []string) {
	t.Helper()
	got := nodeSummary(parseMacro(macro), "")
	if len(got) != len(want) {
		t.Fatalf("parsed %d statements, want %d:\ngot  %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseMacroActions(t *testing.T) {
	checkParse(t, `move(398, 915)
click()

typeText("hello, world")
`, []string{
		"move(398, 915)",
		"click()",
		`typeText(hello, world)`,
	})
}

func TestParseMacroIfBlock(t *testing.T) {
	checkParse(t, `move(398, 915)
click()
if (:: the window focus on a wechat user ::) {
    move(128, 738)
    click()
}
drag(-775, -615)`, []string{
		"move(398, 915)",
		"click()",
		"if (:: the window focus on a wechat user ::)",
		"  move(128, 738)",
		"  click()",
		"drag(-775, -615)",
	})
}

// A slot goes anywhere in a statement, not only in an if. What the statement
// says is not known at parse time, so it is kept as written and read again once
// the replay has filled it.
func TestParseMacroSlotsInAnyStatement(t *testing.T) {
	checkParse(t, `move(:: the x offset to the Save button ::, 40)
typeText(:: what to say ::)
typeText("Hi :: the name on screen ::")
click()`, []string{
		"unfilled: move(:: the x offset to the Save button ::, 40)",
		"unfilled: typeText(:: what to say ::)",
		`unfilled: typeText("Hi :: the name on screen ::")`,
		"click()",
	})
}

// A condition written out rather than asked needs no model call, and reads as
// the value it names.
func TestParseMacroLiteralCondition(t *testing.T) {
	checkParse(t, `if (true) {
	click()
}
if (false) {
	move(1, 2)
}`, []string{
		"if (true)",
		"  click()",
		"if (false)",
		"  move(1, 2)",
	})
}

// The keyword is written lowercase, and read whatever its case: a block that
// went unrecognised would run its body unguarded.
func TestParseMacroIfKeywordCase(t *testing.T) {
	for _, keyword := range []string{"if", "IF", "If"} {
		checkParse(t, keyword+` (:: a chat window is open ::) {
	click()
}`, []string{
			"if (:: a chat window is open ::)",
			"  click()",
		})
	}
}

func TestParseMacroNestedIf(t *testing.T) {
	checkParse(t, `if (:: a chat window is open ::) {
	if (:: the message list is empty ::) {
		typeText("hi")
	}
	click()
}`, []string{
		"if (:: a chat window is open ::)",
		"  if (:: the message list is empty ::)",
		"    typeText(hi)",
		"  click()",
	})
}

// An if left open runs to the end of the macro rather than dropping the lines
// under it — they stay guarded by the condition either way.
func TestParseMacroUnclosedIf(t *testing.T) {
	checkParse(t, `if (:: a chat window is open ::) {
	click()`, []string{
		"if (:: a chat window is open ::)",
		"  click()",
	})
}

// A `}` with no if above it is dropped, and the statements around it stand.
func TestParseMacroStrayCloseBrace(t *testing.T) {
	checkParse(t, `click()
}
move(1, 2)`, []string{
		"click()",
		"move(1, 2)",
	})
}

// A malformed if header still opens a block, and the block is dropped: lines
// written to be guarded must not run unguarded.
func TestParseMacroMalformedIfDropsBlock(t *testing.T) {
	for _, header := range []string{
		"if (:: the window is focused ::)",  // no {
		"if (:: the window is focused :: {", // no )
		"if :: the window is focused :: {",  // no parentheses
		"if (the window is focused) {",      // neither a slot nor true/false
		"if (:: ::) {",                      // nothing to ask
		"if () {",
		"if {",
		"if",
	} {
		checkParse(t, header+`
	click()
}
move(1, 2)`, []string{
			"move(1, 2)",
		})
	}
}

// A line that only starts with the letters of the keyword is an action line.
func TestParseMacroIfPrefixIsNotKeyword(t *testing.T) {
	checkParse(t, `iframe(1, 2)`, []string{"iframe(1, 2)"})
	checkParse(t, `IFrame(1, 2)`, []string{"IFrame(1, 2)"})
}

func TestParseIfHeader(t *testing.T) {
	tests := []struct {
		line      string
		condition string
		isIf      bool
	}{
		{"if (:: the window focus on a wechat user ::) {", ":: the window focus on a wechat user ::", true},
		{"if  (  ::  spaced  out  ::  )  {", "::  spaced  out  ::", true},
		{"if\t(:: a tab after the keyword ::) {", ":: a tab after the keyword ::", true},
		{"if(:: no space at all ::){", ":: no space at all ::", true},
		{"IF (:: read whatever its case ::) {", ":: read whatever its case ::", true},
		{"if (:: a { brace in the condition ::) {", ":: a { brace in the condition ::", true},
		{"if (true) {", "true", true},
		{"if (FALSE) {", "FALSE", true},
		{"if (:: no closing brace ::)", "", true},
		{"if :: no parentheses :: {", "", true},
		{"if (no slot) {", "", true},
		{"if (:: half a slot ::) {", ":: half a slot ::", true},
		{"if (:: half marked) {", "", true},
		{"if (:: ::) {", "", true},
		{"if () {", "", true},
		{"if {", "", true},
		{"if", "", true},
		{"click()", "", false},
		{"iframe(1, 2)", "", false},
		{"ifs(1, 2)", "", false},
	}
	for _, tt := range tests {
		condition, isIf := parseIfHeader(tt.line)
		if isIf != tt.isIf || condition != tt.condition {
			t.Errorf("parseIfHeader(%q) = (%q, %v), want (%q, %v)", tt.line, condition, isIf, tt.condition, tt.isIf)
		}
	}
}

// What the AI answers is substituted and the statement read again, so the shape
// of the answer is what decides what the statement becomes.
func TestFilledStatementsParse(t *testing.T) {
	tests := []struct {
		filled string
		name   string
		args   []string
	}{
		{`move(-120, 40)`, "move", []string{"-120", "40"}},
		{`typeText("Hello")`, "typeText", []string{"Hello"}},
		{`typeText("Hi Bob")`, "typeText", []string{"Hi Bob"}},
		{`keyPress("cmd+v")`, "keyPress", []string{"cmd+v"}},
	}
	for _, tt := range tests {
		name, args, ok := parseMacroLine(tt.filled)
		if !ok {
			t.Errorf("parseMacroLine(%q) did not read as a statement", tt.filled)
			continue
		}
		if name != tt.name {
			t.Errorf("parseMacroLine(%q) name = %q, want %q", tt.filled, name, tt.name)
		}
		if len(args) != len(tt.args) {
			t.Errorf("parseMacroLine(%q) args = %q, want %q", tt.filled, args, tt.args)
			continue
		}
		for i := range args {
			if args[i] != tt.args[i] {
				t.Errorf("parseMacroLine(%q) arg %d = %q, want %q", tt.filled, i, args[i], tt.args[i])
			}
		}
	}
}

// A macro with no slot never calls the model, so it needs nothing configured —
// and one with a slot anywhere in it, at any depth, does.
func TestHasMacroSlot(t *testing.T) {
	tests := []struct {
		name  string
		macro string
		want  bool
	}{
		{"no slot", "move(1, 2)\nclick()", false},
		{"a literal condition is not a slot", "if (true) {\n\tclick()\n}", false},
		{"an if condition", "click()\nif (:: a dialog is open ::) {\n\tclick()\n}", true},
		{"an argument", "move(:: the x offset ::, 40)", true},
		{"inside a string", `typeText("Hi :: the name ::")`, true},
		{"nested in a block", "if (true) {\n\tif (:: a dialog is open ::) {\n\t\tclick()\n\t}\n}", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMacroSlot(parseMacro(tt.macro)); got != tt.want {
				t.Errorf("hasMacroSlot = %v, want %v", got, tt.want)
			}
		})
	}
}

// A condition holds only when it says so. Anything Pob cannot read as one of
// the two words is not a verdict, and the block it guards stays unexecuted —
// running statements a broken condition was written to guard is the one outcome
// nobody asked for.
func TestConditionHolds(t *testing.T) {
	tests := []struct {
		expr  string
		holds bool
		read  bool
	}{
		{"true", true, true},
		{"false", false, true},
		{"TRUE", true, true},
		{"False", false, true},
		{"  true  ", true, true},
		{`"true"`, true, true},
		{`"false"`, false, true},
		{"yes", false, false},
		{"1", false, false},
		{"", false, false},
		{"the dialog is open", false, false},
		{"true-ish", false, false},
	}
	for _, tt := range tests {
		holds, read := conditionHolds(tt.expr)
		if holds != tt.holds || read != tt.read {
			t.Errorf("conditionHolds(%q) = (%v, %v), want (%v, %v)", tt.expr, holds, read, tt.holds, tt.read)
		}
	}
}

func TestCountMacroNodes(t *testing.T) {
	nodes := parseMacro(`move(1, 2)
if (:: something is true ::) {
	click()
	if (:: something else is true ::) {
		click()
	}
}`)
	if got := countMacroNodes(nodes); got != 5 {
		t.Errorf("countMacroNodes = %d, want 5", got)
	}
}

// psl is handed the macro itself: no preamble, nothing rewritten, the file as
// it was typed.
func TestPSLIsHandedTheMacroItself(t *testing.T) {
	macro := `click()
move(:: the x offset ::, 40)
typeText(:: what to say ::)`
	run := &macroRun{source: macro}

	if run.source != macro {
		t.Errorf("the file for psl is %q, want the macro as written", run.source)
	}
	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("no slot psl would fill in the file handed to it")
	}
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the first statement's slot", slot.Instruction)
	}
	if line, ok := liveSlotLine(run.source); !ok || line != 1 {
		t.Errorf("psl would fill a slot on line %d, want line 2", line+1)
	}
}

// The answer goes into the macro, so the next run is handed a file whose first
// remaining slot is the next statement's. That is what keeps psl — which fills
// the first slot and no other — on the statement the replay is waiting on.
func TestAnswersAreCarriedForward(t *testing.T) {
	run := &macroRun{source: "move(:: the x offset ::, 40)\ntypeText(:: what to say ::)"}

	run.record(1, "move(-120, 40)")

	if want := "move(-120, 40)\ntypeText(:: what to say ::)"; run.source != want {
		t.Errorf("the file for the next run is %q, want %q", run.source, want)
	}
	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("no slot psl would fill in the file handed to it")
	}
	if slot.Instruction != "what to say" {
		t.Errorf("psl would fill %q, want the next statement's slot", slot.Instruction)
	}
}

// A statement with two slots is filled one run at a time: the second is left in
// the file, and is the first remaining slot once the first has its answer.
func TestBothSlotsOfAStatementAreAskedInTurn(t *testing.T) {
	run := &macroRun{source: "move(::the x offset::, ::the y offset::)"}

	slot, _ := psl.FindCompilerSlot(run.source, 0)
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the statement's first slot", slot.Instruction)
	}

	run.record(1, "move(-120, ::the y offset::)")

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("the second slot is no longer in the file")
	}
	if slot.Instruction != "the y offset" {
		t.Errorf("psl would fill %q, want the statement's second slot", slot.Instruction)
	}
}

// A block that did not run is a block whose slots are never asked about. Left in
// the file they would be what psl fills next, in place of the statement under
// them — answered from the wrong screenshot, for a statement that is not going
// to run.
func TestASkippedBlocksSlotsAreSpent(t *testing.T) {
	macro := `if (:: a dialog is open ::) {
	typeText(:: what to say ::)
}
move(:: the x offset ::, 40)`
	nodes := parseMacro(macro)
	run := &macroRun{source: macro}

	run.spendBlock(nodes[0].body)
	run.spend(nodes[0].line)

	if !strings.Contains(run.source, "<a dialog is open>") || !strings.Contains(run.source, "<what to say>") {
		t.Errorf("the skipped block still holds a slot: %q", run.source)
	}
	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("no slot psl would fill in the file handed to it")
	}
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the statement under the block", slot.Instruction)
	}
}

// A line no statement came out of is a line that will never run, and its slot is
// spent before the first psl run rather than filled in place of a statement that
// does — here, the body of an if whose header Pob could not read.
func TestUncoveredLinesAreSpent(t *testing.T) {
	macro := `if :: a dialog is open :: {
	typeText(:: what to say ::)
}
move(:: the x offset ::, 40)`
	run := &macroRun{source: macro}

	run.spendUncovered(parseMacro(macro))

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("no slot psl would fill in the file handed to it")
	}
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the one statement that runs", slot.Instruction)
	}
}

// The answer comes back inside the file psl rewrote, and what the statement
// became is what sits between the lines before it and the lines after it —
// however many lines the answer ran to.
func TestExtractLine(t *testing.T) {
	before := "a\nb\nTARGET\nd\ne"
	tests := []struct {
		name  string
		after string
		line  int
		want  string
		ok    bool
	}{
		{"one line", "a\nb\nfilled\nd\ne", 2, "filled", true},
		{"the first line", "filled\nb\nTARGET\nd\ne", 0, "filled", true},
		{"the last line", "a\nb\nTARGET\nd\nfilled", 4, "filled", true},
		{"an answer of several lines", "a\nb\nfilled\nover\nlines\nd\ne", 2, "filled\nover\nlines", true},
		{"the file came back shorter", "a\nb", 2, "", false},
		{"a line that is not in the file", "a\nb\nc\nd\ne", 9, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractLine(before, tt.after, tt.line)
			if ok != tt.ok {
				t.Fatalf("extractLine ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("extractLine = %q, want %q", got, tt.want)
			}
		})
	}
}

// A tool that rewrites a file decides for itself whether it ends with a
// newline, and the statement must come back the same either way.
func TestExtractLineIgnoresTheFinalNewline(t *testing.T) {
	for _, tt := range []struct{ name, before, after string }{
		{"a newline appeared", "a\nTARGET\nc", "a\nfilled\nc\n"},
		{"a newline went missing", "a\nTARGET\nc\n", "a\nfilled\nc"},
		{"neither had one", "a\nTARGET\nc", "a\nfilled\nc"},
		{"both had one", "a\nTARGET\nc\n", "a\nfilled\nc\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractLine(tt.before, tt.after, 1)
			if !ok || got != "filled" {
				t.Errorf("extractLine = (%q, %v), want (\"filled\", true)", got, ok)
			}
		})
	}
}

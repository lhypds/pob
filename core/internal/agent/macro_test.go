package agent

import (
	"strings"
	"testing"
	"time"

	"pob/core/internal/psl"
)

// nodeSummary renders a parsed macro as one line per statement, indented by
// depth, so a test can state the shape it expects. A statement still holding a
// slot is shown as it was written, since that is all that is known of it until
// the replay fills it.
func nodeSummary(nodes []macroNode, indent string) []string {
	var out []string
	for _, n := range nodes {
		if n.isIf || n.isLoop {
			out = append(out, indent+macroBlockLabel(n))
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

// A loop with nothing but a count runs its body exactly that many times, and
// asks nothing.
func TestParseMacroLoopBlock(t *testing.T) {
	checkParse(t, `click()
loop (3) {
	keyPress("down")
	click()
}
drag(-775, -615)`, []string{
		"click()",
		"loop (3)",
		`  keyPress(down)`,
		"  click()",
		"drag(-775, -615)",
	})
}

// stop() is a call like the rest of them — the parentheses hold nothing and are
// written all the same.
func TestParseMacroStop(t *testing.T) {
	checkParse(t, `click()
stop()
move(1, 2)`, []string{
		"click()",
		"stop()",
		"move(1, 2)",
	})
	checkParse(t, "  stop()  ", []string{"stop()"})
}

// The bare word is not the statement. It ran once, so a macro written then is
// dropped a line at a time rather than quietly ending the run at a word that no
// longer says anything — and the check ahead of the replay names the fix.
func TestParseMacroStopWantsItsParentheses(t *testing.T) {
	checkParse(t, "stop\nclick()", []string{"click()"})
	checkParse(t, "STOP\nclick()", []string{"click()"})
	checkParse(t, "stopped(1)", []string{"stopped(1)"})
}

// A line of the macro and a line psl filled to the same text are the same
// statement — the two go through one reader so that they cannot come apart.
//
// quoted is the one thing the reader knows that the values it hands back do not:
// the quotes are off them by then, and `sleep("10m")` and `sleep(10m)` are the
// same three characters afterwards.
func TestParseMacroLineReadsEveryStatement(t *testing.T) {
	tests := []struct {
		line   string
		name   string
		quoted bool
		ok     bool
	}{
		{"stop()", "stop", false, true},
		{"stop", "", false, false},
		{"STOP", "", false, false},
		{"stop now", "", false, false},
		{"sleep(3s)", "sleep", false, true},
		{`sleep("3s")`, "sleep", true, true},
		{`call("../shared.psl")`, "call", true, true},
		{"click()", "click", false, true},
		{"move(1, 2)", "move", false, true},
	}
	for _, tt := range tests {
		name, _, quoted, ok := parseMacroLine(tt.line)
		if ok != tt.ok || name != tt.name || quoted != tt.quoted {
			t.Errorf("parseMacroLine(%q) = (%q, quoted %v, %v), want (%q, quoted %v, %v)", tt.line, name, quoted, ok, tt.name, tt.quoted, tt.ok)
		}
	}
}

// A time is a number with its unit on the end. Both the check and the replay
// read one here, so a time one of them refuses is one the other would not have
// waited.
func TestMacroTime(t *testing.T) {
	tests := []struct {
		arg  string
		want time.Duration
		ok   bool
	}{
		{"3s", 3 * time.Second, true},
		{"10m", 10 * time.Minute, true},
		{"5h", 5 * time.Hour, true},
		{"250ms", 250 * time.Millisecond, true},
		{"0.5s", 500 * time.Millisecond, true},
		{"0s", 0, true},

		// Units written one after another add up, which is how a time says what
		// two of them would have to say together.
		{"10h5m", 10*time.Hour + 5*time.Minute, true},
		{"1h30m", 90 * time.Minute, true},
		{"  3s  ", 3 * time.Second, true},

		// The unit is the type. A bare number is a number — which is what a macro
		// written before times existed has in it, and what the check now names.
		{"500", 0, false},
		{"2", 0, false},
		{"0", 0, false},

		// And a time is nothing else: no unit that is not one, no space in the
		// middle of it, and nothing below none at all. The quotes reach here only
		// from the check, which reads an argument as written; the replay is told
		// by the parse — see TestParseMacroLineReadsEveryStatement.
		{`"10m"`, 0, false},
		{"soon", 0, false},
		{"10 m", 0, false},
		{"10 minutes", 0, false},
		{"-3s", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := macroTime(tt.arg)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("macroTime(%q) = (%v, %v), want (%v, %v)", tt.arg, got, ok, tt.want, tt.ok)
		}
	}
}

// call is an ordinary call: it is what it does that is new, not how it is
// written.
func TestParseMacroCall(t *testing.T) {
	checkParse(t, `call("../shared.psl")
call("sub/other.psl")`, []string{
		"call(../shared.psl)",
		"call(sub/other.psl)",
	})
}

// A loop with a condition in front of the count runs while the condition holds,
// and the count is the most passes it may make.
func TestParseMacroLoopWithCondition(t *testing.T) {
	checkParse(t, `loop (:: the window is still open ::, 5) {
	move(:: the x offset to the Close button ::, 0)
	click()
}`, []string{
		"loop (:: the window is still open ::, 5)",
		"  unfilled: move(:: the x offset to the Close button ::, 0)",
		"  click()",
	})
}

// An instruction is a sentence written for a model, and a comma in it is part of
// what is being asked rather than the one that separates it from the count.
func TestParseMacroLoopConditionHoldingAComma(t *testing.T) {
	checkParse(t, `loop (:: the list is still loading, not empty ::, 4) {
	click()
}`, []string{
		"loop (:: the list is still loading, not empty ::, 4)",
		"  click()",
	})
}

// A loop written without a condition is a loop whose condition always holds:
// `loop (3)` and `loop (true, 3)` are one and the same loop. The count is what
// ends both, the body of both is the same body, and neither asks anything on the
// way — the one that has a condition is read without a model, and the one that
// has none has nothing to read.
func TestALoopWithoutAConditionIsALoopWhoseConditionHolds(t *testing.T) {
	body := "\tkeyPress(\"down\")\n\tclick()\n}"
	counted := parseMacro("loop (3) {\n" + body)
	written := parseMacro("loop (true, 3) {\n" + body)

	if len(counted) != 1 || len(written) != 1 {
		t.Fatalf("parsed %d and %d statements, want one loop each", len(counted), len(written))
	}
	if counted[0].count != 3 || written[0].count != 3 {
		t.Errorf("counts are %d and %d, want 3 passes at most either way", counted[0].count, written[0].count)
	}
	if counted[0].condition != "" {
		t.Errorf("loop (3) parsed the condition %q, want none — nothing is checked before a pass", counted[0].condition)
	}
	if holds, read := conditionHolds(written[0].condition); !holds || !read {
		t.Errorf("conditionHolds(%q) = (%v, %v), want it read as true", written[0].condition, holds, read)
	}
	if hasMacroSlot(counted) || hasMacroSlot(written) {
		t.Error("one of them would send Pob to psl, and neither has anything to ask")
	}

	got := strings.Join(nodeSummary(counted[0].body, ""), "\n")
	want := strings.Join(nodeSummary(written[0].body, ""), "\n")
	if got != want {
		t.Errorf("the bodies differ:\ngot  %q\nwant %q", got, want)
	}
}

func TestParseMacroLoopKeywordCase(t *testing.T) {
	for _, keyword := range []string{"loop", "LOOP", "Loop"} {
		checkParse(t, keyword+` (2) {
	click()
}`, []string{
			"loop (2)",
			"  click()",
		})
	}
}

// Blocks nest either way round, as deep as the macro needs.
func TestParseMacroNestedLoop(t *testing.T) {
	checkParse(t, `loop (:: another unread message ::, 10) {
	if (:: the message is a question ::) {
		loop (2) {
			keyPress("return")
		}
	}
	click()
}`, []string{
		"loop (:: another unread message ::, 10)",
		"  if (:: the message is a question ::)",
		"    loop (2)",
		`      keyPress(return)`,
		"  click()",
	})
}

// A malformed loop header still opens a block, and the block is dropped: a body
// written to run until something is true must not run once, unbounded.
func TestParseMacroMalformedLoopDropsBlock(t *testing.T) {
	for _, header := range []string{
		"loop (:: the window is still open ::, 5)",  // no {
		"loop (:: the window is still open ::, 5 {", // no )
		"loop :: the window is still open ::, 5 {",  // no parentheses
		"loop (:: how many times ::) {",             // the count is written out, never asked
		"loop (:: still open ::, :: how many ::) {",
		"loop (twice) {",
		"loop (2.5) {",
		"loop (0) {",
		"loop (-1) {",
		"loop (the window is still open, 5) {", // neither a slot nor true/false
		"loop (, 5) {",
		"loop () {",
		"loop {",
		"loop",
	} {
		checkParse(t, header+`
	click()
}
move(1, 2)`, []string{
			"move(1, 2)",
		})
	}
}

func TestParseMacroLoopPrefixIsNotKeyword(t *testing.T) {
	checkParse(t, `loopback(1, 2)`, []string{"loopback(1, 2)"})
	checkParse(t, `loops(3)`, []string{"loops(3)"})
}

func TestParseLoopHeader(t *testing.T) {
	tests := []struct {
		line      string
		condition string
		count     int
		isLoop    bool
	}{
		{"loop (3) {", "", 3, true},
		{"loop(3){", "", 3, true},
		{"loop  (  3  )  {", "", 3, true},
		{"loop\t(3) {", "", 3, true},
		{"LOOP (3) {", "", 3, true},
		{"loop (1) {", "", 1, true},
		{"loop (:: the window is still open ::, 5) {", ":: the window is still open ::", 5, true},
		{"loop (::the window is still open::,5) {", "::the window is still open::", 5, true},
		{"loop (:: still loading, not empty ::, 5) {", ":: still loading, not empty ::", 5, true},
		{"loop (:: a { brace in the condition ::, 5) {", ":: a { brace in the condition ::", 5, true},
		{"loop (true, 5) {", "true", 5, true},
		{"loop (FALSE, 5) {", "FALSE", 5, true},
		{"loop (:: no closing brace ::, 5)", "", 0, true},
		{"loop :: no parentheses ::, 5 {", "", 0, true},
		{"loop (:: how many times ::) {", "", 0, true},
		{"loop (:: ::, 5) {", "", 0, true},
		{"loop (no slot, 5) {", "", 0, true},
		{"loop (, 5) {", "", 0, true},
		{"loop (5, :: the window is still open ::) {", "", 0, true},
		{"loop (0) {", "", 0, true},
		{"loop (-2) {", "", 0, true},
		{"loop (2.5) {", "", 0, true},
		{"loop () {", "", 0, true},
		{"loop {", "", 0, true},
		{"loop", "", 0, true},
		{"click()", "", 0, false},
		{"loopback(1, 2)", "", 0, false},
		{"loops(3)", "", 0, false},
	}
	for _, tt := range tests {
		condition, count, isLoop := parseLoopHeader(tt.line)
		if isLoop != tt.isLoop || condition != tt.condition || count != tt.count {
			t.Errorf("parseLoopHeader(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tt.line, condition, count, isLoop, tt.condition, tt.count, tt.isLoop)
		}
	}
}

// The count is the last thing in the header, so the comma that separates it from
// the condition is the last one written outside a slot.
func TestLastArgumentComma(t *testing.T) {
	tests := []struct {
		expr string
		want int
	}{
		{"3", -1},
		{":: the window is still open ::", -1},
		{":: a, b, c ::", -1},
		{"true, 5", 4},
		{":: the window is still open ::, 5", 30},
		{":: a, b ::, 5", 10},
	}
	for _, tt := range tests {
		if got := lastArgumentComma(tt.expr); got != tt.want {
			t.Errorf("lastArgumentComma(%q) = %d, want %d", tt.expr, got, tt.want)
		}
	}
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
		name, args, _, ok := parseMacroLine(tt.filled)
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
		{"a loop counted out is not a slot", "loop (3) {\n\tclick()\n}", false},
		{"a loop condition", "loop (:: another unread message ::, 5) {\n\tclick()\n}", true},
		{"nested in a loop", "loop (3) {\n\ttypeText(:: what to say ::)\n}", true},
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

// A loop is the size it is written, not the length of the run it turns into.
func TestCountMacroNodesCountsALoopOnce(t *testing.T) {
	nodes := parseMacro(`loop (10) {
	click()
	move(1, 2)
}`)
	if got := countMacroNodes(nodes); got != 3 {
		t.Errorf("countMacroNodes = %d, want 3", got)
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

// Every pass of a loop puts its statements back the way they were written, so
// their slots are asked again from the screen as it now is rather than answered
// once and repeated. The slot psl reaches for is the header's again — the block
// the replay is standing on is the topmost thing in the file still holding one.
func TestALoopAsksItsSlotsAgainOnTheNextPass(t *testing.T) {
	macro := `click()
loop (:: the window is still open ::, 3) {
	move(:: the x offset to the Close button ::, 0)
	click()
}
typeText(:: what to say ::)`
	nodes := parseMacro(macro)
	loop := nodes[1]
	run := newMacroRun("test", "macro.psl", macro, "")

	// The first pass: the condition answered, then the statement under it.
	run.record(loop.line, "loop (true, 3) {")
	run.record(loop.body[0].line, "move(-120, 0)")

	run.restore(loop.line)
	run.restoreBlock(loop.body)

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("the restored macro holds nothing psl would fill")
	}
	if slot.Instruction != "the window is still open" {
		t.Errorf("psl would fill %q, want the loop's condition asked again", slot.Instruction)
	}
	if !strings.Contains(run.source, ":: the x offset to the Close button ::") {
		t.Errorf("the body was not put back for the next pass: %q", run.source)
	}
	if got, want := strings.Count(run.source, "\n"), strings.Count(macro, "\n"); got != want {
		t.Errorf("restoring left %d newlines, want %d — every statement is found by its line number", got, want)
	}
}

// However a loop ends, what it put back and did not run is spent. Left live it
// is what psl fills next, in place of the statement under the block — answered
// from the wrong screenshot, for a pass that is not going to happen.
func TestALoopSpendsThePassItSetUpAndDidNotRun(t *testing.T) {
	macro := `loop (:: another unread message ::, 3) {
	typeText(:: a short reply ::)
}
move(:: the x offset to Send ::, 0)`
	nodes := parseMacro(macro)
	run := newMacroRun("test", "macro.psl", macro, "")

	// A pass was set up and the condition then read false, so the body never ran.
	run.record(nodes[0].line, "loop (false, 3) {")
	run.spendBlock(nodes[0].body)
	run.spend(nodes[0].line)

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("no slot psl would fill in the file handed to it")
	}
	if slot.Instruction != "the x offset to Send" {
		t.Errorf("psl would fill %q, want the statement under the block", slot.Instruction)
	}
}

// A block that never ran is one whose statements were never asked about, and a
// loop puts back only what it is about to replay: everything above the header is
// filled or spent by then, and everything below is as it was.
func TestALoopRestoresOnlyItsOwnStatements(t *testing.T) {
	macro := `if (:: a dialog is open ::) {
	typeText(:: what to say ::)
}
loop (2) {
	move(:: the x offset ::, 0)
}`
	nodes := parseMacro(macro)
	run := newMacroRun("test", "macro.psl", macro, "")

	// The if did not hold, so its block is spent and its header is answered.
	run.spendBlock(nodes[0].body)
	run.record(nodes[0].line, "if (false) {")

	run.restore(nodes[1].line)
	run.restoreBlock(nodes[1].body)

	if strings.Contains(run.source, ":: what to say ::") {
		t.Errorf("the loop put back a statement that is not its own: %q", run.source)
	}
	slot, _ := psl.FindCompilerSlot(run.source, 0)
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the statement the loop is about to replay", slot.Instruction)
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

package agent

import (
	"strings"
	"testing"

	"pob/core/internal/psl"
)

// What is left of a line once its comments are out. The line is what the parse
// reads, so this is the whole of what a comment does to a statement.
func TestStripComments(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"a whole line", "// move the cursor", ""},
		{"the end of a line", "click() // the Save button", "click() "},
		{"indented", "\t// inside a block", "\t"},
		{"nothing to take out", "click()", "click()"},
		{"a block on one line", "click() /* why */", "click() "},
		{"a block between two statements", "click() /* why */ ", "click()  "},
		{"a block opening", "click() /* why", "click() "},
		{"a header", "if (true) { // when it holds", "if (true) { "},
		{"a closing brace", "} // end of the block", "} "},

		// A comment marker is only a comment marker where it is not something the
		// statement was already saying.
		{"a URL in a string", `typeText("http://example.com")`, `typeText("http://example.com")`},
		{"a URL and a comment", `typeText("http://x") // the site`, `typeText("http://x") `},
		{"a star-slash in a string", `typeText("a /* b")`, `typeText("a /* b")`},
		{"an escaped quote before one", `typeText("say \"hi\"") // greet`, `typeText("say \"hi\"") `},
		{"a slash-slash in an instruction", `move(:: the x offset to // ::, 0)`, `move(:: the x offset to // ::, 0)`},
		{"a comment after a slot", `move(:: the x offset ::, 0) // to Save`, `move(:: the x offset ::, 0) `},
		{"a quote inside an instruction", `typeText(:: what to say, like "hi" ::) // reply`, `typeText(:: what to say, like "hi" ::) `},
		{"a slot inside a string", `typeText("Hi :: the name ::") // greet`, `typeText("Hi :: the name ::") `},
		{"a string that is never closed", `typeText("abc // def`, `typeText("abc // def`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := stripComments(tt.line, false); got != tt.want {
				t.Errorf("stripComments(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

// A block comment runs until something closes it, however many lines that takes,
// and the lines it covers stay where they are.
func TestStripCommentsAcrossLines(t *testing.T) {
	macro := `click()
/* everything under here
   is a note, including
   move(1, 2) */
drag(3, 4)`

	want := []string{"click()", "", "", "", "drag(3, 4)"}
	got := codeLines(macro)

	if len(got) != len(want) {
		t.Fatalf("codeLines returned %d lines, want %d — a statement is found by its line number", len(got), len(want))
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != want[i] {
			t.Errorf("line %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

// Code on the line a block comment opens on, and on the line it closes on, is
// still code.
func TestStripCommentsAroundABlock(t *testing.T) {
	macro := `click() /* a note
that runs on */ drag(3, 4)`

	got := codeLines(macro)
	if len(got) != 2 {
		t.Fatalf("codeLines returned %d lines, want 2", len(got))
	}
	if strings.TrimSpace(got[0]) != "click()" {
		t.Errorf("line 1 = %q, want the statement in front of the comment", got[0])
	}
	if strings.TrimSpace(got[1]) != "drag(3, 4)" {
		t.Errorf("line 2 = %q, want the statement after the comment closed", got[1])
	}
}

// A comment is a line the parse never sees, and the statements around it stand.
func TestParseMacroComments(t *testing.T) {
	checkParse(t, `// what this macro does
move(398, 915)   // to the Save button
/* the part that
   is not wanted today:
   click()
*/
drag(-775, -615)`, []string{
		"move(398, 915)",
		"drag(-775, -615)",
	})
}

// A block is closed by the } under it whether or not there is a comment on the
// end of either line.
func TestParseMacroCommentsInBlocks(t *testing.T) {
	checkParse(t, `if (true) {   // only sometimes
	click()   // the button
}   // done
loop (2) {   /* twice */
	// nothing but a note
	drag(1, 2)
}`, []string{
		"if (true)",
		"  click()",
		"loop (2)",
		"  drag(1, 2)",
	})
}

// The statement is what is left of the line, so a comment on the end of one does
// not make it a line that cannot be read.
func TestACommentedStatementStillReads(t *testing.T) {
	nodes := parseMacro(`typeText("http://example.com")   // a URL, not a comment`)
	if len(nodes) != 1 {
		t.Fatalf("parsed %d statements, want 1", len(nodes))
	}
	if len(nodes[0].args) != 1 || nodes[0].args[0] != "http://example.com" {
		t.Errorf("typeText got %q, want the whole URL", nodes[0].args)
	}
}

// A comment is never a statement waiting on an answer, so nothing in one is what
// psl reaches for.
func TestCommentSlotsAreNotFilled(t *testing.T) {
	tests := []struct {
		name  string
		macro string
	}{
		{"a commented-out statement", "// move(:: the x offset ::, 0)\nclick()"},
		{"a note on the end of a statement", "click() // :: which button was it ::\nmove(1, 2)"},
		{"a block comment", "/*\n move(:: the x offset ::, 0)\n*/\nclick()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := newMacroRun("test", "macro.psl", tt.macro, "")
			if psl.CompilerHasSlot(run.source) {
				slot, _ := psl.FindCompilerSlot(run.source, 0)
				t.Errorf("psl would fill %q, which is written in a comment: %q", slot.Instruction, run.source)
			}
		})
	}
}

// The half-written marker, which is the one that matters. `// TODO :: fix` holds
// no slot of its own — nothing on that line closes it — but psl does not read a
// file a line at a time, and the next real slot below would close it: what gets
// filled is the span from the middle of a comment down into a live statement.
func TestAHalfMarkerInACommentDoesNotSwallowTheStatementBelow(t *testing.T) {
	macro := "// TODO :: put the offset back\nmove(:: the x offset ::, 0)"
	run := newMacroRun("test", "macro.psl", macro, "")

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("nothing left for psl to fill, want the statement's own slot")
	}
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the statement's own slot", slot.Instruction)
	}
	if line, ok := liveSlotLine(run.source); !ok || line != 1 {
		t.Errorf("psl would fill a slot on line %d, want line 2", line+1)
	}
}

// The comment text itself stays: what it says about the screen is the sort of
// thing a model filling a slot two lines down has no other way of knowing.
func TestCommentsSurviveNeutralizing(t *testing.T) {
	macro := "// the Save button is bottom right\nmove(:: the x offset ::, 0)"
	run := newMacroRun("test", "macro.psl", macro, "")

	if !strings.Contains(run.source, "the Save button is bottom right") {
		t.Errorf("the comment did not survive: %q", run.source)
	}
}

// Every statement is found by its line number, so taking the markers out of a
// comment must not take a line with them.
func TestNeutralizingCommentsKeepsTheLineCount(t *testing.T) {
	macro := "// :: one ::\nclick()\n/* :: two ::\n:: three :: */\nmove(1, 2)"
	got := neutralizeComments(macro)

	if want, have := strings.Count(macro, "\n"), strings.Count(got, "\n"); have != want {
		t.Errorf("neutralizing left %d newlines, want %d", have, want)
	}
}

// A loop puts its statements back the way they were written before every pass.
// What it must not put back is a comment's markers, which are not a question
// anyone is waiting on the answer to.
func TestALoopDoesNotRestoreCommentSlots(t *testing.T) {
	macro := `loop (2) {
	move(:: the x offset ::, 0)   // :: which button ::
}`
	nodes := parseMacro(macro)
	run := newMacroRun("test", "macro.psl", macro, "")

	run.record(nodes[0].body[0].line, "move(-120, 0)   <which button>")
	run.restore(nodes[0].line)
	run.restoreBlock(nodes[0].body)

	slot, found := psl.FindCompilerSlot(run.source, 0)
	if !found {
		t.Fatal("the restored macro holds nothing psl would fill")
	}
	if slot.Instruction != "the x offset" {
		t.Errorf("psl would fill %q, want the statement's own slot asked again", slot.Instruction)
	}
}

// A statement psl filled comes back with its comment still on the end of it, and
// has to read as a statement all the same — a call ends at its closing
// parenthesis, and a note after that would otherwise be a line Pob cannot read.
func TestAFilledStatementWithACommentReadsBack(t *testing.T) {
	tests := []struct {
		filled string
		name   string
		args   []string
	}{
		{`move(-120, 40)   // to the Save button`, "move", []string{"-120", "40"}},
		{`typeText("Hello")   /* the reply */`, "typeText", []string{"Hello"}},
		{`stop()   // nothing below this is wanted`, "stop", nil},
	}
	for _, tt := range tests {
		name, args, ok := parseMacroLine(strings.TrimSpace(stripLine(tt.filled)))
		if !ok {
			t.Errorf("%q did not read as a statement", tt.filled)
			continue
		}
		if name != tt.name || len(args) != len(tt.args) {
			t.Errorf("%q = (%q, %q), want (%q, %q)", tt.filled, name, args, tt.name, tt.args)
			continue
		}
		for i := range args {
			if args[i] != tt.args[i] {
				t.Errorf("%q arg %d = %q, want %q", tt.filled, i, args[i], tt.args[i])
			}
		}
	}
}

// The same for a block header, which ends at its { and is read back out of the
// whole line after psl has filled its condition.
func TestAFilledConditionWithACommentReadsBack(t *testing.T) {
	ifNode := macroNode{isIf: true}
	if got := readCondition(ifNode, `if (true) {   // when a dialog is up`); got != "true" {
		t.Errorf("readCondition = %q, want %q", got, "true")
	}
	loopNode := macroNode{isLoop: true}
	if got := readCondition(loopNode, `loop (false, 5) {   /* while it is open */`); got != "false" {
		t.Errorf("readCondition = %q, want %q", got, "false")
	}
}

// A comment cannot turn a statement into one Pob will not run, and a commented-
// out one cannot turn into a statement it will.
func TestCommentsDoNotChangeWhatRuns(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, `sleep(1ms)   // the first
// sleep(9ms)
/* sleep(8ms)
   sleep(7ms) */
sleep(2ms)`)

	checkRan(t, m.ran(t), []string{"1", "2"})
}

// A call someone commented out is not a file that runs, and not a file the check
// for psl reads.
func TestACommentedCallIsNotMade(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared.psl", "sleep(9ms)")
	m.replay(t, "sleep(1ms)\n// call(\"../shared.psl\")\nsleep(2ms)")

	checkRan(t, m.ran(t), []string{"1", "2"})
}

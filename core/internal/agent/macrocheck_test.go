package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// checkLines is what the check says about a macro, one problem per line, so a
// test can state which lines it expects to be told about and what about them.
func checkLines(t *testing.T, macro string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "macro.psl")
	if err := os.WriteFile(path, []byte(macro), 0o644); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range checkMacroSource(macro, path) {
		out = append(out, p.String())
	}
	return out
}

// wantProblems states the lines a macro is wrong on and a fragment of what the
// check says about each, in the order they are written.
func wantProblems(t *testing.T, macro string, want ...string) {
	t.Helper()
	got := checkLines(t, macro)
	if len(got) != len(want) {
		t.Fatalf("found %d problems, want %d:\ngot  %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Errorf("problem %d = %q, want it to mention %q", i, got[i], want[i])
		}
	}
}

// A macro that says what it means is one the check has nothing to say about,
// slots and all — the check is about the file and never about what psl will
// answer.
func TestCheckPassesAGoodMacro(t *testing.T) {
	wantProblems(t, `// Reply to whatever is waiting in the chat window.
move(398, 915)   // the message box
click()
drag(-775, -615)
if (:: the window focus on a wechat user ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply ::)
}
loop (:: another unread message ::, 10) {
    keyPress("return")
    sleep(500)
}
loop (false, 3) {
    stop
}
take_screenshot()
take_screenshot(0, 0, 100, 100)
typeText("say \"hi\"")
typeText("a, b")
resetCursor()
stop()
`)
}

// A slot written where the whole argument list goes is answered with the whole
// list, so one of them stands for as many arguments as the call takes. The check
// has nothing to say about it: `-120, 40` leaves a statement that reads as PSL,
// and rescaleFilled grows both halves of it.
func TestCheckAllowsASlotForAWholeArgumentList(t *testing.T) {
	wantProblems(t, `if (::zed is running::) {
    move(::the profile icon::)
    click()
}
take_screenshot(:: the region the dialog is in ::)
`)
}

// What a fill cannot do is take arguments away, so a statement already written
// with more than the call can hold is wrong however its slots are answered.
func TestCheckCatchesTooManyArgumentsAroundASlot(t *testing.T) {
	wantProblems(t, `move(:: the x offset ::, 40, 60)
click(:: where ::)
`,
		"move takes 2 arguments, and 3 were written already",
		"click takes no arguments, and 1 was written already",
	)
}

// Too few is still too few when nothing in the statement could grow. Markers
// touching a digit are not a slot — psl's own rule — so the second line here is
// a call with one argument that is not a number, and neither half of that is
// waiting on an answer.
func TestCheckCatchesTooFewArguments(t *testing.T) {
	wantProblems(t, `move(915)
move(1::how much further::)
drag(-775)
`,
		"line 1: move takes 2 arguments, and 1 was written",
		"line 2: move takes 2 arguments, and 1 was written",
		"line 3: drag takes 2 arguments, and 1 was written",
	)
}

func TestCheckCatchesArgumentCounts(t *testing.T) {
	wantProblems(t, `click(1, 2)
typeText("a", "b")
call()
take_screenshot(1, 2)
sleep()
`,
		"click takes no arguments, and 2 were written",
		"typeText takes 1 argument, and 2 were written",
		"call takes 1 argument, and none was written",
		"take_screenshot takes all 4 arguments or none at all, and 2 were written",
		"sleep takes 1 argument, and none was written",
	)
}

func TestCheckCatchesArgumentsThatAreNotNumbers(t *testing.T) {
	wantProblems(t, `scroll(a, b)
move(1, "2")
sleep(0.5)
drag(:: how far left ::, -10)
`,
		`scroll wants numbers, and its first argument is "a"`,
		`move wants numbers, and its second argument is "\"2\""`,
	)
}

// A slot is a value that is not there yet, so what is checked around one is
// everything the fill cannot change: which call it is, and how many arguments it
// was written with. The commas inside an instruction are the instruction's.
func TestCheckReadsSlotsAsOneArgument(t *testing.T) {
	wantProblems(t, `move(:: left, or right, or neither ::, 0)
loop (:: still loading, not empty ::, 4) {
    sleep(100)
}
typeText("Hi :: the name at the top ::, thanks!")
`)
}

func TestCheckCatchesUnknownStatements(t *testing.T) {
	wantProblems(t, `clik()
Move(1, 2)
STOP
halt
`,
		`there is no statement called "clik"`,
		`there is no statement called "Move" — see the Calls table in docs/03_Macro PSL.md. Did you mean move? Names are case-sensitive`,
		`"STOP" is not a statement — stop is spelled lowercase`,
		`"halt" is not a statement — a call is name(argument, argument)`,
	)
}

func TestCheckCatchesBrokenBlocks(t *testing.T) {
	wantProblems(t, `if (something) {
    click()
}
loop (2.5) {
    click()
}
loop (:: how many ::) {
    click()
}
}
if (:: a dialog is up ::) {
    click()
`,
		"line 1: if wants a condition in parentheses",
		"line 4: loop wants a whole count",
		"line 7: loop wants a whole count",
		"line 10: } closes a block that was never opened",
		"line 11: the block opened here is never closed",
	)
}

// A `/*` with no `*/` under it takes every statement below it, so the macro
// still parses and there is almost nothing left of it. Worth its own line for
// how much of a file it costs.
func TestCheckCatchesUnclosedBlockComment(t *testing.T) {
	wantProblems(t, `click()
/* left out until the dialog settles down:
   typeText("Hi")
click()
`, "line 2: /* is never closed by a */")
}

// A comment that closes mid-line leaves two statements on one, which is one
// line and not one statement. It reads as a call with arguments nobody wrote.
func TestCheckCatchesTwoStatementsOnALine(t *testing.T) {
	wantProblems(t, `click() /* why */ move(1, 2)
`, "line 1: click takes no arguments, and 2 were written")
}

// A marker that is part of what is being typed is not a slot, and the statement
// around it is the ordinary one it looks like.
func TestCheckLeavesTouchingMarkersAlone(t *testing.T) {
	wantProblems(t, `typeText("std::cout")
typeText("a::b::c")
`)
}

// Nothing in a comment is a statement, so nothing in one is checked — including
// the statement someone commented out rather than deleted.
func TestCheckIgnoresCommentedOutStatements(t *testing.T) {
	wantProblems(t, `click()
// move(1)
/* clik()
   typeText("a", "b") */
drag(1, 2)   // and a note about scroll(a, b)
`)
}

func TestCheckFollowsCalledFiles(t *testing.T) {
	dir := t.TempDir()
	called := filepath.Join(dir, "sign-in.psl")
	if err := os.WriteFile(called, []byte("click()\nmove(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "macro.psl")
	got := checkMacroSource("call(\"sign-in.psl\")\ncall(\"missing.psl\")\n", path)

	if len(got) != 2 {
		t.Fatalf("found %d problems, want 2: %v", len(got), got)
	}
	if got[0].File != "sign-in.psl" || got[0].Line != 2 {
		t.Errorf("first problem = %q, want it against sign-in.psl line 2", got[0])
	}
	if !strings.Contains(got[0].String(), "move takes 2 arguments") {
		t.Errorf("first problem = %q, want the called file's bad move", got[0])
	}
	if !strings.Contains(got[1].String(), "there is no such file") {
		t.Errorf("second problem = %q, want the missing file", got[1])
	}
}

// A file that reaches itself is a replay with no end in it. The check says so
// rather than following it round.
func TestCheckCatchesACallThatComesBackRound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "macro.psl")
	other := filepath.Join(dir, "other.psl")
	if err := os.WriteFile(other, []byte(`call("macro.psl")`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkMacroSource(`call("other.psl")`, path)
	if len(got) != 1 || !strings.Contains(got[0].String(), "already running above it") {
		t.Fatalf("got %v, want the ring reported once", got)
	}
}

// A call whose path is a slot cannot be followed and does not need to be: what
// it names is not known until the replay reaches it.
func TestCheckLeavesASlotPathAlone(t *testing.T) {
	wantProblems(t, `call(:: which file to run ::)`)
}

// A macro is read against the file it is about, so its problems come back in
// the order its lines are written — whichever of the passes found them.
func TestCheckReportsInLineOrder(t *testing.T) {
	got := checkLines(t, `clik()
if (nonsense) {
    move(1)
}
}
`)
	want := []int{1, 2, 3, 5}
	if len(got) != len(want) {
		t.Fatalf("found %d problems, want %d: %v", len(got), len(want), got)
	}
	for i, line := range want {
		if !strings.HasPrefix(got[i], "line "+strconv.Itoa(line)+":") {
			t.Errorf("problem %d = %q, want it against line %d", i, got[i], line)
		}
	}
}

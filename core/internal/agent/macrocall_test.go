package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pob/core/internal/applog"
	"pob/core/internal/config"
	"pob/core/internal/psl"
	"pob/core/internal/storage"
)

// The two statements a macro makes about the run itself — stop() and call() — are
// the ones that never reach the machine, so they replay under a Runner with no
// shell behind it. That is what these tests are: the real runMacroNodes over a
// real macro, with nothing faked but the ~/.pob it works in.
//
// What ran is read back out of the log, and a sleep of its own length is how a
// statement says which one it is: sleep(1ms) logs `sleep(1ms)` and nothing else
// in the macro does. Milliseconds, so that a test replaying a dozen of them
// still takes no time.

// macroTest is a Runner over a temporary ~/.pob, the directory its macro.psl
// would sit in, and the log everything it does is written to — instance.log,
// which is where a replay's detail goes. app.log keeps only the app and its
// instances starting, stopping and failing.
type macroTest struct {
	runner      *Runner
	dir         string // the instance directory, where macro.psl lives
	root        string // ~/.pob itself, which is one up — where call("../x.psl") lands
	instanceLog string
}

func newMacroTest(t *testing.T) *macroTest {
	t.Helper()
	root := t.TempDir()

	// Written before the config reads it: the delay between one statement and the
	// next is a second by default, and these tests replay a dozen statements.
	settings := `{"macro_default_delay": 0}`
	if err := os.WriteFile(filepath.Join(root, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	applog.Init(root)

	cfg := config.New(root, "pob-test")
	store := storage.New(root, "pob-test", cfg.SettingsDict, cfg.Macro)
	// The same sink pob-core sets: what the runner logs is the instance's, so
	// it lands in instance.log whatever its level.
	applog.SetInstanceSink(func(level, message string) {
		store.LogInstance(level, message)
	})
	return &macroTest{
		runner:      NewRunner(cfg, store, psl.Compiler{}, nil),
		dir:         cfg.InstanceDir(),
		root:        root,
		instanceLog: store.InstanceLogFile(),
	}
}

// write puts a PSL file next to the macro, or anywhere under ~/.pob that a
// relative path names.
func (m *macroTest) write(t *testing.T, name, source string) string {
	t.Helper()
	path := filepath.Join(m.root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// replay runs a macro the way runMacro does, minus the screen: the statements
// are parsed, the lines no statement came out of are spent, and the whole thing
// is replayed from the macro's own directory.
func (m *macroTest) replay(t *testing.T, source string) *macroRun {
	t.Helper()
	path := filepath.Join(m.dir, "macro.psl")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes := parseMacro(source)
	run := newMacroRun("test", path, source, "")
	run.spendUncovered(nodes)
	m.runner.runMacroNodes(context.Background(), run, nodes)
	return run
}

// ran reports the sleeps the replay reached, in the order it reached them, so a
// test can state the path it expects the run to have taken.
func (m *macroTest) ran(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(m.instanceLog)
	if err != nil {
		t.Fatalf("nothing was logged: %v", err)
	}
	var marks []string
	for _, line := range strings.Split(string(data), "\n") {
		if _, mark, ok := strings.Cut(line, "Macro sleep("); ok {
			marks = append(marks, strings.TrimSuffix(mark, "ms)"))
		}
	}
	return marks
}

func (m *macroTest) logged(t *testing.T, want string) bool {
	t.Helper()
	data, err := os.ReadFile(m.instanceLog)
	return err == nil && strings.Contains(string(data), want)
}

func checkRan(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the replay ran %v, want %v", got, want)
	}
}

func TestEveryExecutedStatementIsWrittenToTheInstanceLog(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "sleep(1ms)\nstop()\nsleep(2ms)")

	data, err := os.ReadFile(m.instanceLog)
	if err != nil {
		t.Fatalf("reading instance.log: %v", err)
	}
	log := string(data)
	for _, want := range []string{
		`STEP START line=1`,
		`kind="sleep" statement="sleep(1ms)"`,
		`STEP END line=1`,
		`STEP START line=2`,
		`kind="stop" statement="stop()"`,
		`STEP END line=2`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("instance.log does not contain %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, `statement="sleep(2ms)"`) {
		t.Errorf("instance.log recorded a statement after stop():\n%s", log)
	}
	if strings.Contains(log, "STEP START session=") || strings.Contains(log, "STEP END session=") ||
		strings.Contains(log, "STEP START file=") || strings.Contains(log, "STEP END file=") {
		t.Errorf("step events repeat macro-level fields:\n%s", log)
	}
}

func TestLoopEventsDoNotRepeatTheMacroSession(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "loop (2) {\n\tsleep(1ms)\n}")

	data, err := os.ReadFile(m.instanceLog)
	if err != nil {
		t.Fatalf("reading instance.log: %v", err)
	}
	log := string(data)
	for _, event := range []string{"LOOP START", "LOOP PASS", "LOOP STOP"} {
		if !strings.Contains(log, event+` line=1`) {
			t.Errorf("instance.log has no %s event with its line:\n%s", event, log)
		}
		if strings.Contains(log, event+" session=") || strings.Contains(log, event+" file=") {
			t.Errorf("%s repeats macro-level fields:\n%s", event, log)
		}
	}
}

// stop() ends the run where it stands: not the statements after it, and not the
// rest of the block it is in.
func TestStopEndsTheRun(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, `sleep(1ms)
if (true) {
	sleep(2ms)
	stop()
	sleep(3ms)
}
sleep(4ms)`)

	checkRan(t, m.ran(t), []string{"1", "2"})
}

// A loop is not the exception: stop() is immediate inside one too, rather than
// ending the pass and going round again.
func TestStopEndsALoop(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, `loop (5) {
	sleep(1ms)
	stop()
}
sleep(2ms)`)

	checkRan(t, m.ran(t), []string{"1"})
}

// An else is the block that runs when the condition does not hold, and exactly
// one of the two runs either way. The statements under the whole thing run
// whichever it was.
func TestAnElseRunsWhenTheConditionDoesNot(t *testing.T) {
	for _, tt := range []struct {
		condition string
		want      []string
	}{
		{"true", []string{"1", "3"}},
		{"false", []string{"2", "3"}},
	} {
		m := newMacroTest(t)
		m.replay(t, `if (`+tt.condition+`) {
	sleep(1ms)
} else {
	sleep(2ms)
}
sleep(3ms)`)
		checkRan(t, m.ran(t), tt.want)
	}
}

// A chain asks one condition at a time and stops at the first that holds: the
// block under it runs, and nothing below it is asked at all.
func TestAChainRunsTheFirstBlockWhoseConditionHolds(t *testing.T) {
	chain := func(a, b string) string {
		return `if (` + a + `) {
	sleep(1ms)
} else if (` + b + `) {
	sleep(2ms)
} else {
	sleep(3ms)
}
sleep(4ms)`
	}
	for _, tt := range []struct {
		a, b string
		want []string
	}{
		{"true", "true", []string{"1", "4"}},
		{"true", "false", []string{"1", "4"}},
		{"false", "true", []string{"2", "4"}},
		{"false", "false", []string{"3", "4"}},
	} {
		m := newMacroTest(t)
		m.replay(t, chain(tt.a, tt.b))
		checkRan(t, m.ran(t), tt.want)
	}
}

// A condition Pob cannot read is no verdict at all, and an if written with an
// else runs neither block rather than reading the non-answer as a no. It is what
// a filled condition that came back as neither word leaves behind, put into the
// statement here rather than asked for — the replay reads the two the same way.
func TestAnUnreadableConditionRunsNeitherBlock(t *testing.T) {
	m := newMacroTest(t)
	macro := `if (true) {
	sleep(1ms)
} else {
	sleep(2ms)
}
sleep(3ms)`
	nodes := parseMacro(macro)
	nodes[0].condition = "probably"

	run := newMacroRun("test", filepath.Join(m.dir, "macro.psl"), macro, "")
	m.runner.runMacroNodes(context.Background(), run, nodes)

	checkRan(t, m.ran(t), []string{"3"})
	if !m.logged(t, "is not true or false — skipping both blocks") {
		t.Error("the log does not say the else was skipped with the block above it")
	}
}

// stop() inside an else ends the run where it stands, the same as it does in the
// block above it.
func TestStopInsideAnElseEndsTheRun(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, `if (false) {
	sleep(1ms)
} else {
	sleep(2ms)
	stop()
	sleep(3ms)
}
sleep(4ms)`)

	checkRan(t, m.ran(t), []string{"2"})
}

// An if inside a loop is asked again on every pass, and the else is the block a
// pass runs when the answer that pass gave was no.
func TestAnElseInsideALoopRunsOnEveryPassThatNeedsIt(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, `loop (2) {
	if (false) {
		sleep(1ms)
	} else {
		sleep(2ms)
	}
}`)

	checkRan(t, m.ran(t), []string{"2", "2"})
}

// call replays another file where it stands, and comes back to the statement
// under it.
func TestCallRunsAnotherFile(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared.psl", "sleep(2ms)\nsleep(3ms)")
	m.replay(t, "sleep(1ms)\ncall(\"../shared.psl\")\nsleep(4ms)")

	checkRan(t, m.ran(t), []string{"1", "2", "3", "4"})
}

// A relative path is relative to the file the call is written in, so a call in a
// called file names its own neighbours rather than the macro's.
func TestACalledFileResolvesItsOwnCalls(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared/outer.psl", "sleep(2ms)\ncall(\"inner.psl\")")
	m.write(t, "shared/inner.psl", "sleep(3ms)")
	m.replay(t, "call(\"../shared/outer.psl\")\nsleep(4ms)")

	checkRan(t, m.ran(t), []string{"2", "3", "4"})
}

// stop() ends the replay, not the file the statement is in: the file that called
// it does not go on to its next statement.
func TestStopInACalledFileEndsEverything(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared.psl", "sleep(2ms)\nstop()\nsleep(3ms)")
	m.replay(t, "sleep(1ms)\ncall(\"../shared.psl\")\nsleep(4ms)")

	checkRan(t, m.ran(t), []string{"1", "2"})
}

// The file is read where the call is reached, so a call inside a loop replays it
// on every pass.
func TestCallRunsTheFileOnEveryPass(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared.psl", "sleep(2ms)")
	m.replay(t, "loop (3) {\n\tcall(\"../shared.psl\")\n}")

	checkRan(t, m.ran(t), []string{"2", "2", "2"})
}

// A file that reaches itself is a replay with no end in it. The call is refused
// rather than made, and the statements around it stand — the same thing that
// happens to any other line Pob will not run. A test that got this wrong would
// not fail; it would never finish.
func TestCallRefusesAFileThatIsAlreadyRunning(t *testing.T) {
	m := newMacroTest(t)
	path := m.write(t, "shared.psl", "sleep(2ms)\ncall(\"shared.psl\")\nsleep(3ms)")
	m.replay(t, "call(\"../shared.psl\")\nsleep(4ms)")

	checkRan(t, m.ran(t), []string{"2", "3", "4"})
	if !m.logged(t, path+" is already running") {
		t.Error("the log does not say why the call was refused")
	}
}

// A ring of files is the same thing said the long way round.
func TestCallRefusesARingOfFiles(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "a.psl", "sleep(2ms)\ncall(\"b.psl\")")
	m.write(t, "b.psl", "sleep(3ms)\ncall(\"a.psl\")")
	m.replay(t, "call(\"../a.psl\")\nsleep(4ms)")

	checkRan(t, m.ran(t), []string{"2", "3", "4"})
}

// A file that is not there is a statement that cannot run, which is a line in
// the log and nothing else — the statements around it are still played.
func TestCallOfAMissingFileIsSkipped(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "sleep(1ms)\ncall(\"../nowhere.psl\")\nsleep(2ms)")

	checkRan(t, m.ran(t), []string{"1", "2"})
	if !m.logged(t, "cannot read") {
		t.Error("the log does not say the file could not be read")
	}
}

// The replay refuses a quoted time the same way the check does, which is what
// keeps the two readings from coming apart: a statement psl filled with `"5ms"`
// is skipped rather than waited out, because the check never saw that line and
// something has to.
func TestAQuotedTimeIsSkippedByTheReplay(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "sleep(1ms)\nsleep(\"5ms\")\nsleep(2ms)")

	checkRan(t, m.ran(t), []string{"1", "2"})
	if !m.logged(t, "a time is not a string") {
		t.Error("the log does not say why the quoted time was skipped")
	}
}

// The bound on a chain of files that each call a new one, which nothing else
// catches: every file is different, so none of them is already running.
func TestCallStopsAtTheDepthBound(t *testing.T) {
	m := newMacroTest(t)
	// Each file sleeps its own number of milliseconds and calls the next, so the
	// sleeps that were logged are exactly the files that ran.
	for i := 1; i <= maxCallDepth+3; i++ {
		source := fmt.Sprintf("sleep(%dms)\ncall(\"f%d.psl\")", i, i+1)
		m.write(t, fmt.Sprintf("f%d.psl", i), source)
	}
	m.replay(t, `call("../f1.psl")`)

	// The macro is depth 0 and f1 is depth 1, so the last file to run is the one
	// at maxCallDepth.
	var want []string
	for i := 1; i <= maxCallDepth; i++ {
		want = append(want, fmt.Sprintf("%d", i))
	}
	checkRan(t, m.ran(t), want)
}

// A relative path is relative to the file the call is written in; ~/ is the home
// directory, and an absolute path is itself.
func TestResolveCallPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory to resolve ~/ against")
	}
	dir := filepath.Join(home, "somewhere", "pob-test")

	tests := []struct{ arg, want string }{
		{"other.psl", filepath.Join(dir, "other.psl")},
		{"../shared.psl", filepath.Join(home, "somewhere", "shared.psl")},
		{"  ../shared.psl  ", filepath.Join(home, "somewhere", "shared.psl")},
		{"sub/other.psl", filepath.Join(dir, "sub", "other.psl")},
		{"~/macros/other.psl", filepath.Join(home, "macros", "other.psl")},
		{filepath.Join(home, "other.psl"), filepath.Join(home, "other.psl")},
	}
	for _, tt := range tests {
		got, err := resolveCallPath(dir, tt.arg)
		if err != nil {
			t.Errorf("resolveCallPath(%q) failed: %v", tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("resolveCallPath(%q) = %q, want %q", tt.arg, got, tt.want)
		}
	}
	if _, err := resolveCallPath(dir, "   "); err == nil {
		t.Error("resolveCallPath accepted an empty path, want an error")
	}
}

// The check for psl runs over the whole macro before the cursor moves, and a
// call() is part of the macro: a file it brings in is read now rather than found
// out about thirty statements in.
func TestMacroNeedsPSLFollowsCalls(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "plain.psl", "click()\nsleep(1ms)")
	m.write(t, "asks.psl", "move(:: the x offset to Save ::, 0)")
	m.write(t, "hop.psl", `call("asks.psl")`)
	// A ring, which the check has to walk once rather than round and round.
	m.write(t, "ring-a.psl", "click()\ncall(\"ring-b.psl\")")
	m.write(t, "ring-b.psl", "click()\ncall(\"ring-a.psl\")")

	tests := []struct {
		name  string
		macro string
		want  bool
	}{
		{"nothing asks", `click()`, false},
		{"the macro asks", `move(:: the x offset ::, 0)`, true},
		{"a called file that asks nothing", `call("../plain.psl")`, false},
		{"a called file that asks", `call("../asks.psl")`, true},
		{"a file called by a called file", `call("../hop.psl")`, true},
		{"a file that is not there", `call("../nowhere.psl")`, false},
		{"a ring", `call("../ring-a.psl")`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(m.dir, "main.macro.psl")
			nodes := parseMacro(tt.macro)
			got := macroNeedsPSL(nodes, path, map[string]bool{path: true})
			if got != tt.want {
				t.Errorf("macroNeedsPSL = %v, want %v", got, tt.want)
			}
		})
	}
}

// A block a statement slot filled to is replayed the way a called file is: a
// file of its own, with its own line numbers, under the run whose line asked for
// it. What it shares with that run is the session, the directory a relative path
// is resolved against, and the stop that ends every file at once.
func TestAGeneratedBlockReplaysLikeACalledFile(t *testing.T) {
	m := newMacroTest(t)
	m.write(t, "shared.psl", "sleep(3ms)")

	parent := newMacroRun("test", filepath.Join(m.dir, "macro.psl"), "sleep(1ms)\n:: do the thing ::", "")
	block := "sleep(2ms)\ncall(\"../shared.psl\")\nsleep(4ms)"
	generated := newGeneratedRun(parent, 2, block)
	m.runner.runMacroNodes(context.Background(), generated, parseMacro(block))

	checkRan(t, m.ran(t), []string{"2", "3", "4"})
	if generated.name != "macro-line2.psl" {
		t.Errorf("the block is called %q, want it named for the line that asked", generated.name)
	}
	if generated.dir != parent.dir {
		t.Errorf("the block resolves paths against %q, want the asking file's %q", generated.dir, parent.dir)
	}
	if generated.depth() != 1 {
		t.Errorf("the block is %d files deep, want 1 — it counts towards the bound the same way a call does", generated.depth())
	}
}

// stop() ends the whole replay wherever it is written, and a generated block is
// no different from a called file about that.
func TestStopInAGeneratedBlockEndsEverything(t *testing.T) {
	m := newMacroTest(t)

	parent := newMacroRun("test", filepath.Join(m.dir, "macro.psl"), ":: do the thing ::", "")
	block := "sleep(1ms)\nstop()\nsleep(2ms)"
	generated := newGeneratedRun(parent, 1, block)
	m.runner.runMacroNodes(context.Background(), generated, parseMacro(block))

	checkRan(t, m.ran(t), []string{"1"})
	if !parent.halted() {
		t.Error("a stop inside a generated block left the run that asked for it going")
	}
}

// A generated block is replayed inside the file that asked for it, so a call()
// written in one that reaches that file back is the replay with no end in it
// that a file calling itself would be.
func TestACallInAGeneratedBlockCannotReachTheFileThatAskedForIt(t *testing.T) {
	m := newMacroTest(t)

	source := ":: do the thing ::"
	path := filepath.Join(m.dir, "macro.psl")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := newMacroRun("test", path, source, "")
	block := "call(\"macro.psl\")\nsleep(2ms)"
	generated := newGeneratedRun(parent, 1, block)
	m.runner.runMacroNodes(context.Background(), generated, parseMacro(block))

	checkRan(t, m.ran(t), []string{"2"})
	if !m.logged(t, "is already running and would call itself") {
		t.Error("the call was not refused as one reaching a file already running")
	}
}

// The macro a session writes out is the program that ran, one statement per
// line — so a generated block goes back to being lines in it, under the
// indentation of the line that asked for it, and a block generated inside one
// goes in with it.
func TestTheCompiledMacroOpensGeneratedBlocksBackOut(t *testing.T) {
	macro := "click(120, 300)\nif (true) {\n\t<make the two clicks>\n}\nsleep(2s)"
	run := &macroRun{sessionID: "test", source: macro, written: strings.Split(macro, "\n")}

	// What the replay leaves behind: the line with the block folded onto it, and
	// the block kept beside it. The indentation comes from the line as it was
	// written, so it is there whatever psl left of the rest of the line.
	run.record(3, "click(489, 597) click(814, 814)")
	run.recordBlock(3, "click(489, 597)\nclick(814, 814)")

	want := `click(120, 300)
if (true) {
	click(489, 597)
	click(814, 814)
}
sleep(2s)`
	if got := run.compiled(); got != want {
		t.Errorf("compiled() =\n%s\nwant\n%s", got, want)
	}
}

// A statement slot inside a generated block generates a block of its own, and
// what the session writes out is the outer one with the inner one already opened
// out in it — each run's compiled() feeding the one above it.
func TestTheCompiledMacroNestsGeneratedBlocks(t *testing.T) {
	macro := ":: do the thing ::"
	run := &macroRun{sessionID: "test", source: macro, written: strings.Split(macro, "\n")}

	outerSource := "click(0, 0)\n\t:: and then the rest ::"
	outer := &macroRun{sessionID: "test", source: outerSource, written: strings.Split(outerSource, "\n"), parent: run}
	outer.record(2, "\tclick(1, 2) sleep(1ms)")
	outer.recordBlock(2, "click(1, 2)\nsleep(1ms)")

	run.record(1, "click(0, 0) click(1, 2) sleep(1ms)")
	run.recordBlock(1, outer.compiled())

	want := "click(0, 0)\n\tclick(1, 2)\n\tsleep(1ms)"
	if got := run.compiled(); got != want {
		t.Errorf("compiled() =\n%s\nwant\n%s", got, want)
	}
}

// A macro with no generated block in it is the file itself, untouched.
func TestTheCompiledMacroOfAMacroWithNoBlocks(t *testing.T) {
	macro := "click(120, 300)\nmove(-120, 40)"
	run := &macroRun{sessionID: "test", source: macro}
	if got := run.compiled(); got != macro {
		t.Errorf("compiled() = %q, want the file itself %q", got, macro)
	}
}

// What a generated block is called: the file that asked and the line it asked
// on, written as a filename because psl is handed one.
func TestGeneratedName(t *testing.T) {
	for _, tt := range []struct {
		name string
		line int
		want string
	}{
		{"macro.psl", 2, "macro-line2.psl"},
		{"shared.psl", 40, "shared-line40.psl"},
		// A block that generated a block, which the depth bound is what stops.
		{"macro-line2.psl", 1, "macro-line2-line1.psl"},
		{"", 7, "macro-line7.psl"},
	} {
		if got := generatedName(tt.name, tt.line); got != tt.want {
			t.Errorf("generatedName(%q, %d) = %q, want %q", tt.name, tt.line, got, tt.want)
		}
	}
}

// The slot directories of a session are numbered in the order they were filled,
// straight through however many files did the filling — a called file does not
// start again at 1 and write over what the macro left.
func TestSlotsAreNumberedAcrossFiles(t *testing.T) {
	macro := newMacroRun("test", "/pob/macro.psl", "click()", "")
	called := newCallRun(macro, "/pob/shared.psl", "click()")

	got := []int{macro.nextSlot(), called.nextSlot(), called.nextSlot(), macro.nextSlot()}
	for i, n := range got {
		if n != i+1 {
			t.Errorf("fill %d was numbered %d, want %d — the count is the session's", i+1, n, i+1)
		}
	}
}

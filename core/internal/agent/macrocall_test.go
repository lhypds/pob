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
// would sit in, and the log everything it does is written to.
type macroTest struct {
	runner *Runner
	dir    string // the instance directory, where macro.psl lives
	root   string // ~/.pob itself, which is one up — where call("../x.psl") lands
	log    string
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
	return &macroTest{
		runner: NewRunner(cfg, store, psl.Compiler{}, nil),
		dir:    cfg.InstanceDir(),
		root:   root,
		log:    filepath.Join(root, "app.log"),
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
	data, err := os.ReadFile(m.log)
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
	data, err := os.ReadFile(m.log)
	return err == nil && strings.Contains(string(data), want)
}

func checkRan(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the replay ran %v, want %v", got, want)
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
			path := filepath.Join(m.dir, "macro.psl")
			nodes := parseMacro(tt.macro)
			got := macroNeedsPSL(nodes, m.dir, map[string]bool{path: true})
			if got != tt.want {
				t.Errorf("macroNeedsPSL = %v, want %v", got, tt.want)
			}
		})
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

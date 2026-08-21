package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// run() is the third statement that never goes through the shell app — see the
// note at the top of macrocall_test.go — so it replays under the same Runner with
// no screen behind it. What it did is read back out of the log and off the disk:
// a command that wrote a file left one, and one that printed something has its
// words in the log line about it.

// The commands here are written in the shell the core hands them to, which is
// /bin/sh everywhere but Windows. What runs the statement is the same either way;
// what these say is not.
func shellTest(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these commands are /bin/sh's, and Windows gets cmd")
	}
}

// The statement is a command line handed to the shell, and it is run from the
// directory the PSL file is in — so a relative path in one names a file beside
// the macro, the way a call() names a file beside it.
func TestRunHandsTheCommandToTheShellFromTheMacrosDirectory(t *testing.T) {
	shellTest(t)
	m := newMacroTest(t)
	m.replay(t, `run("echo played > ran.txt")`)

	data, err := os.ReadFile(filepath.Join(m.dir, "ran.txt"))
	if err != nil {
		t.Fatalf("the command did not run, or not from the macro's directory: %v", err)
	}
	if got := string(data); got != "played\n" {
		t.Errorf("the file the command wrote holds %q, want %q", got, "played\n")
	}
	if !m.logged(t, `kind="run" statement="run(\"echo played > ran.txt\")" status="completed"`) {
		t.Error("the step was not logged as a completed run")
	}
}

// Waited on, because the statement under it is written after it: what the first
// command wrote is there for the second to read.
func TestRunWaitsForTheCommandToFinish(t *testing.T) {
	shellTest(t)
	m := newMacroTest(t)
	m.replay(t, "run(\"echo one > order.txt\")\nrun(\"cat order.txt\")")

	if !m.logged(t, "it said: one") {
		t.Error("the second command did not see what the first one wrote")
	}
}

// A command's own words are the only thing it says about itself, so they go in
// the log — both streams, on the one line the entry is.
func TestRunLogsWhatTheCommandPrinted(t *testing.T) {
	shellTest(t)
	m := newMacroTest(t)
	m.replay(t, `run("echo out; echo err >&2")`)

	if !m.logged(t, "it said: out err") {
		t.Error("what the command printed is not in the log")
	}
}

// A command that exits non-zero did not do its thing, and that is a step that
// failed rather than one that ran. The replay carries on either way.
func TestRunFailsOnANonZeroExit(t *testing.T) {
	shellTest(t)
	m := newMacroTest(t)
	m.replay(t, "run(\"echo nope >&2; exit 3\")\nrun(\"echo after > after.txt\")")

	if !m.logged(t, `statement="run(\"echo nope >&2; exit 3\")" status="failed"`) {
		t.Error("a command that exited non-zero was not marked failed")
	}
	if !m.logged(t, "it said: nope") {
		t.Error("what the failing command printed is not in the log")
	}
	if _, err := os.Stat(filepath.Join(m.dir, "after.txt")); err != nil {
		t.Errorf("the statement after the failing one did not run: %v", err)
	}
}

// The shape the statement was written for: a watch on the screen, and a sound
// played every time it changes into what the condition holds of.
func TestCheckPassesARunInsideAOnce(t *testing.T) {
	wantProblems(t, `once (:: a new message is on screen ::) {
    run("afplay /System/Library/Sounds/Morse.aiff")
}
`)
}

// Nothing to run is nothing to hand over: the statement is skipped and says so,
// the way every statement skipped for how it was written does.
func TestRunSkipsAnEmptyCommand(t *testing.T) {
	shellTest(t)
	m := newMacroTest(t)
	m.replay(t, "run(\"\")\nrun(\"echo after > after.txt\")")

	if !m.logged(t, "was written with an empty command") {
		t.Error("an empty command was not named in the log")
	}
	if _, err := os.Stat(filepath.Join(m.dir, "after.txt")); err != nil {
		t.Errorf("the statement after it did not run: %v", err)
	}
}

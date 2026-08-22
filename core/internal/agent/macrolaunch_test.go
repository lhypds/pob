package agent

import "testing"

// launch() is the one statement that both reaches outside the window and needs
// the shell app to carry it out: the shell opens the application, finds the
// window and places it, and the core asks for all three in one call and logs
// what came back. So a Runner with no shell behind it — the one these tests
// replay under, see the note at the top of macrocall_test.go — can say what the
// statement does with how it was written and what it does when there is nothing
// to ask, and no more than that. What the fitting itself does is the shells',
// and is tested by running one.

// Nothing to open is nothing to ask for: the statement is skipped and says so,
// the way every statement skipped for how it was written does, and the replay
// carries on to the next one.
func TestLaunchSkipsAnEmptyApplication(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "launch(\"\")\nsleep(1ms)")

	if !m.logged(t, "was written with no application") {
		t.Error("an empty application was not named in the log")
	}
	if !m.logged(t, `statement="launch(\"\")" status="failed"`) {
		t.Error("a launch with nothing to open was not marked failed")
	}
	checkRan(t, m.ran(t), []string{"1"})
}

// A launch that could not be carried out is a step that failed and not a run
// that ended: every coordinate under it is now aimed at a frame the application
// is not in, which is worth a row saying so — and is still not a reason to stop
// replaying the statements around it.
func TestLaunchFailsTheStepWithoutEndingTheRun(t *testing.T) {
	m := newMacroTest(t)
	m.replay(t, "launch(\"Firefox\")\nsleep(1ms)")

	if !m.logged(t, `Macro launch("Firefox")`) {
		t.Error("the statement is not in the log as it was written")
	}
	if !m.logged(t, `statement="launch(\"Firefox\")" status="failed"`) {
		t.Error("a launch that could not be carried out was not marked failed")
	}
	checkRan(t, m.ran(t), []string{"1"})
}

// The shape the statement was written for: the application opened at the top of
// the macro, and the coordinates under it aimed into the frame it was put in.
func TestCheckPassesALaunchAtTheTopOfAMacro(t *testing.T) {
	wantProblems(t, `launch("Firefox")
sleep(3s)
click(398, 915)
typeText("example.com")
keyPress("return")
`)
}

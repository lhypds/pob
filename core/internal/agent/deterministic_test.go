package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkFileLines is checkLines over a file whose name is the point of the test:
// the extension is what says whether psl is behind the file, so which name it is
// written under is part of the case rather than a detail of the fixture.
func checkFileLines(t *testing.T, name, macro string) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(macro), 0o644); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range checkMacroSource(macro, path) {
		out = append(out, p.String())
	}
	return out
}

// A `.macro` is replayed without the compiler, so a slot in one never fills.
// The check names it, because the fix is the file's name and nobody reads a log
// line halfway down a run to find that out.
func TestASlotInADeterministicMacroIsRefused(t *testing.T) {
	problems := checkFileLines(t, "steps.macro", "click()\nmove(:: the x offset ::, 0)\n")
	if len(problems) != 1 {
		t.Fatalf("check said %d things, want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "line 2") {
		t.Errorf("problem = %q, want it against line 2", problems[0])
	}
	if !strings.Contains(problems[0], "steps.macro.psl") {
		t.Errorf("problem = %q, want the .macro.psl name it could be renamed to", problems[0])
	}
}

// A slot inside a block is as unfillable as one at the top level: the walk goes
// all the way down, the way every other pass of the check does.
func TestASlotInsideABlockOfADeterministicMacroIsRefused(t *testing.T) {
	problems := checkFileLines(t, "steps.macro", "loop (2) {\n  click(:: which button ::)\n}\n")
	if len(problems) != 1 {
		t.Fatalf("check said %d things, want 1: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0], "line 2") {
		t.Errorf("problem = %q, want it against line 2", problems[0])
	}
}

// The same file under the `.macro.psl` name is a macro psl fills, and the check
// has nothing to say about it — which is the whole of the difference between the
// two extensions.
func TestTheSameSlotIsFineInAMacroPSL(t *testing.T) {
	if problems := checkFileLines(t, "steps.macro.psl", "click()\nmove(:: the x offset ::, 0)\n"); len(problems) != 0 {
		t.Errorf("check said %v, want nothing", problems)
	}
}

// A `.macro` never asks for psl, however it is written. A macro that called one
// would otherwise be refused on a machine with no compiler installed, over a
// slot that was never going to be filled from it.
func TestADeterministicMacroNeverNeedsPSL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "steps.macro")
	nodes := parseMacro("move(:: the x offset ::, 0)\n")
	if macroNeedsPSL(nodes, path, map[string]bool{path: true}) {
		t.Error("macroNeedsPSL = true for a .macro, want false — it is replayed without the compiler")
	}
}

// And a call() to one does not ask for psl either. The check walks the called
// files by name, so the name is what has to answer this — the file is not read
// twice to find out.
func TestACallToADeterministicMacroDoesNotAskForPSL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "steps.macro"), []byte("move(:: the x offset ::, 0)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.macro.psl")
	nodes := parseMacro(`call("steps.macro")`)
	if macroNeedsPSL(nodes, path, map[string]bool{path: true}) {
		t.Error("macroNeedsPSL = true for a call() to a .macro, want false")
	}
}

// A call() to a `.macro.psl` with a slot in it does ask for psl, which is the
// case the walk exists for: what the second file needs is settled before the
// cursor moves in the first.
func TestACallToAMacroPSLWithASlotAsksForPSL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "steps.macro.psl"), []byte("move(:: the x offset ::, 0)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.macro.psl")
	nodes := parseMacro(`call("steps.macro.psl")`)
	if !macroNeedsPSL(nodes, path, map[string]bool{path: true}) {
		t.Error("macroNeedsPSL = false for a call() to a .macro.psl holding a slot, want true")
	}
}

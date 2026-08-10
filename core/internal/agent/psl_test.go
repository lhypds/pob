package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pob/core/internal/psl"
)

// stubCompiler stands in for the psl executable: it fills the first slot in the
// file with whatever the test last wrote to answer.txt, and prints the progress
// line the real psl prints. Written in sh and awk so the test needs nothing
// installed beyond what a shell already has.
//
// It reads the markers as psl does, spaces or no spaces. A stub that wanted
// them spaced out would pass over every header example and every written-out
// slot and land on the statement by luck rather than by construction — which is
// the one thing these tests are here to check.
func stubCompiler(t *testing.T) (psl.Compiler, func(answer string)) {
	t.Helper()
	dir := t.TempDir()
	answerFile := filepath.Join(dir, "answer.txt")
	stub := filepath.Join(dir, "psl")

	script := `#!/bin/sh
file="$1"
answer=$(cat ` + answerFile + `)
awk -v ans="$answer" '{
  if (!done && match($0, /::[^:]+::/)) {
    $0 = substr($0, 1, RSTART-1) ans substr($0, RSTART+RLENGTH)
    done = 1
  }
  print
}' "$file" > "$file.new" && mv "$file.new" "$file"
echo "psl: macro.psl resolved with stub-model (35 tokens: 31 in, 4 out) — the slot" >&2
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	answer := func(a string) {
		if err := os.WriteFile(answerFile, []byte(a+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return psl.Compiler{Binary: stub, Dir: dir}, answer
}

// The whole way through, statement by statement: psl is handed the macro, fills
// the first slot left in it, and Pob reads the statement back out ready to
// execute and puts the answer into the file the next run is handed.
func TestFillingAMacroThroughTheCompiler(t *testing.T) {
	compiler, answer := stubCompiler(t)

	macro := `click()
move(:: the x offset to the Save button ::, 40)
typeText(:: what to say ::)
if (:: a save dialog is on screen ::) {
	keyPress("return")
}`
	run := &macroRun{sessionID: "test", source: macro}

	tests := []struct {
		line        int
		given, want string
	}{
		{2, "-120", `move(-120, 40)`},
		{3, `"Hello"`, `typeText("Hello")`},
		{4, "true", `if (true) {`},
	}
	for _, tt := range tests {
		answer(tt.given)
		source := run.source
		target := tt.line - 1

		// Whatever else is in the macro, the slot psl reaches for is this
		// statement's — the ones above it have their answers in them by now.
		if live, ok := liveSlotLine(source); !ok || live != target {
			t.Fatalf("line %d: psl would fill a slot on line %d", tt.line, live+1)
		}

		result, err := compiler.Fill(context.Background(), psl.Request{Source: source, Name: "macro.psl"})
		if err != nil {
			t.Fatalf("line %d: %v", tt.line, err)
		}
		got, ok := extractLine(source, result.Source, target)
		if !ok {
			t.Fatalf("line %d: could not read the filled statement back", tt.line)
		}
		if strings.TrimSpace(got) != tt.want {
			t.Errorf("line %d filled to %q, want %q", tt.line, got, tt.want)
		}
		if result.Model != "stub-model" {
			t.Errorf("line %d: model = %q, want it read out of psl's progress line", tt.line, result.Model)
		}
		run.record(tt.line, got)
	}

	if psl.HasSlot(run.source) {
		t.Errorf("the macro still holds a slot: %q", run.source)
	}
}

// A statement with two slots goes through the compiler twice, and the second
// run is asked about a statement that already carries the first answer.
func TestFillingTwoSlotsInOneStatement(t *testing.T) {
	compiler, answer := stubCompiler(t)

	run := &macroRun{sessionID: "test", source: "click()\nmove(:: the x offset ::, :: the y offset ::)"}

	for _, given := range []string{"-120", "40"} {
		if !psl.HasSlot(run.line(2)) {
			t.Fatalf("nothing left to fill in %q", run.line(2))
		}
		answer(given)
		source := run.source

		result, err := compiler.Fill(context.Background(), psl.Request{Source: source, Name: "macro.psl"})
		if err != nil {
			t.Fatalf("%q: %v", run.line(2), err)
		}
		filled, ok := extractLine(source, result.Source, 1)
		if !ok {
			t.Fatalf("%q: could not read the filled statement back", run.line(2))
		}
		run.record(2, filled)
	}

	if want := "move(-120, 40)"; strings.TrimSpace(run.line(2)) != want {
		t.Errorf("filled to %q, want %q", run.line(2), want)
	}
}

// A filled if header is read back as a condition, so the block runs on what the
// compiler answered rather than on the slot it was asked about.
func TestFilledConditionReadsBack(t *testing.T) {
	for _, tt := range []struct {
		filled string
		holds  bool
		read   bool
	}{
		{`if (true) {`, true, true},
		{`if (false) {`, false, true},
		{`if ("true") {`, true, true},
		{`if (probably) {`, false, false},
	} {
		expr, isIf := parseIfHeader(tt.filled)
		if !isIf {
			t.Errorf("%q did not read as an if header", tt.filled)
			continue
		}
		holds, read := conditionHolds(expr)
		if holds != tt.holds || read != tt.read {
			t.Errorf("%q -> (%v, %v), want (%v, %v)", tt.filled, holds, read, tt.holds, tt.read)
		}
	}
}

// A filled loop header is read back the same way an if's is, so the pass runs on
// what the compiler answered — and the count the loop was written with is still
// there afterwards, since nothing was asked about it.
func TestFilledLoopConditionReadsBack(t *testing.T) {
	for _, tt := range []struct {
		filled string
		count  int
		holds  bool
		read   bool
	}{
		{`loop (true, 5) {`, 5, true, true},
		{`loop (false, 5) {`, 5, false, true},
		{`loop ("true", 5) {`, 5, true, true},
		{`loop (probably, 5) {`, 0, false, false},
	} {
		condition, count, isLoop := parseLoopHeader(tt.filled)
		if !isLoop {
			t.Errorf("%q did not read as a loop header", tt.filled)
			continue
		}
		if count != tt.count {
			t.Errorf("%q -> count %d, want %d", tt.filled, count, tt.count)
		}
		holds, read := conditionHolds(condition)
		if holds != tt.holds || read != tt.read {
			t.Errorf("%q -> (%v, %v), want (%v, %v)", tt.filled, holds, read, tt.holds, tt.read)
		}
	}
}

// The whole way through a loop: the condition is answered, the pass runs, and
// the statements go back the way they were written so the next pass asks its own
// questions rather than repeating the last pass's answers.
func TestFillingALoopThroughTheCompiler(t *testing.T) {
	compiler, answer := stubCompiler(t)

	macro := `loop (:: the window is still open ::, 3) {
	move(:: the x offset to the Close button ::, 0)
	click()
}`
	nodes := parseMacro(macro)
	loop := nodes[0]
	run := newMacroRun("test", macro, "")

	fill := func(line int) string {
		t.Helper()
		source := run.source
		if live, ok := liveSlotLine(source); !ok || live != line-1 {
			t.Fatalf("psl would fill a slot on line %d, want line %d", live+1, line)
		}
		result, err := compiler.Fill(context.Background(), psl.Request{Source: source, Name: "macro.psl"})
		if err != nil {
			t.Fatalf("line %d: %v", line, err)
		}
		filled, ok := extractLine(source, result.Source, line-1)
		if !ok {
			t.Fatalf("line %d: could not read the filled statement back", line)
		}
		run.record(line, filled)
		return strings.TrimSpace(filled)
	}

	offsets := []string{"-120", "-260"}
	for pass, offset := range offsets {
		if pass > 0 {
			run.restore(loop.line)
			run.restoreBlock(loop.body)
		}

		answer("true")
		if got, want := fill(loop.line), "loop (true, 3) {"; got != want {
			t.Fatalf("pass %d: the header filled to %q, want %q", pass+1, got, want)
		}

		answer(offset)
		if got, want := fill(loop.body[0].line), "move("+offset+", 0)"; got != want {
			t.Errorf("pass %d: the statement filled to %q, want %q", pass+1, got, want)
		}
	}

	// The pass that ends it: the condition is asked once more, and the body is
	// not.
	run.restore(loop.line)
	run.restoreBlock(loop.body)
	answer("false")
	if got, want := fill(loop.line), "loop (false, 3) {"; got != want {
		t.Errorf("the closing check filled to %q, want %q", got, want)
	}
	run.spendBlock(loop.body)
	run.spend(loop.line)

	if psl.HasSlot(run.source) {
		t.Errorf("the finished loop still holds a slot: %q", run.source)
	}
}

// psl exits 0 having done nothing when it finds no slot. For Pob that is a
// failure: the statement it was called for still holds the slot.
func TestCompilerLeavingTheSlotUnfilledIsAnError(t *testing.T) {
	compiler, answer := stubCompiler(t)
	answer("-120")

	// No slot in it at all, so the stub changes nothing.
	if _, err := compiler.Fill(context.Background(), psl.Request{Source: "click()\n", Name: "macro.psl"}); err == nil {
		t.Error("Fill succeeded on a file with nothing to fill, want an error")
	}
}

// The two forms go the same way through the compiler.
func TestBothSlotFormsFillTheSame(t *testing.T) {
	compiler, answer := stubCompiler(t)
	answer("-120")

	for _, macro := range []string{
		"click()\nmove(::the x offset::, 40)",
		"click()\nmove(:: the x offset ::, 40)",
	} {
		run := &macroRun{sessionID: "test", source: macro}
		statement := run.line(2)
		source, target := run.source, 1

		result, err := compiler.Fill(context.Background(), psl.Request{Source: source, Name: "macro.psl"})
		if err != nil {
			t.Fatalf("%q: %v", statement, err)
		}
		got, ok := extractLine(source, result.Source, target)
		if !ok {
			t.Fatalf("%q: could not read the filled statement back", statement)
		}
		if want := "move(-120, 40)"; strings.TrimSpace(got) != want {
			t.Errorf("%q filled to %q, want %q", statement, got, want)
		}
	}
}

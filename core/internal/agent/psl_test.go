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
func stubCompiler(t *testing.T) (psl.Compiler, func(answer string)) {
	t.Helper()
	dir := t.TempDir()
	answerFile := filepath.Join(dir, "answer.txt")
	stub := filepath.Join(dir, "psl")

	script := `#!/bin/sh
file="$1"
answer=$(cat ` + answerFile + `)
awk -v ans="$answer" '{
  if (!done && match($0, /:: [^:]+ ::/)) {
    $0 = substr($0, 1, RSTART-1) ans substr($0, RSTART+RLENGTH)
    done = 1
  }
  print
}' "$file" > "$file.new" && mv "$file.new" "$file"
echo "psl: macro.psl resolved with stub-model — the slot" >&2
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

// The whole way through: Pob builds the file psl is shown, psl fills the one
// live slot in it, and Pob reads the statement back out ready to execute.
func TestFillingAStatementThroughTheCompiler(t *testing.T) {
	compiler, answer := stubCompiler(t)

	macro := `click()
move(:: the x offset to the Save button ::, 40)
typeText(:: what to say ::)
if (:: a save dialog is on screen ::) {
	keyPress("return")
}`
	run := &macroRun{sessionID: "test", source: macro}

	tests := []struct {
		line             int
		statement, given string
		want             string
	}{
		{2, `move(:: the x offset to the Save button ::, 40)`, "-120", `move(-120, 40)`},
		{3, `typeText(:: what to say ::)`, `"Hello"`, `typeText("Hello")`},
		{4, `if (:: a save dialog is on screen ::) {`, "true", `if (true) {`},
	}
	for _, tt := range tests {
		answer(tt.given)
		source, target := run.sourceFor(tt.line, tt.statement)

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

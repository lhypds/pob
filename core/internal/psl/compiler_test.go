package psl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubBinary stands in for the psl executable: it writes down the arguments it
// was run with, and fills the first slot in the file so the run counts as one
// that did something. Written in sh and sed so the test needs nothing installed.
func stubBinary(t *testing.T) (Compiler, func() []string) {
	t.Helper()
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stub := filepath.Join(dir, "psl")

	script := `#!/bin/sh
printf '%s\n' "$@" > ` + argsFile + `
file="$1"
sed 's/::[^:]*::/-120/' "$file" > "$file.new" && mv "$file.new" "$file"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	args := func() []string {
		data, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("the stub recorded no arguments: %v", err)
		}
		return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	return Compiler{Binary: stub, Dir: dir}, args
}

// helpStub is a psl that prints the help text given and does nothing else —
// enough to be asked what flags it takes.
func helpStub(t *testing.T, help string) Compiler {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "psl")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat <<'EOF'\n"+help+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Compiler{Binary: stub, Dir: dir}
}

// argValue returns what followed a flag, and whether the flag was passed at all.
func argValue(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// The briefing goes over as psl's own --prompt, with the text passed as it
// stands: it describes the vocabulary the file is written in, which psl adds to
// the system prompt for the slot it resolves on this run.
func TestPromptIsPassedToTheCompiler(t *testing.T) {
	compiler, args := stubBinary(t)
	briefing := "move(dx, dy) is an offset from the cursor, in pixels"

	if _, err := compiler.Fill(context.Background(), Request{
		Source: "move(:: the x offset ::, 40)\n",
		Name:   "macro.psl",
		Prompt: briefing,
	}); err != nil {
		t.Fatal(err)
	}

	got, passed := argValue(args(), "--prompt")
	if !passed {
		t.Fatalf("psl was run as %q, want --prompt among the arguments", args())
	}
	if got != briefing {
		t.Errorf("--prompt = %q, want the briefing as it stands", got)
	}
}

// Whether the flag can be passed at all is read out of psl's own help. A psl
// older than the flag exits on an option it does not know, so asking first is
// what keeps a machine that has one filling its slots.
func TestSupportsPromptReadsTheHelp(t *testing.T) {
	options := "Options:\n  -i, --image <data>   image given to the slot\n"

	if takes := helpStub(t, options+"  -p, --prompt <text>  guidance added to the system prompt\n"); !takes.SupportsPrompt() {
		t.Error("SupportsPrompt() = false on a psl whose help lists --prompt")
	}
	if older := helpStub(t, options); older.SupportsPrompt() {
		t.Error("SupportsPrompt() = true on a psl whose help does not list --prompt")
	}
	if missing := (Compiler{Binary: "/nonexistent/psl"}); missing.SupportsPrompt() {
		t.Error("SupportsPrompt() = true with no psl to ask")
	}
}

// A run with nothing to say passes no flag, rather than an empty one psl would
// have to decide what to do with.
func TestNoPromptMeansNoFlag(t *testing.T) {
	compiler, args := stubBinary(t)

	if _, err := compiler.Fill(context.Background(), Request{
		Source: "move(:: the x offset ::, 40)\n",
		Name:   "macro.psl",
	}); err != nil {
		t.Fatal(err)
	}

	if value, passed := argValue(args(), "--prompt"); passed {
		t.Errorf("--prompt = %q, want no --prompt at all", value)
	}
}

func TestFailedCompilerReturnsItsCompleteOutput(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "psl")
	script := "#!/bin/sh\nprintf 'first response row\\nsecond response row\\n' >&2\nexit 3\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := (Compiler{Binary: stub, Dir: dir}).Fill(context.Background(), Request{
		Source: "click(:: target ::)\n",
		Name:   "macro.psl",
	})
	if err == nil {
		t.Fatal("Fill succeeded, want the stub's exit failure")
	}
	if result == nil {
		t.Fatal("Fill returned no result details for the failed compiler")
	}
	want := "first response row\nsecond response row"
	if result.Output != want {
		t.Errorf("failed compiler output = %q, want %q", result.Output, want)
	}
	if result.Duration <= 0 {
		t.Errorf("failed compiler duration = %s, want a measured duration", result.Duration)
	}
}

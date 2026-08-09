package psl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DefaultBinary is what Pob runs when nothing names another one. Bare, so it is
// found on the PATH — psl's own install.sh puts it there.
const DefaultBinary = "psl"

// Compiler runs the psl executable.
type Compiler struct {
	// Binary is the executable, a name to find on the PATH or a path to one.
	Binary string
	// Dir is what psl runs in, which is where it looks for .pslrc before
	// falling back to the home directory. Pob points it at <root>, so a machine
	// keeps its model configuration beside the rest of what Pob works with.
	Dir string
	// Timeout bounds one slot. A model call is slow and a hung one would
	// otherwise hold the replay open with the cursor parked mid-macro.
	Timeout time.Duration
}

// DefaultTimeout is generous: a vision request with a full-screen screenshot on
// a slow link is minutes rather than seconds, and the cost of waiting is a
// pause, while the cost of giving up early is a macro that stops halfway.
const DefaultTimeout = 5 * time.Minute

// Available reports whether the executable can be found. It is what Pob checks
// before a macro with slots in it moves the cursor: a macro that cannot be
// filled is one to say so about up front, not halfway down.
func (c Compiler) Available() bool {
	_, err := c.resolve()
	return err == nil
}

// Describe says where the compiler was found, or that it was not — what
// `pob status` and the dashboard report, since a macro with an AI slot in it
// will not run without one and nothing else on those pages would say so.
func (c Compiler) Describe() string {
	path, err := c.resolve()
	if err != nil {
		binary := c.Binary
		if binary == "" {
			binary = DefaultBinary
		}
		return binary + " (not found)"
	}
	return path
}

func (c Compiler) resolve() (string, error) {
	binary := c.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	if strings.ContainsRune(binary, os.PathSeparator) {
		if info, err := os.Stat(binary); err == nil && !info.IsDir() {
			return binary, nil
		}
		return "", fmt.Errorf("%s is not an executable", binary)
	}
	return exec.LookPath(binary)
}

// Request is one slot to fill.
type Request struct {
	// Source is the whole file psl is shown, with exactly one live slot in it.
	Source string
	// Name is what the temporary file is called, which is what psl reports in
	// its messages and shows the model as the file's name.
	Name string
	// Image is the PNG handed to the model as the slot's visual context — for
	// Pob, the screen as it is at the moment the statement is reached.
	Image []byte
}

// Result is what one run of psl did.
type Result struct {
	// Source is the file after psl wrote the answer into it.
	Source string
	// Model and Instruction are read back out of psl's own progress line, so
	// the log says which model answered without Pob having to know the
	// configuration that picked it.
	Model       string
	Instruction string
	// Output is everything psl said on stderr, kept whole for the log.
	Output   string
	Duration time.Duration
}

// resolvedLine is what psl prints when it has filled a slot:
//
//	psl: <file> resolved with <model> — <instruction>
//	psl: <file> resolved with <model> (35 tokens: 31 in, 4 out) — <instruction>
//
// The token count is there whenever the model reported one, so it is optional
// here rather than assumed either way.
var resolvedLine = regexp.MustCompile(`resolved with (\S+)(?: \([^)]*\))? — (.*)`)

// Fill runs psl once over the source and returns it with the first slot
// replaced by what the model answered.
//
// Nothing is left behind on failure: psl rewrites its input file only once the
// model has returned usable text, so a run that fails leaves the source as it
// was and can simply be tried again.
func (c Compiler) Fill(ctx context.Context, req Request) (*Result, error) {
	binary, err := c.resolve()
	if err != nil {
		return nil, fmt.Errorf("psl not found: %w", err)
	}

	dir, err := os.MkdirTemp("", "pob-psl-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	name := req.Name
	if name == "" {
		name = "macro.psl"
	}
	sourcePath := filepath.Join(dir, name)
	if err := os.WriteFile(sourcePath, []byte(req.Source), 0o600); err != nil {
		return nil, err
	}

	args := []string{sourcePath}
	if len(req.Image) > 0 {
		imagePath := filepath.Join(dir, "screen.png")
		if err := os.WriteFile(imagePath, req.Image, 0o600); err != nil {
			return nil, err
		}
		args = append(args, "--image", imagePath)
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = c.Dir
	cmd.Stderr = &stderr

	started := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(started)

	output := strings.TrimSpace(stderr.String())
	if runErr != nil {
		return nil, fmt.Errorf("%s: %s", runErr, firstLine(output))
	}

	filled, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, err
	}

	result := &Result{Source: string(filled), Output: output, Duration: elapsed}
	if match := resolvedLine.FindStringSubmatch(output); match != nil {
		result.Model, result.Instruction = match[1], strings.TrimSpace(match[2])
	}

	// psl exits 0 with the file untouched when it finds nothing to do. For Pob
	// that is a failure rather than a success: the statement it was called for
	// still holds the slot it was called about.
	if result.Source == req.Source {
		return nil, fmt.Errorf("psl left the slot unfilled: %s", firstLine(output))
	}
	return result, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

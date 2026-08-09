package psl

// llm.log — one block per psl run, written whether it succeeded or not.
//
// Pob makes no model calls of its own any more, so what it can report about one
// is what psl says about it: which model answered, how long the run took, and
// what came back. Tokens and money are not among them — psl does not report
// them, and Pob would have to guess. Its own progress output is kept whole in
// the block, so whatever it does say is there to read.

import (
	"fmt"
	"strings"
	"time"

	"pob/core/internal/applog"
)

// LogCall appends one block for a run of the compiler. purpose heads it, and
// result is nil when the run failed before it produced anything.
func LogCall(purpose string, instruction string, result *Result, err error) {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), purpose)
	fmt.Fprintf(&b, "  compiler   psl\n")
	if instruction != "" {
		fmt.Fprintf(&b, "  slot       %s\n", oneLine(instruction))
	}

	if result != nil {
		if result.Model != "" {
			fmt.Fprintf(&b, "  model      %s\n", result.Model)
		}
		fmt.Fprintf(&b, "  duration   %s\n", result.Duration.Round(time.Millisecond))
	}

	if err != nil {
		fmt.Fprintf(&b, "  status     failed\n")
		fmt.Fprintf(&b, "  error      %s\n", oneLine(truncate(err.Error(), 500)))
	} else {
		fmt.Fprintf(&b, "  status     ok\n")
	}

	if result != nil && result.Output != "" {
		// psl's own progress, indented under one heading so the block keeps its
		// shape however many lines it ran to.
		fmt.Fprintf(&b, "  psl        %s\n", indentAfterFirst(result.Output, "             "))
	}

	applog.LLM(b.String())
}

func indentAfterFirst(text, indent string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

// oneLine keeps a block one block: a newline in an error would otherwise look
// like the next field.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

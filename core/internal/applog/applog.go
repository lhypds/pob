// Package applog appends timestamped lines to <root>/app.log, matching the
// Swift AppLogger format. Both processes append to the same file; each write
// is a single O_APPEND line so entries interleave without corruption.
//
// <root>/llm.log is the other one it writes: a block per model call, kept apart
// from app.log because the two are read for different reasons. app.log is what
// the app did; llm.log is what it sent, what came back, and what that cost.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu      sync.Mutex
	path    string
	llmPath string
)

func Init(root string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(root, "app.log")
	llmPath = filepath.Join(root, "llm.log")
}

// LLM appends one block to <root>/llm.log. The block is written whole, in one
// call, so two of them never interleave halfway down.
func LLM(block string) {
	mu.Lock()
	defer mu.Unlock()
	if llmPath == "" {
		return
	}
	f, err := os.OpenFile(llmPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\n", block)
}

func Logf(format string, args ...any) {
	Log(fmt.Sprintf(format, args...))
}

func Log(message string) {
	mu.Lock()
	defer mu.Unlock()
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fmt.Fprintf(f, "[%s] %s\n", timestamp, message)
}

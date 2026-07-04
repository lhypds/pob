// Package applog appends timestamped lines to <root>/app.log, matching the
// Swift AppLogger format. Both processes append to the same file; each write
// is a single O_APPEND line so entries interleave without corruption.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	path string
)

func Init(root string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(root, "app.log")
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

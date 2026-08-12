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
	mu           sync.Mutex
	path         string
	instanceSink func(string)
)

func Init(root string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(root, "app.log")
	instanceSink = nil
}

// SetInstanceSink mirrors subsequent app messages into the running
// instance's own log. A callback keeps applog independent of the storage
// package and lets tests initialise the global app log without retaining a
// sink from the test before them.
func SetInstanceSink(sink func(string)) {
	mu.Lock()
	defer mu.Unlock()
	instanceSink = sink
}

func Logf(format string, args ...any) {
	Log(fmt.Sprintf(format, args...))
}

func Log(message string) {
	mu.Lock()
	if path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
			_, _ = fmt.Fprintf(f, "[%s] %s\n", timestamp, message)
			_ = f.Close()
		}
	}
	sink := instanceSink
	mu.Unlock()

	// Do not call external code while holding applog's mutex. The sink writes a
	// different file today, and keeping it outside the lock also makes that
	// separation impossible to accidentally deadlock later.
	if sink != nil {
		sink(message)
	}
}

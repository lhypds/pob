// Package applog decides which of Pob's two logs a message belongs in.
//
// <root>/app.log is the machine's record across instances, and it is kept
// short on purpose: the app starting and stopping, an instance starting and
// stopping, and errors. Read on its own it should answer "did it come up, and
// did anything break" without scrolling.
//
// Everything else is detail — every step, every psl call, every frame — and
// detail belongs to the running instance, in <instance>/instance.log. The
// storage package writes that file; applog reaches it through the sink set
// here, so every message logged lands there whatever its level.
//
// So: Log for detail (instance.log only), Event for the lifecycle lines and
// Error for failures (both, in app.log too). Both processes append to app.log;
// each write is a single O_APPEND line so entries interleave without
// corruption.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TimeLayout is how every row of both logs is stamped: the machine's own clock,
// with the zone's offset on the end. A log is read by whoever is sitting at the
// machine, next to a run they remember the time of, so it is written in the time
// they were watching it happen rather than in UTC.
//
// Six fractional digits are always written, including trailing zeroes, and the
// offset is a fixed six characters, so adjacent rows do not shift horizontally.
// The shells stamp the lines they append to the same two files this way too —
// see AppLogger in macos, win and linux-x11 — and storage.LogInstance uses this
// for instance.log.
const TimeLayout = "2006-01-02T15:04:05.000000-07:00"

var (
	mu           sync.Mutex
	path         string
	instanceSink func(level, message string)
)

func Init(root string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(root, "app.log")
	instanceSink = nil
}

// SetInstanceSink mirrors subsequent messages into the running instance's own
// log, under the level given. A callback keeps applog independent of the
// storage package and lets tests initialise the global app log without
// retaining a sink from the test before them.
func SetInstanceSink(sink func(level, message string)) {
	mu.Lock()
	defer mu.Unlock()
	instanceSink = sink
}

// Log records detail: it goes to the instance log alone.
func Log(message string) { write("INFO", false, message) }

func Logf(format string, args ...any) { Log(fmt.Sprintf(format, args...)) }

// Event records a line app.log is kept for — the app or an instance starting
// or stopping. It goes to both logs.
func Event(message string) { write("INFO", true, message) }

func Eventf(format string, args ...any) { Event(fmt.Sprintf(format, args...)) }

// Error records a failure. It goes to both logs, marked ERROR, so app.log
// answers what went wrong and instance.log keeps it beside the detail that
// led there.
func Error(message string) { write("ERROR", true, message) }

func Errorf(format string, args ...any) { Error(fmt.Sprintf(format, args...)) }

func write(level string, toAppLog bool, message string) {
	mu.Lock()
	if toAppLog && path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			timestamp := time.Now().Format(TimeLayout)
			line := message
			if level != "INFO" {
				line = level + " " + message
			}
			_, _ = fmt.Fprintf(f, "[%s] %s\n", timestamp, line)
			_ = f.Close()
		}
	}
	sink := instanceSink
	mu.Unlock()

	// Do not call external code while holding applog's mutex. The sink writes a
	// different file today, and keeping it outside the lock also makes that
	// separation impossible to accidentally deadlock later.
	if sink != nil {
		sink(level, message)
	}
}

// Package agent runs an instance's macro.psl — the Prompt Script Language
// program the Play button, `pob macro` and the control API all replay. All
// screen perception and operation goes through the bridge to the native shell,
// and the :: … :: slots in the program are filled by running the psl compiler.
package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/config"
	"pob/core/internal/psl"
	"pob/core/internal/storage"
)

type Runner struct {
	cfg   *config.Config
	store *storage.Storage
	psl   psl.Compiler
	br    *bridge.Bridge

	mu             sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
	running        bool
	currentSession string

	recording atomic.Bool
}

func NewRunner(cfg *config.Config, store *storage.Storage, compiler psl.Compiler, br *bridge.Bridge) *Runner {
	return &Runner{cfg: cfg, store: store, psl: compiler, br: br}
}

func (r *Runner) SetRecording(recording bool) {
	previous := r.recording.Swap(recording)
	if previous == recording {
		return
	}
	if recording {
		r.store.LogInstance("RECORDING START", "macro.psl recording enabled")
	} else {
		r.store.LogInstance("RECORDING STOP", "macro.psl recording disabled")
	}
}

// Recording reports whether macro recording is on.
func (r *Runner) Recording() bool { return r.recording.Load() }

// Running reports whether a session is executing.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// CurrentSession returns the executing session's ID, or "" when idle.
func (r *Runner) CurrentSession() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return ""
	}
	return r.currentSession
}

func (r *Runner) setCurrentSession(id string) {
	r.mu.Lock()
	r.currentSession = id
	r.mu.Unlock()
}

// TakeScreenshot handles the toolbar screenshot button: flash, capture and
// save under <instance>/screenshots/. While recording it also appends
// takeScreenshot() to the macro so replay repeats the capture. Ignored
// during a session — the session owns the capture pipeline then.
func (r *Runner) TakeScreenshot() {
	r.mu.Lock()
	busy := r.running
	r.mu.Unlock()
	if busy {
		return
	}
	r.recordMacro("takeScreenshot()")
	r.br.FlashScreenshot()
	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Log("Screenshot button: capture failed")
		return
	}
	r.store.SaveUserScreenshot(shot)
	applog.Log("Screenshot saved")
}

// Stop cancels the running session, if any.
func (r *Runner) Stop() {
	_, stopped := r.requestStop("user request")
	if !stopped {
		r.store.LogInstance("MACRO STOP REQUEST", "ignored: no macro is running")
	}
	applog.Log("Stopped")
}

// StopAndWait cancels a macro during instance shutdown and gives it time to
// save its compiled macro, session times and MACRO STOP event before the core
// process exits. It returns false only when the timeout expires.
func (r *Runner) StopAndWait(timeout time.Duration) bool {
	done, stopped := r.requestStop("instance shutdown")
	if !stopped || done == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (r *Runner) requestStop(reason string) (<-chan struct{}, bool) {
	r.mu.Lock()
	cancel := r.cancel
	sessionID := r.currentSession
	done := r.done
	r.mu.Unlock()
	if cancel == nil {
		return done, false
	}
	if sessionID == "" {
		sessionID = "pending"
	}
	r.store.LogInstancef("MACRO STOP REQUEST", "session=%s reason=%q", sessionID, reason)
	cancel()
	return done, true
}

// start reserves the runner and returns a fresh session context, or nil if a
// session is already running.
func (r *Runner) start() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.running = true
	r.cancel = cancel
	r.done = make(chan struct{})
	r.currentSession = ""
	return ctx
}

func (r *Runner) finish() {
	r.mu.Lock()
	done := r.done
	r.running = false
	r.cancel = nil
	r.done = nil
	r.currentSession = ""
	r.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// RunMacro starts a macro session asynchronously. It returns false when a
// session is already running.
func (r *Runner) RunMacro() bool {
	ctx := r.start()
	if ctx == nil {
		r.store.LogInstance("MACRO START REJECTED", "another macro is already running")
		return false
	}
	r.store.LogInstance("MACRO START REQUEST", "accepted")
	go func() {
		defer r.finish()
		r.br.NotifyExecutionState(true)
		defer r.br.NotifyExecutionState(false)
		r.runMacro(ctx)
	}()
	return true
}

func (r *Runner) recordMacro(line string) {
	if r.recording.Load() {
		r.cfg.AppendToMacro(line)
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// oneLine folds a value onto the single line a log entry is. The log stamps the
// time on the front of a message and the newline on the end of it, so a message
// carrying newlines of its own is several entries with one timestamp between
// them — and what a statement slot fills to is a block of statements.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// Package agent runs an instance's macro.psl — the Prompt Script Language
// program the Play button, `pob macro` and the control API all replay. All
// screen perception and operation goes through the bridge to the native shell,
// and the :: … :: slots in the program are filled by running the psl compiler.
package agent

import (
	"context"
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
	running        bool
	currentSession string

	recording atomic.Bool
}

func NewRunner(cfg *config.Config, store *storage.Storage, compiler psl.Compiler, br *bridge.Bridge) *Runner {
	return &Runner{cfg: cfg, store: store, psl: compiler, br: br}
}

func (r *Runner) SetRecording(recording bool) { r.recording.Store(recording) }

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
// save under <instance>/logs/screenshots/. While recording it also appends
// take_screenshot() to the macro so replay repeats the capture. Ignored
// during a session — the session owns the capture pipeline then.
func (r *Runner) TakeScreenshot() {
	r.mu.Lock()
	busy := r.running
	r.mu.Unlock()
	if busy {
		return
	}
	r.recordMacro("take_screenshot()")
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	applog.Log("Stopped")
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
	return ctx
}

func (r *Runner) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
	r.cancel = nil
}

// RunMacro starts a macro session asynchronously. It returns false when a
// session is already running.
func (r *Runner) RunMacro() bool {
	ctx := r.start()
	if ctx == nil {
		return false
	}
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

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}

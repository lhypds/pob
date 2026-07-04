// Package agent drives instruction sessions (plan → execute → verify) and
// macro sessions. It is a direct port of the execution loop that previously
// lived in the Swift ContentView; all screen perception and operation goes
// through the bridge to the Swift shell.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/config"
	"pob/core/internal/llm"
	"pob/core/internal/storage"
)

type Runner struct {
	cfg   *config.Config
	store *storage.Storage
	llm   *llm.Client
	br    *bridge.Bridge

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool

	recording atomic.Bool
}

// session carries the per-session execution state (the counters that were
// @State vars on the Swift ContentView).
type session struct {
	ctx         context.Context
	id          string
	stepCount   int // global model-call counter, reset per plan and on user Continue
	resumeCount int // verification resume counter, reset per plan
}

func (s *session) cancelled() bool { return s.ctx.Err() != nil }

type planOutcome int

const (
	outcomeDone planOutcome = iota
	outcomeResumePlan
	outcomeStop
)

func NewRunner(cfg *config.Config, store *storage.Storage, llmClient *llm.Client, br *bridge.Bridge) *Runner {
	return &Runner{cfg: cfg, store: store, llm: llmClient, br: br}
}

func (r *Runner) SetRecording(recording bool) { r.recording.Store(recording) }

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

// RunInstruction starts an instruction session asynchronously.
func (r *Runner) RunInstruction() {
	ctx := r.start()
	if ctx == nil {
		return
	}
	go func() {
		defer r.finish()
		r.br.NotifyExecutionState(true)
		defer r.br.NotifyExecutionState(false)
		r.runInstruction(ctx)
	}()
}

// RunMacro starts a macro session asynchronously.
func (r *Runner) RunMacro() {
	ctx := r.start()
	if ctx == nil {
		return
	}
	go func() {
		defer r.finish()
		r.br.NotifyExecutionState(true)
		defer r.br.NotifyExecutionState(false)
		r.runMacro(ctx)
	}()
}

func (r *Runner) runInstruction(ctx context.Context) {
	if _, err := r.br.ResetCursor(); err != nil {
		return
	}

	sessionID := r.store.CreateSession()
	sessionStart := time.Now()
	applog.Logf("[%s] Session started", sessionID)

	currentShot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Log("Failed to capture screenshot")
		return
	}

	instruction := r.cfg.Instruction()
	r.store.SaveInstruction(sessionID)

	sess := &session{ctx: ctx, id: sessionID}

	// Generate plan and execute; loop on resumePlan, stop on stop/done/cancel.
	outcome := outcomeResumePlan
	for outcome == outcomeResumePlan && !sess.cancelled() {
		sess.stepCount = 0
		sess.resumeCount = 0
		planID := r.store.CreatePlan(sessionID)
		applog.Logf("[%s/%s] Generating plan...", sessionID, planID)
		plan := r.generatePlan(instruction, currentShot, sessionID, planID)
		if plan != "" {
			applog.Logf("[%s/%s] Plan: %s", sessionID, planID, plan)
		}

		if sess.cancelled() {
			break
		}

		outcome = r.executePlan(sess, planID, plan)

		if outcome == outcomeResumePlan {
			if fresh, err := r.br.CaptureScreenshot(true, nil); err == nil {
				currentShot = fresh
			}
		}
	}

	r.store.SaveSessionStartEndTimes(sessionID, sessionStart, time.Now())
	r.store.SaveSessionUsage(sessionID)
	applog.Logf("[%s] Session usage saved", sessionID)

	if !sess.cancelled() {
		if hook := r.cfg.StopHook(); hook != "" {
			_ = exec.Command("/bin/sh", "-c", hook).Start()
		}
	}
}

type planStep struct {
	Sequence    int
	Instruction string
	Expectation string
}

func (r *Runner) executePlan(sess *session, planID, plan string) planOutcome {
	var parsed struct {
		Steps []struct {
			Sequence    *int   `json:"sequence"`
			Instruction string `json:"instruction"`
			Expectation string `json:"expectation"`
		} `json:"steps"`
	}
	if plan == "" || json.Unmarshal([]byte(plan), &parsed) != nil || len(parsed.Steps) == 0 {
		applog.Logf("[%s] No plan steps to execute.", sess.id)
		return outcomeDone
	}

	var steps []planStep
	for _, s := range parsed.Steps {
		if s.Sequence == nil || s.Instruction == "" {
			continue
		}
		steps = append(steps, planStep{Sequence: *s.Sequence, Instruction: s.Instruction, Expectation: s.Expectation})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].Sequence < steps[j].Sequence })

	stepIndex := 0
	for stepIndex < len(steps) && !sess.cancelled() {
		step := steps[stepIndex]
		stepDone := false
		jumpToIndex := -1
		isStepResume := false

		for !stepDone && jumpToIndex < 0 && !sess.cancelled() {
			limitHit := r.executeStep(sess, planID, step, plan, isStepResume)

			if limitHit {
				applog.Logf("[plan:%s/step:%d] Step log limit hit — resuming step...", sess.id, step.Sequence)
				isStepResume = true
				continue
			}

			verdict, targetSeq := r.verifyStep(sess, planID, step, plan)
			switch verdict {
			case verifyVerified:
				stepDone = true
			case verifyResumeStep:
				sess.resumeCount++
				if sess.resumeCount > r.cfg.MaxResumes() {
					applog.Logf("[plan:%s/step:%d] Resume count %d exceeded limit — regenerating plan...", sess.id, step.Sequence, sess.resumeCount)
					return outcomeResumePlan
				}
				jump := -1
				if targetSeq != nil {
					for i, s := range steps {
						if s.Sequence == *targetSeq && i != stepIndex {
							jump = i
							break
						}
					}
				}
				if jump >= 0 {
					applog.Logf("[plan:%s/step:%d] Jumping to step %d...", sess.id, step.Sequence, *targetSeq)
					jumpToIndex = jump
				} else {
					applog.Logf("[plan:%s/step:%d] Retrying step %d...", sess.id, step.Sequence, step.Sequence)
					isStepResume = true
				}
			case verifyResumePlan:
				applog.Logf("[plan:%s] Resume All — regenerating plan...", sess.id)
				return outcomeResumePlan
			case verifyStop:
				applog.Logf("[plan:%s/step:%d] Stop — halting execution.", sess.id, step.Sequence)
				return outcomeStop
			}
		}

		if jumpToIndex >= 0 {
			stepIndex = jumpToIndex
		} else {
			stepIndex++
		}
	}
	return outcomeDone
}

const stepSystemPromptFormat = `You are a desktop automation assistant. All coordinates are screenshot pixel coordinates (origin = top-left of the screenshot, x right, y down). The app converts them to real screen positions — you never deal with screen or OS coordinates.

Use the available tools to interact with the screen.

Workflow:
1. Use move(dx, dy) repeatedly to position the cursor arrow tip precisely on the target.
2. Call the appropriate action (click, rightClick, doubleClick, drag, scroll, type, keyPress).

The cursor starts at (20, 20).

Execute this step: %s
Expectation: %s

Full execution plan for reference:
%s`

// executeStep runs the tool-call loop for one plan step. Returns true when
// the step-log limit was hit (caller resumes the step with fresh context).
func (r *Runner) executeStep(sess *session, planID string, step planStep, plan string, isResume bool) bool {
	r.store.WriteStepStatus("RUNNING", sess.id, planID, step.Sequence)
	verb := "Starting"
	if isResume {
		verb = "Resuming"
	}
	applog.Logf("[plan:%s/step:%d] %s step %d: %s", sess.id, step.Sequence, verb, step.Sequence, step.Instruction)

	initShot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		r.store.WriteStepStatus("ERROR", sess.id, planID, step.Sequence)
		return false
	}

	messages := []map[string]any{
		{"role": "system", "content": fmt.Sprintf(stepSystemPromptFormat, step.Instruction, step.Expectation, plan)},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": fmt.Sprintf("Step %d: %s", step.Sequence, step.Instruction)},
			imagePart(initShot),
		}},
	}

	tools := makeTools()
	lastScreenshot := initShot
	logCount := 0
	emptyResponseCount := 0
	limitHit := false

	for !sess.cancelled() {
		if sess.stepCount >= r.cfg.MaxSteps() {
			applog.Logf("[plan:%s/step:%d] Max step exceeded.", sess.id, step.Sequence)
			if !r.br.ConfirmMaxStep() {
				break
			}
			sess.stepCount = 0
		}
		sess.stepCount++

		logCount++
		applog.Logf("[plan:%s/step:%d/log:%d] Analyzing...", sess.id, step.Sequence, time.Now().Unix())

		result := r.llm.Chat(messages, tools, nil)

		// Copy before adding usage — RawAssistantMessage is also appended to
		// the conversation and must not carry extra fields back to the API.
		responseToSave := map[string]any{"error": result.Error}
		if result.Success {
			responseToSave = shallowCopy(result.RawAssistantMessage)
		}
		if result.Usage != nil {
			responseToSave["usage"] = result.Usage
		}
		r.store.SaveStepLog(sess.id, planID, step.Sequence, messages, responseToSave, lastScreenshot)

		if logCount > r.cfg.MaxStepLogs() {
			applog.Logf("[plan:%s/step:%d] Step log limit exceeded.", sess.id, step.Sequence)
			limitHit = true
			break
		}

		if !result.Success {
			applog.Logf("[plan:%s/step:%d] Error: %s", sess.id, step.Sequence, result.Error)
			break
		}

		messages = append(messages, result.RawAssistantMessage)

		if len(result.ToolCalls) == 0 {
			if result.ContentText != "" {
				applog.Logf("[plan:%s/step:%d] Done: %s", sess.id, step.Sequence, truncate(result.ContentText, 100))
				break
			}
			emptyResponseCount++
			if emptyResponseCount >= 3 {
				applog.Logf("[plan:%s/step:%d] Too many empty responses, stopping.", sess.id, step.Sequence)
				break
			}
			applog.Logf("[plan:%s/step:%d] Empty response, prompting to continue...", sess.id, step.Sequence)
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": "Continue the task. Use move(dx, dy) to position the cursor, then call the appropriate action.",
			})
			continue
		}
		emptyResponseCount = 0

		for _, toolCall := range result.ToolCalls {
			if sess.cancelled() {
				break
			}
			messages, lastScreenshot = r.dispatchToolCall(sess, planID, step, toolCall, messages, lastScreenshot)
		}

		if sess.cancelled() {
			break
		}
	}

	r.store.WriteStepStatus("DONE", sess.id, planID, step.Sequence)
	return limitHit
}

// dispatchToolCall executes one tool call, appends the tool result (and the
// follow-up screenshot message) and returns the updated conversation.
func (r *Runner) dispatchToolCall(sess *session, planID string, step planStep, toolCall llm.ToolCall, messages []map[string]any, lastScreenshot []byte) ([]map[string]any, []byte) {
	args := toolCall.Arguments
	toolMsg := func(content string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": toolCall.ID, "content": content}
	}
	// screenshotFollowUp captures a fresh screenshot and appends it as a user
	// message, optionally preceded by a text part.
	screenshotFollowUp := func(text string) {
		shot, err := r.br.CaptureScreenshot(true, nil)
		if err != nil {
			return
		}
		lastScreenshot = shot
		var parts []any
		if text != "" {
			parts = append(parts, map[string]any{"type": "text", "text": text})
		}
		parts = append(parts, imagePart(shot))
		messages = append(messages, map[string]any{"role": "user", "content": parts})
	}

	switch toolCall.Name {
	case "move":
		dx := floatArg(args, "dx")
		dy := floatArg(args, "dy")
		pos, err := r.br.MoveCursor(dx, dy)
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[plan:%s/step:%d] move(dx:%d, dy:%d) -> (%d, %d)", sess.id, step.Sequence, int(dx), int(dy), pos.X, pos.Y)
		r.recordMacro(fmt.Sprintf("move(%d, %d)", int(dx), int(dy)))
		messages = append(messages, toolMsg(fmt.Sprintf("Cursor moved by (%d, %d). New position: (%d, %d).", int(dx), int(dy), pos.X, pos.Y)))
		screenshotFollowUp(fmt.Sprintf("Cursor at (%d, %d). The arrow tip is the click point. Move again or call click().", pos.X, pos.Y))

	case "click":
		pos, err := r.br.Click()
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[plan:%s/step:%d] click at (%d, %d)", sess.id, step.Sequence, pos.X, pos.Y)
		r.recordMacro("click()")
		messages = append(messages, toolMsg(fmt.Sprintf("Clicked at (%d, %d).", pos.X, pos.Y)))
		screenshotFollowUp(fmt.Sprintf("Clicked at (%d, %d). Screenshot after click:", pos.X, pos.Y))

	case "rightClick":
		pos, err := r.br.RightClick()
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[%s] rightClick at (%d, %d)", sess.id, pos.X, pos.Y)
		r.recordMacro("rightClick()")
		messages = append(messages, toolMsg(fmt.Sprintf("Right-clicked at (%d, %d).", pos.X, pos.Y)))
		screenshotFollowUp("")

	case "doubleClick":
		pos, err := r.br.DoubleClick()
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[%s] doubleClick at (%d, %d)", sess.id, pos.X, pos.Y)
		r.recordMacro("doubleClick()")
		messages = append(messages, toolMsg(fmt.Sprintf("Double-clicked at (%d, %d).", pos.X, pos.Y)))
		screenshotFollowUp("")

	case "drag":
		dx := floatArg(args, "dx")
		dy := floatArg(args, "dy")
		pos, err := r.br.Drag(dx, dy)
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[%s] drag(%d, %d) -> (%d, %d)", sess.id, int(dx), int(dy), pos.X, pos.Y)
		r.recordMacro(fmt.Sprintf("drag(%d, %d)", int(dx), int(dy)))
		messages = append(messages, toolMsg(fmt.Sprintf("Dragged to (%d, %d).", pos.X, pos.Y)))
		screenshotFollowUp(fmt.Sprintf("Cursor at (%d, %d).", pos.X, pos.Y))

	case "scroll":
		dx := int(floatArg(args, "dx"))
		dy := int(floatArg(args, "dy"))
		pos, err := r.br.Scroll(dx, dy)
		if err != nil {
			return messages, lastScreenshot
		}
		applog.Logf("[%s] scroll(dx:%d, dy:%d) at (%d, %d)", sess.id, dx, dy, pos.X, pos.Y)
		r.recordMacro(fmt.Sprintf("scroll(%d, %d)", dx, dy))
		messages = append(messages, toolMsg(fmt.Sprintf("Scrolled dx:%d dy:%d at (%d, %d).", dx, dy, pos.X, pos.Y)))
		screenshotFollowUp("")

	case "typeText":
		text, _ := args["text"].(string)
		applog.Logf("[%s] typeText(%q)", sess.id, truncate(text, 80))
		r.recordMacro(fmt.Sprintf("typeText(%q)", text))
		if err := r.br.TypeText(text); err != nil {
			return messages, lastScreenshot
		}
		messages = append(messages, toolMsg(fmt.Sprintf("Typed %q.", text)))
		screenshotFollowUp("")

	case "keyPress":
		key, _ := args["key"].(string)
		applog.Logf("[%s] keyPress(%q)", sess.id, key)
		r.recordMacro(fmt.Sprintf("keyPress(%q)", key))
		if err := r.br.KeyPress(key); err != nil {
			return messages, lastScreenshot
		}
		messages = append(messages, toolMsg(fmt.Sprintf("Pressed %q.", key)))
		screenshotFollowUp("")

	case "sleep":
		ms := floatArg(args, "milliseconds")
		applog.Logf("[%s] sleep(%dms)", sess.id, int(ms))
		r.recordMacro(fmt.Sprintf("sleep(%d)", int(ms)))
		sleepCtx(sess.ctx, time.Duration(ms)*time.Millisecond)
		messages = append(messages, toolMsg(fmt.Sprintf("Slept for %dms.", int(ms))))

	case "take_screenshot":
		crop := cropFromArgs(args)
		if crop != nil {
			applog.Logf("[%s] take_screenshot(crop: %d, %d, %d, %d)", sess.id, int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
			r.recordMacro(fmt.Sprintf("take_screenshot(%d, %d, %d, %d)", int(crop.X), int(crop.Y), int(crop.W), int(crop.H)))
		} else {
			applog.Logf("[%s] take_screenshot", sess.id)
			r.recordMacro("take_screenshot()")
		}
		r.br.FlashScreenshot()
		messages = append(messages, toolMsg("Screenshot captured."))
		if shot, err := r.br.CaptureScreenshot(true, crop); err == nil {
			lastScreenshot = shot
			r.store.SaveScreenshot(shot, sess.id)
			messages = append(messages, map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Current screenshot:"},
				imagePart(shot),
			}})
		}

	default:
		applog.Logf("[%s] Unknown tool: %s", sess.id, toolCall.Name)
	}

	return messages, lastScreenshot
}

func (r *Runner) recordMacro(line string) {
	if r.recording.Load() {
		r.cfg.AppendToMacro(line)
	}
}

func floatArg(args map[string]any, key string) float64 {
	v, _ := args[key].(float64)
	return v
}

func cropFromArgs(args map[string]any) *bridge.CropRect {
	x, okX := args["crop_x"].(float64)
	y, okY := args["crop_y"].(float64)
	w, okW := args["crop_width"].(float64)
	h, okH := args["crop_height"].(float64)
	if !okX || !okY || !okW || !okH {
		return nil
	}
	return &bridge.CropRect{X: x, Y: y, W: w, H: h}
}

func shallowCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
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

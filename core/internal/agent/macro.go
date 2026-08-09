package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
)

// macroNode is one statement of a macro: either an action call, or an if block
// whose body only runs when the model judges its condition true against a fresh
// screenshot.
type macroNode struct {
	action    string      // action call name; empty on an if block
	args      []string    // action arguments
	condition string      // if only — what the AI is asked to judge
	body      []macroNode // if only — what runs when the condition holds
	line      int         // 1-based line in macro.psl, for logs
}

func (n macroNode) isIf() bool { return n.condition != "" }

// macroRun carries the state of one replay: the session it writes under, and
// how many if conditions have been judged so far — the count names their log
// directories.
type macroRun struct {
	sessionID  string
	conditions int
}

// runMacro replays macro.psl statement by statement.
func (r *Runner) runMacro(ctx context.Context) {
	nodes := parseMacro(r.cfg.Macro())

	// Checked before anything moves: a macro whose if cannot be judged is one
	// Pob cannot run as written, and finding that out halfway through would
	// leave the actions above the if already played.
	if missing := r.macroSettingsMissing(nodes); missing != "" {
		message := "macro.psl has an if the AI must judge, and that is a model call. settings.json has no " + missing + " — set it and run the macro again."
		applog.Logf("Macro not run: %s", message)
		r.br.ShowAlert("Settings needed", message)
		return
	}

	if _, err := r.br.ResetCursor(); err != nil {
		return
	}

	applog.Logf("Executing macro (%d actions)", countMacroNodes(nodes))

	// Initial capture establishes the screenshot→screen coordinate context on
	// the Swift side before any click lands.
	if _, err := r.br.CaptureScreenshot(true, nil); err != nil {
		applog.Log("Macro: failed to get screenshot context")
		return
	}

	sessionID := r.store.CreateSession()
	r.setCurrentSession(sessionID)
	r.store.SaveMacro(sessionID)
	macroStart := time.Now()
	applog.Logf("[%s] Macro session started", sessionID)

	run := &macroRun{sessionID: sessionID}
	r.runMacroNodes(ctx, run, nodes)

	r.store.SaveSessionStartEndTimes(sessionID, macroStart, time.Now())
	applog.Logf("[%s] Macro session times saved", sessionID)
	if run.conditions > 0 {
		// Only an if spends tokens; a macro without one has nothing to sum.
		r.store.SaveSessionUsage(sessionID)
		applog.Logf("[%s] Macro session usage saved", sessionID)
	}
	applog.Log("Macro execution complete")

	// A run that was stopped never reached its end, so nothing is announced:
	// the hook is what says the macro finished.
	if ctx.Err() == nil {
		if hook := r.cfg.StopHook(); hook != "" {
			_ = exec.Command("/bin/sh", "-c", hook).Start()
		}
	}
}

// runMacroNodes executes a block of statements, recursing into the if blocks
// whose condition holds.
func (r *Runner) runMacroNodes(ctx context.Context, run *macroRun, nodes []macroNode) {
	for _, node := range nodes {
		if ctx.Err() != nil {
			return
		}

		if node.isIf() {
			if r.evalMacroCondition(run, node) {
				r.runMacroNodes(ctx, run, node.body)
			}
			continue
		}

		r.runMacroAction(ctx, run.sessionID, node.action, node.args)

		if delayMs := r.cfg.MacroDefaultDelay(); delayMs > 0 {
			sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond)
		}
	}
}

const macroConditionSystemPrompt = `You are judging one condition for a desktop automation macro. You are given the condition in plain language and a screenshot of the current screen. Decide whether the condition holds right now, going only by what the screenshot shows.

Respond with JSON:
  {"result": true, "reason": "..."} — the condition holds.
  {"result": false, "reason": "..."} — it does not hold, or the screenshot does not show enough to tell.

The reason is one short sentence naming what you saw.`

var macroConditionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"result": map[string]any{"type": "boolean"},
		"reason": map[string]any{"type": "string"},
	},
	"required":             []string{"result", "reason"},
	"additionalProperties": false,
}

// evalMacroCondition asks the model whether an if condition holds against a
// fresh screenshot. Anything that goes wrong — no screenshot, no API key, an
// unreadable answer — reads as false: the block stays unexecuted rather than
// running on a guess.
func (r *Runner) evalMacroCondition(run *macroRun, node macroNode) bool {
	run.conditions++
	seq := run.conditions
	condition := node.condition

	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Logf("[%s] Macro if (::%s::) — no screenshot, skipping block", run.sessionID, condition)
		return false
	}

	applog.Logf("[%s] Macro if (::%s::) — checking...", run.sessionID, condition)

	messages := []map[string]any{
		{"role": "system", "content": macroConditionSystemPrompt},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "Condition: " + condition + "\n\nDoes it hold in the current screenshot?"},
			imagePart(shot),
		}},
	}

	result := r.llm.Chat(messages, nil, macroConditionSchema)

	// Copy before adding usage so the raw message stays as the API returned it.
	responseToSave := map[string]any{"error": result.Error}
	if result.Success {
		responseToSave = shallowCopy(result.RawAssistantMessage)
	}
	if result.Usage != nil {
		responseToSave["usage"] = result.Usage
	}

	// A half-decoded answer counts as no answer: the verdict is only the model's
	// when the whole thing read cleanly.
	var parsed struct {
		Result bool   `json:"result"`
		Reason string `json:"reason"`
	}
	verdict, reason := false, ""
	switch {
	case !result.Success:
		applog.Logf("[%s] Macro if (::%s::) — error: %s", run.sessionID, condition, result.Error)
	case result.ContentText == "" || json.Unmarshal([]byte(result.ContentText), &parsed) != nil:
		applog.Logf("[%s] Macro if (::%s::) — unreadable answer", run.sessionID, condition)
	default:
		verdict, reason = parsed.Result, parsed.Reason
	}

	r.store.SaveMacroCondition(run.sessionID, seq, node.line, condition, verdict, reason,
		append(messages, result.RawAssistantMessage), responseToSave, shot)

	reasonSuffix := ""
	if reason != "" {
		reasonSuffix = ": " + reason
	}
	if verdict {
		applog.Logf("[%s] Macro if (::%s::) -> TRUE%s", run.sessionID, condition, reasonSuffix)
	} else {
		applog.Logf("[%s] Macro if (::%s::) -> FALSE — skipping block%s", run.sessionID, condition, reasonSuffix)
	}
	return verdict
}

func (r *Runner) runMacroAction(ctx context.Context, sessionID, name string, args []string) {
	num := func(i int) (float64, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, err := strconv.ParseFloat(args[i], 64)
		return v, err == nil
	}

	switch name {
	case "move":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.MoveCursor(dx, dy); err == nil {
			applog.Logf("[%s] Macro move(%d, %d) -> (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "resetCursor":
		// Recorded when something sent the cursor home mid-sequence. Replaying
		// it matters because every move around it is a relative offset: skip the
		// jump back to the origin and each following move starts from the wrong
		// place.
		if pos, err := r.br.ResetCursor(); err == nil {
			applog.Logf("[%s] Macro resetCursor -> (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "click":
		if pos, err := r.br.Click(); err == nil {
			applog.Logf("[%s] Macro click at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "rightClick":
		if pos, err := r.br.RightClick(); err == nil {
			applog.Logf("[%s] Macro rightClick at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "doubleClick":
		if pos, err := r.br.DoubleClick(); err == nil {
			applog.Logf("[%s] Macro doubleClick at (%d, %d)", sessionID, pos.X, pos.Y)
		}

	case "drag":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Drag(dx, dy); err == nil {
			applog.Logf("[%s] Macro drag(%d, %d) -> (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "scroll":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Scroll(int(dx), int(dy)); err == nil {
			applog.Logf("[%s] Macro scroll(%d, %d) at (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		}

	case "typeText":
		if len(args) == 0 {
			return
		}
		text := args[0]
		applog.Logf("[%s] Macro typeText(%q)", sessionID, truncate(text, 80))
		_ = r.br.TypeText(text)

	case "keyPress":
		if len(args) == 0 {
			return
		}
		applog.Logf("[%s] Macro keyPress(%q)", sessionID, args[0])
		_ = r.br.KeyPress(args[0])

	case "sleep":
		ms, ok := num(0)
		if !ok {
			return
		}
		applog.Logf("[%s] Macro sleep(%dms)", sessionID, int(ms))
		sleepCtx(ctx, time.Duration(ms)*time.Millisecond)

	case "take_screenshot":
		var crop *bridge.CropRect
		if len(args) >= 4 {
			x, okX := num(0)
			y, okY := num(1)
			w, okW := num(2)
			h, okH := num(3)
			if okX && okY && okW && okH {
				crop = &bridge.CropRect{X: x, Y: y, W: w, H: h}
			}
		}
		if crop != nil {
			applog.Logf("[%s] Macro take_screenshot(crop: %d, %d, %d, %d)", sessionID, int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
		} else {
			applog.Logf("[%s] Macro take_screenshot", sessionID)
		}
		r.br.FlashScreenshot()
		if shot, err := r.br.CaptureScreenshot(true, crop); err == nil {
			r.store.SaveScreenshot(shot, sessionID)
		}

	default:
		applog.Logf("[%s] Macro: unknown action: %s", sessionID, name)
	}
}

const (
	// macroIfKeyword opens a conditional block: `if (<expression>) {`, closed by
	// a line holding nothing but `}`. Matched whatever its case, so `IF` opens a
	// block too: the alternative to recognising it is running the body
	// unguarded, which is the one thing a condition was written to prevent.
	macroIfKeyword = "if"
	// macroAIMarker wraps a prompt the AI answers when the line is reached, and
	// the answer stands where the marker was. It is the one expression there is
	// so far, and in an `if` the AI answers it true or false.
	macroAIMarker = "::"
)

// parseMacro turns macro.psl into the statements to execute. Lines it cannot
// read are logged and dropped, the way a bad action line always has been — a
// macro runs as far as it makes sense rather than not at all.
func parseMacro(text string) []macroNode {
	nodes, _ := parseMacroBlock(strings.Split(text, "\n"), 0, 0)
	return nodes
}

// parseMacroBlock reads statements from lines[i:] until the `}` that closes the
// block, or the end of the file at depth 0. It returns the statements and the
// index just past the block.
func parseMacroBlock(lines []string, i, depth int) ([]macroNode, int) {
	var nodes []macroNode

	for i < len(lines) {
		lineNo := i + 1
		trimmed := strings.TrimSpace(lines[i])
		i++

		if trimmed == "" {
			continue
		}

		if trimmed == "}" {
			if depth == 0 {
				applog.Logf("Macro line %d: } without a matching if", lineNo)
				continue
			}
			return nodes, i
		}

		if condition, isIf := parseIfHeader(trimmed); isIf {
			// The block is read either way, so that what a broken if was written
			// to guard is dropped with it rather than left to run unguarded.
			var body []macroNode
			body, i = parseMacroBlock(lines, i, depth+1)
			if condition == "" {
				applog.Logf("Macro line %d: if wants a condition in parentheses — if (::…::) { — skipping its block", lineNo)
				continue
			}
			nodes = append(nodes, macroNode{condition: condition, body: body, line: lineNo})
			continue
		}

		name, args, ok := parseMacroLine(trimmed)
		if !ok {
			applog.Logf("Macro: skipping line: %s", trimmed)
			continue
		}
		nodes = append(nodes, macroNode{action: name, args: args, line: lineNo})
	}

	if depth > 0 {
		applog.Log("Macro: if block left open — the end of the macro closes it")
	}
	return nodes, i
}

// parseIfHeader reads `if (::<condition>::) {` and returns the condition the AI
// is to judge. The second return says the line opens a block at all — a line
// that starts with the keyword opens one whether or not the rest of it is well
// formed, and an empty condition is what says it was not.
func parseIfHeader(line string) (string, bool) {
	rest, isIf := cutIfKeyword(line)
	if !isIf {
		return "", false
	}
	expr, ok := strings.CutSuffix(strings.TrimSpace(rest), "{")
	if !ok {
		return "", true
	}
	expr, ok = cutParens(strings.TrimSpace(expr))
	if !ok {
		return "", true
	}
	condition, ok := parseAISlot(expr)
	if !ok {
		return "", true
	}
	return condition, true
}

// cutIfKeyword returns what follows the `if` keyword, and whether the line
// opened with it. The keyword ends at the space, the `(`, the `::` or the `{`
// after it, so `iframe(1, 2)` is the call it looks like rather than a block —
// while `if::x::` keeps being read as the block it was meant to be, and dropped
// as the malformed one it is.
func cutIfKeyword(line string) (string, bool) {
	if len(line) < len(macroIfKeyword) || !strings.EqualFold(line[:len(macroIfKeyword)], macroIfKeyword) {
		return "", false
	}
	rest := line[len(macroIfKeyword):]
	if rest == "" {
		// The keyword alone: malformed, and still a block — see parseIfHeader.
		return "", true
	}
	switch rest[0] {
	case ' ', '\t', '(', ':', '{':
		return rest, true
	}
	return "", false
}

// cutParens returns what a pair of parentheses holds.
func cutParens(s string) (string, bool) {
	inner, ok := strings.CutPrefix(s, "(")
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, ")")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(inner), true
}

// parseAISlot reads `::<prompt>::`, the marker for what the AI works out rather
// than what the macro says outright, and returns the prompt inside it.
func parseAISlot(s string) (string, bool) {
	inner, ok := strings.CutPrefix(s, macroAIMarker)
	if !ok {
		return "", false
	}
	inner, ok = strings.CutSuffix(inner, macroAIMarker)
	if !ok {
		return "", false
	}
	inner = strings.TrimSpace(inner)
	return inner, inner != ""
}

// parseMacroLine parses `name(arg1, arg2)` or `name("quoted string")`.
func parseMacroLine(line string) (string, []string, bool) {
	openParen := strings.Index(line, "(")
	if openParen < 0 || !strings.HasSuffix(line, ")") {
		return "", nil, false
	}
	name := strings.TrimSpace(line[:openParen])
	if name == "" {
		return "", nil, false
	}

	argsStr := strings.TrimSpace(line[openParen+1 : len(line)-1])
	if argsStr == "" {
		return name, []string{}, true
	}

	if strings.HasPrefix(argsStr, "\"") {
		var result strings.Builder
		runes := []rune(argsStr)
		i := 1
		for i < len(runes) {
			ch := runes[i]
			if ch == '\\' {
				if i+1 < len(runes) {
					result.WriteRune(runes[i+1])
					i += 2
				} else {
					i++
				}
			} else if ch == '"' {
				break
			} else {
				result.WriteRune(ch)
				i++
			}
		}
		return name, []string{result.String()}, true
	}

	parts := strings.Split(argsStr, ",")
	args := make([]string, len(parts))
	for i, p := range parts {
		args[i] = strings.TrimSpace(p)
	}
	return name, args, true
}

// macroSettingsMissing names the settings this macro needs and hasn't got, as a
// phrase to put in front of the user ("openai_api_key", "base_url and model").
// Empty means it can run: a macro with no if never calls the model, so it needs
// nothing configured at all.
func (r *Runner) macroSettingsMissing(nodes []macroNode) string {
	if !hasMacroCondition(nodes) {
		return ""
	}
	return joinNames(r.cfg.MissingLLMSettings())
}

// hasMacroCondition reports whether any statement is an if, at any depth.
func hasMacroCondition(nodes []macroNode) bool {
	for _, node := range nodes {
		if node.isIf() || hasMacroCondition(node.body) {
			return true
		}
	}
	return false
}

// joinNames reads a list out as a phrase: "a", "a and b", "a, b and c".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// countMacroNodes counts every statement, an if and the statements inside it
// alike — what the log line reports as the size of the macro.
func countMacroNodes(nodes []macroNode) int {
	n := len(nodes)
	for _, node := range nodes {
		n += countMacroNodes(node.body)
	}
	return n
}

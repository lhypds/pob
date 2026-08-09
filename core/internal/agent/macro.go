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
// whose body only runs when its condition holds.
type macroNode struct {
	// raw is the statement as written, with any ::…:: slot still in it. It is
	// what a slot is filled into, and what is parsed once it has been.
	raw string

	action string   // action call name; empty on an if block or an unfilled statement
	args   []string // action arguments

	// slots says the statement holds at least one ::…:: and so cannot be read
	// until the replay reaches it — action and args are empty until then.
	slots bool

	isIf      bool        // if only
	condition string      // if only — the parenthesised expression, slots unfilled
	body      []macroNode // if only — what runs when the condition holds

	line int // 1-based line in macro.psl, for logs
}

// macroRun carries the state of one replay: the session it writes under, and
// how many slots have been filled so far — the count names their log
// directories.
type macroRun struct {
	sessionID string
	slots     int
}

// runMacro replays macro.psl statement by statement.
func (r *Runner) runMacro(ctx context.Context) {
	nodes := parseMacro(r.cfg.Macro())

	// Checked before anything moves: a macro whose slots cannot be filled is one
	// Pob cannot run as written, and finding that out halfway through would
	// leave the statements above the slot already played.
	if missing := r.macroSettingsMissing(nodes); missing != "" {
		message := "macro.psl has a ::…:: slot the AI must fill, and that is a model call. settings.json has no " + missing + " — set it and run the macro again."
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
	if run.slots > 0 {
		// Only a slot spends tokens; a macro without one has nothing to sum.
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

		if node.isIf {
			if r.evalMacroCondition(ctx, run, node) {
				r.runMacroNodes(ctx, run, node.body)
			}
			continue
		}

		if name, args, ok := r.resolveMacroAction(ctx, run, node); ok {
			r.runMacroAction(ctx, run.sessionID, name, args)
		}

		if delayMs := r.cfg.MacroDefaultDelay(); delayMs > 0 {
			sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond)
		}
	}
}

// resolveMacroAction fills the statement's slots, if it has any, and reads the
// result as a call. A statement that cannot be read once its slots are filled
// is logged and skipped, the way a bad line always has been.
func (r *Runner) resolveMacroAction(ctx context.Context, run *macroRun, node macroNode) (string, []string, bool) {
	if !node.slots {
		return node.action, node.args, true
	}

	filled, ok := r.fillSlots(ctx, run, node, node.raw)
	if !ok {
		applog.Logf("[%s] Macro line %d: %s — slot unfilled, skipping", run.sessionID, node.line, node.raw)
		return "", nil, false
	}

	name, args, ok := parseMacroLine(strings.TrimSpace(filled))
	if !ok {
		applog.Logf("[%s] Macro line %d: filled to %q, which does not read as a statement — skipping", run.sessionID, node.line, filled)
		return "", nil, false
	}
	applog.Logf("[%s] Macro line %d: %s -> %s", run.sessionID, node.line, node.raw, filled)
	return name, args, true
}

// evalMacroCondition works out whether an if block's condition holds. The
// condition is ordinary PSL text: a ::…:: slot the AI fills with true or false,
// or one of those two written out by hand.
//
// Anything that goes wrong — no screenshot, an unreadable answer, an answer that
// is not true or false — reads as false: the block stays unexecuted rather than
// running on a guess.
func (r *Runner) evalMacroCondition(ctx context.Context, run *macroRun, node macroNode) bool {
	expr := node.condition
	if hasSlot(expr) {
		filled, ok := r.fillSlots(ctx, run, node, expr)
		if !ok {
			applog.Logf("[%s] Macro if (%s) — slot unfilled, skipping block", run.sessionID, expr)
			return false
		}
		expr = filled
	}

	holds, read := conditionHolds(expr)
	switch {
	case !read:
		applog.Logf("[%s] Macro if (%s) -> %q is not true or false — skipping block", run.sessionID, node.condition, expr)
	case holds:
		applog.Logf("[%s] Macro if (%s) -> TRUE", run.sessionID, node.condition)
	default:
		applog.Logf("[%s] Macro if (%s) -> FALSE — skipping block", run.sessionID, node.condition)
	}
	return holds
}

// conditionHolds reads a filled-in condition. The second return says it was one
// of the two words at all; anything else is not a verdict, and the first return
// is false so the block stays unexecuted rather than running on a guess.
//
// Quotes are stripped first: a model asked for `true` sometimes answers
// `"true"`, and that is the same answer.
func conditionHolds(expr string) (holds, read bool) {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(expr), `"`)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// fillSlots replaces every ::…:: in a statement with what the AI answers.
func (r *Runner) fillSlots(ctx context.Context, run *macroRun, node macroNode, text string) (string, bool) {
	return fillSlotsWith(text, func(statement, prompt string) (string, bool) {
		if ctx.Err() != nil {
			return "", false
		}
		return r.fillSlot(run, node, statement, prompt)
	})
}

// fillSlotsWith fills a statement's slots left to right, asking `ask` for each
// one. What it is given is the statement as it stands — so the second slot of a
// line is asked about with the first one already answered, which is the state
// the statement is actually in by then.
//
// Scanning resumes past each answer, so a value that happens to hold `::` is
// text rather than another slot to fill: what the AI says is a value, never more
// of the program.
func fillSlotsWith(text string, ask func(statement, prompt string) (string, bool)) (string, bool) {
	for from := 0; ; {
		slot, found := findSlot(text, from)
		if !found {
			return text, true
		}
		value, ok := ask(text, slot.prompt)
		if !ok {
			return "", false
		}
		text = text[:slot.start] + value + text[slot.end:]
		from = slot.start + len(value)
	}
}

const macroSlotSystemPrompt = `You are filling in one slot in a Prompt Script Language (PSL) macro that Pob is replaying on this machine right now.

A PSL macro is one statement per line. A statement is either a call — move(dx, dy), drag(dx, dy), scroll(dx, dy), click(), rightClick(), doubleClick(), typeText("..."), keyPress("..."), sleep(ms), resetCursor(), take_screenshot() — or an if block: if (<condition>) { ... }.

A ::…:: marker is a slot: a prompt standing where a value would be. You are given the whole macro, the one statement being run, the prompt inside its slot, and a screenshot of the screen as it is at this moment.

Answer with the text that replaces the marker. It is substituted literally, exactly as you write it, and the statement must be valid PSL afterwards. Work out from the statement what shape the answer has to take:

- a bare number where a number goes — move(::…::, 40) wants -120, not "-120" and not "120 pixels"
- a quoted string where a whole string argument goes — typeText(::…::) wants "Hello"
- bare text where the slot sits inside a string already — typeText("Hi ::…::") wants Bob
- true or false in the condition of an if — if (::…::) wants true

Coordinates are screenshot pixels: origin top-left, x increases right, y increases down. move and drag are relative to where the cursor is now — the arrow you can see in the screenshot — so answer with the offset from it, not with an absolute position.

Go only by the screenshot and the macro. You have no memory of what the earlier statements did beyond what the picture shows. If the screenshot does not show enough to answer, say so in the reason and give the value that does the least: 0 for a number, "" for a string, false for a condition.

Respond with JSON:
  {"value": "...", "reason": "..."}

The reason is one short sentence naming what you saw.`

var macroSlotSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"value":  map[string]any{"type": "string"},
		"reason": map[string]any{"type": "string"},
	},
	"required":             []string{"value", "reason"},
	"additionalProperties": false,
}

// fillSlot asks the model what one slot should say, against a fresh screenshot
// and with the whole macro for context — what a statement means is often the
// statements around it. The judgement is kept under the session's slots/.
func (r *Runner) fillSlot(run *macroRun, node macroNode, statement, prompt string) (string, bool) {
	run.slots++
	seq := run.slots

	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Logf("[%s] Macro slot (::%s::) — no screenshot", run.sessionID, prompt)
		return "", false
	}

	applog.Logf("[%s] Macro slot (::%s::) — asking...", run.sessionID, prompt)

	messages := []map[string]any{
		{"role": "system", "content": macroSlotSystemPrompt},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "The macro:\n\n" + r.cfg.Macro() +
				"\n\nThe statement being run, line " + strconv.Itoa(node.line) + ":\n\n" + statement +
				"\n\nThe slot to fill: ::" + prompt + "::\n\nWhat replaces the marker?"},
			imagePart(shot),
		}},
	}

	result := r.llm.Chat(messages, nil, macroSlotSchema)

	// Copy before adding usage so the raw message stays as the API returned it.
	responseToSave := map[string]any{"error": result.Error}
	if result.Success {
		responseToSave = shallowCopy(result.RawAssistantMessage)
	}
	if result.Usage != nil {
		responseToSave["usage"] = result.Usage
	}

	// A half-decoded answer counts as no answer: the value is only the model's
	// when the whole thing read cleanly.
	var parsed struct {
		Value  string `json:"value"`
		Reason string `json:"reason"`
	}
	value, reason, ok := "", "", false
	switch {
	case !result.Success:
		applog.Logf("[%s] Macro slot (::%s::) — error: %s", run.sessionID, prompt, result.Error)
	case result.ContentText == "" || json.Unmarshal([]byte(result.ContentText), &parsed) != nil:
		applog.Logf("[%s] Macro slot (::%s::) — unreadable answer", run.sessionID, prompt)
	default:
		value, reason, ok = strings.TrimSpace(parsed.Value), parsed.Reason, true
	}

	r.store.SaveMacroSlot(run.sessionID, seq, node.line, statement, prompt, value, reason, ok,
		append(messages, result.RawAssistantMessage), responseToSave, shot)

	if ok {
		reasonSuffix := ""
		if reason != "" {
			reasonSuffix = " — " + reason
		}
		applog.Logf("[%s] Macro slot (::%s::) -> %s%s", run.sessionID, prompt, value, reasonSuffix)
	}
	return value, ok
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
	// macroAIMarker wraps a prompt the AI answers when the statement is reached,
	// and the answer stands where the marker was. It can go anywhere in a
	// statement — an argument, part of one, or the whole condition of an `if`.
	macroAIMarker = "::"
)

// slotRange is where one ::…:: sits in a statement: the marker spans
// text[start:end], and prompt is what it holds.
type slotRange struct {
	start, end int
	prompt     string
}

// findSlot returns the first ::…:: at or after `from`. Markers pair off left to
// right, and a pair holding nothing asks nothing: `::::` is passed over — both
// its markers used up — rather than reported as a slot, since a statement should
// only be held up by a slot there is a question in.
func findSlot(text string, from int) (slotRange, bool) {
	for i := from; i <= len(text); {
		open := strings.Index(text[i:], macroAIMarker)
		if open < 0 {
			return slotRange{}, false
		}
		open += i
		inner := open + len(macroAIMarker)

		closer := strings.Index(text[inner:], macroAIMarker)
		if closer < 0 {
			return slotRange{}, false
		}
		closer += inner

		if prompt := strings.TrimSpace(text[inner:closer]); prompt != "" {
			return slotRange{start: open, end: closer + len(macroAIMarker), prompt: prompt}, true
		}
		i = closer + len(macroAIMarker)
	}
	return slotRange{}, false
}

// hasSlot reports whether a statement holds a ::…:: the AI has to fill.
func hasSlot(text string) bool {
	_, found := findSlot(text, 0)
	return found
}

// parseMacro turns macro.psl into the statements to execute. Lines it cannot
// read are logged and dropped, the way a bad action line always has been — a
// macro runs as far as it makes sense rather than not at all.
//
// A statement holding a slot is kept as it was written and read again once the
// replay has filled it: what it says is not known until then.
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
			nodes = append(nodes, macroNode{isIf: true, condition: condition, body: body, line: lineNo, raw: trimmed})
			continue
		}

		if hasSlot(trimmed) {
			// What it says depends on what the AI answers, so it is read when
			// the replay gets to it rather than now.
			nodes = append(nodes, macroNode{raw: trimmed, slots: true, line: lineNo})
			continue
		}

		name, args, ok := parseMacroLine(trimmed)
		if !ok {
			applog.Logf("Macro: skipping line: %s", trimmed)
			continue
		}
		nodes = append(nodes, macroNode{raw: trimmed, action: name, args: args, line: lineNo})
	}

	if depth > 0 {
		applog.Log("Macro: if block left open — the end of the macro closes it")
	}
	return nodes, i
}

// parseIfHeader reads `if (<condition>) {` and returns the condition, which is
// PSL text rather than a value: a ::…:: slot the AI fills with true or false, or
// one of those written out. The second return says the line opens a block at all
// — a line that starts with the keyword opens one whether or not the rest of it
// is well formed, and an empty condition is what says it was not.
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
	// A slot with nothing in it asks nothing, and `if ()` says nothing: either
	// way there is no condition, and the block goes with it.
	if expr != "" && !hasSlot(expr) && !isBoolLiteral(expr) {
		return "", true
	}
	return expr, true
}

// isBoolLiteral reports whether a condition is written out rather than asked.
func isBoolLiteral(expr string) bool {
	switch strings.ToLower(strings.TrimSpace(expr)) {
	case "true", "false":
		return true
	}
	return false
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
// Empty means it can run: a macro with no slot never calls the model, so it
// needs nothing configured at all.
func (r *Runner) macroSettingsMissing(nodes []macroNode) string {
	if !hasMacroSlot(nodes) {
		return ""
	}
	return joinNames(r.cfg.MissingLLMSettings())
}

// hasMacroSlot reports whether any statement holds a ::…:: slot, at any depth.
func hasMacroSlot(nodes []macroNode) bool {
	for _, node := range nodes {
		if node.slots || hasSlot(node.condition) || hasMacroSlot(node.body) {
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

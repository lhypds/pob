package agent

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/psl"
)

// macroNode is one statement of a macro: either an action call, or an if block
// whose body only runs when its condition holds.
type macroNode struct {
	// raw is the statement as written, with any :: … :: slot still in it. It is
	// the line psl fills, and what is parsed once it has been.
	raw string

	action string   // action call name; empty on an if block or an unfilled statement
	args   []string // action arguments

	// slots says the statement holds at least one :: … :: and so cannot be read
	// until the replay reaches it — action and args are empty until then.
	slots bool

	isIf      bool        // if only
	condition string      // if only — the parenthesised expression, slots unfilled
	body      []macroNode // if only — what runs when the condition holds

	line int // 1-based line in macro.psl, for logs and for finding the line back
}

// macroRun carries the state of one replay: the session it writes under, the
// macro as it now stands, and how many slots have been filled so far, since the
// count names their log directories.
//
// source is the file itself, and psl is handed it whole and unaltered — no
// preamble, nothing rewritten, the macro as it was typed. That works because
// psl fills the first slot in the file and Pob replays the file top to bottom:
// the two agree on which slot is next as long as the file carries its answers
// forward, which is what record does after every run. A statement the replay is
// finished with and did not fill — a skipped block, a run that failed — is spent
// instead, since a slot left on it would be the one psl reaches for next.
type macroRun struct {
	sessionID string
	source    string
	slots     int
}

// line returns the macro's line as it now stands, 1-based, or "" past the end.
func (run *macroRun) line(n int) string {
	lines := strings.Split(run.source, "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}

// record writes a filled statement back into the macro, so the next run is
// handed a file with this answer already in it.
//
// An answer of several lines is folded onto one. Every statement is found by
// its line number, and a file that gained a line would put every statement
// under it at the wrong one — while an answer that ran to several lines does
// not read as a statement anyway, and is on its way to being logged and
// skipped.
func (run *macroRun) record(n int, filled string) {
	lines := strings.Split(run.source, "\n")
	if n < 1 || n > len(lines) {
		return
	}
	lines[n-1] = strings.ReplaceAll(filled, "\n", " ")
	run.source = strings.Join(lines, "\n")
}

// spend writes the slots on a line out of the macro — `:: x ::` becomes `<x>` —
// leaving the instruction there to be read but nothing psl would fill.
//
// It is what Pob says about a statement it is done with and did not fill: the
// body of a block whose condition did not hold, a statement whose own fill
// failed, a line that never parsed. psl fills the first slot in the file, so a
// slot left behind on one of those is a slot answered in place of the statement
// the replay is actually waiting on — with the wrong screenshot, in the wrong
// place, for a statement that is not going to run.
func (run *macroRun) spend(n int) {
	if line := run.line(n); psl.HasSlot(line) {
		run.record(n, psl.Neutralize(line))
	}
}

// spendBlock spends a block of statements that will not run, and the blocks
// inside it.
func (run *macroRun) spendBlock(nodes []macroNode) {
	for _, node := range nodes {
		run.spend(node.line)
		run.spendBlock(node.body)
	}
}

// spendUncovered spends every line no statement came out of. A line Pob could
// not read is a line it will never run — and one holding a slot, such as the
// body of an if whose header was malformed, would otherwise be filled in place
// of a statement that does run.
func (run *macroRun) spendUncovered(nodes []macroNode) {
	covered := map[int]bool{}
	var walk func([]macroNode)
	walk = func(nodes []macroNode) {
		for _, node := range nodes {
			covered[node.line] = true
			walk(node.body)
		}
	}
	walk(nodes)

	for n := 1; n <= strings.Count(run.source, "\n")+1; n++ {
		if !covered[n] {
			run.spend(n)
		}
	}
}

// runMacro replays macro.psl statement by statement.
func (r *Runner) runMacro(ctx context.Context) {
	source := r.cfg.Macro()
	nodes := parseMacro(source)

	// Checked before anything moves: a macro whose slots cannot be filled is one
	// Pob cannot run as written, and finding that out halfway through would
	// leave the statements above the slot already played.
	if hasMacroSlot(nodes) && !r.psl.Available() {
		message := "macro.psl has a :: … :: slot, and Pob fills those by running the psl compiler. " +
			"psl was not found — install it (see https://github.com/pob/psl), or set \"psl\" in " +
			"settings.json to the path of the executable, and run the macro again."
		applog.Logf("Macro not run: %s", message)
		r.br.ShowAlert("psl needed", message)
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

	run := &macroRun{sessionID: sessionID, source: source}
	run.spendUncovered(nodes)
	r.runMacroNodes(ctx, run, nodes)

	// The macro with every answer in it, kept beside the one that was written:
	// what the replay actually ran, rather than what it was asked to.
	r.store.SaveCompiledMacro(sessionID, run.source)

	r.store.SaveSessionStartEndTimes(sessionID, macroStart, time.Now())
	applog.Logf("[%s] Macro session times saved", sessionID)
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
			} else {
				// Nothing in it runs, so nothing in it is asked about.
				run.spendBlock(node.body)
			}
			run.spend(node.line)
			continue
		}

		if name, args, ok := r.resolveMacroAction(ctx, run, node); ok {
			r.runMacroAction(ctx, run.sessionID, name, args)
		}
		// A no-op when the statement filled: what it spends is the slot of one
		// that did not, which the replay is nonetheless done with.
		run.spend(node.line)

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

	filled, ok := r.fillStatement(ctx, run, node)
	if !ok {
		return "", nil, false
	}

	name, args, ok := parseMacroLine(strings.TrimSpace(filled))
	if !ok {
		applog.Logf("[%s] Macro line %d: filled to %q, which does not read as a statement — skipping", run.sessionID, node.line, filled)
		return "", nil, false
	}
	return name, args, true
}

// evalMacroCondition works out whether an if block's condition holds. The
// condition is ordinary PSL text: a :: … :: slot psl fills with true or false,
// or one of those two written out by hand.
//
// Anything that goes wrong — psl failing, an answer that is not true or false —
// reads as false: the block stays unexecuted rather than running on a guess.
func (r *Runner) evalMacroCondition(ctx context.Context, run *macroRun, node macroNode) bool {
	expr := node.condition
	if psl.HasSlot(expr) {
		filled, ok := r.fillStatement(ctx, run, node)
		if !ok {
			applog.Logf("[%s] Macro if (%s) — slot unfilled, skipping block", run.sessionID, expr)
			return false
		}
		// The whole header line comes back, so the condition is read out of it
		// again rather than assumed to be what replaced the slot.
		expr, _ = parseIfHeader(strings.TrimSpace(filled))
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

// fillStatement has psl fill every slot in one statement, one run each, and
// returns the statement as it then reads.
//
// The statement is taken from the macro as it now stands rather than as it was
// written, since a slot filled a moment ago is already in there.
func (r *Runner) fillStatement(ctx context.Context, run *macroRun, node macroNode) (string, bool) {
	statement := run.line(node.line)
	for psl.HasSlot(statement) {
		if ctx.Err() != nil {
			return "", false
		}
		filled, ok := r.fillOneSlot(ctx, run, node, statement)
		if !ok {
			return "", false
		}
		if filled == statement {
			applog.Logf("[%s] Macro line %d: psl returned the statement unchanged — giving up on it", run.sessionID, node.line)
			return "", false
		}
		statement = filled
	}
	applog.Logf("[%s] Macro line %d: %s -> %s", run.sessionID, node.line, node.raw, strings.TrimSpace(statement))
	return statement, true
}

// fillOneSlot runs psl once over the macro and returns the statement with its
// first slot filled.
func (r *Runner) fillOneSlot(ctx context.Context, run *macroRun, node macroNode, statement string) (string, bool) {
	run.slots++
	seq := run.slots

	slot, found := psl.FindSlot(statement, 0)
	if !found {
		return statement, true
	}

	// psl fills the first slot in the file, and the file is handed over whole,
	// so the first slot in it has to be this statement's. Checked here rather
	// than found out afterwards: a statement that comes back untouched says only
	// that something else was answered, not what.
	source := run.source
	targetLine := node.line - 1
	if live, found := liveSlotLine(source); !found || live != targetLine {
		applog.Logf("[%s] Macro slot (%s) — psl would fill a slot on line %d, not this statement on line %d; not running it",
			run.sessionID, slot.Instruction, live+1, node.line)
		r.store.SaveMacroSlot(run.sessionID, seq, node.line, statement, slot.Instruction, "", "", false,
			"the first slot in the macro is not this statement's", nil)
		return "", false
	}

	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Logf("[%s] Macro slot (%s) — no screenshot", run.sessionID, slot.Instruction)
		return "", false
	}

	applog.Logf("[%s] Macro slot (%s) — running psl...", run.sessionID, slot.Instruction)

	result, err := r.psl.Fill(ctx, psl.Request{Source: source, Name: "macro.psl", Image: shot})
	if err != nil {
		applog.Logf("[%s] Macro slot (%s) — psl failed: %v", run.sessionID, slot.Instruction, err)
		r.store.SaveMacroSlot(run.sessionID, seq, node.line, statement, slot.Instruction, "", "", false, err.Error(), shot)
		return "", false
	}

	filled, ok := extractLine(source, result.Source, targetLine)
	if !ok {
		applog.Logf("[%s] Macro slot (%s) — could not read the filled statement back", run.sessionID, slot.Instruction)
		r.store.SaveMacroSlot(run.sessionID, seq, node.line, statement, slot.Instruction, "", result.Model, false, result.Output, shot)
		return "", false
	}

	applog.Logf("[%s] Macro slot (%s) -> %s  [%s, %s]", run.sessionID, slot.Instruction, strings.TrimSpace(filled),
		result.Model, result.Duration.Round(time.Millisecond))
	r.store.SaveMacroSlot(run.sessionID, seq, node.line, statement, slot.Instruction, filled, result.Model, true, result.Output, shot)

	// The answer goes into the macro, which is what the next run is handed: with
	// this slot no longer in the file, the first one left is the next statement's
	// — or the second slot of this one, if it has two.
	run.record(node.line, filled)
	return filled, true
}

// psl is handed the macro and nothing else — no preamble of Pob's own, no
// statement rewritten, the file as it was typed, with the screenshot alongside
// it as the slot's image. What a value has to look like is what the statement
// around the slot already says, and psl's own prompting is what says the rest.

// liveSlotLine returns the 0-based line of the slot psl would fill in the file
// it is handed.
func liveSlotLine(source string) (int, bool) {
	slot, found := psl.FindCompilerSlot(source, 0)
	if !found {
		return 0, false
	}
	return strings.Count(source[:slot.Start], "\n"), true
}

// extractLine reads the filled statement back out of the file psl rewrote.
//
// The answer replaces a span inside one line, but a model that answered with
// more than one line would leave the file longer than it was. Everything after
// the statement is untouched either way, so what the statement became is what
// sits between the lines before it and the lines after it.
//
// Trailing newlines are trimmed off both first. Whether a rewritten file ends
// with one is the sort of thing a tool that rewrites files decides for itself,
// and counting from the end means a single extra blank line would otherwise
// hand back the statement below as well.
func extractLine(before, after string, line int) (string, bool) {
	beforeLines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	afterLines := strings.Split(strings.TrimRight(after, "\n"), "\n")
	if line < 0 || line >= len(beforeLines) {
		return "", false
	}
	tail := len(beforeLines) - line - 1
	end := len(afterLines) - tail
	if end <= line {
		return "", false
	}
	return strings.Join(afterLines[line:end], "\n"), true
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

// macroIfKeyword opens a conditional block: `if (<expression>) {`, closed by a
// line holding nothing but `}`. Matched whatever its case, so `IF` opens a block
// too: the alternative to recognising it is running the body unguarded, which is
// the one thing a condition was written to prevent.
const macroIfKeyword = "if"

// parseMacro turns macro.psl into the statements to execute. Lines it cannot
// read are logged and dropped, the way a bad action line always has been — a
// macro runs as far as it makes sense rather than not at all.
//
// A statement holding a slot is kept as it was written and read again once psl
// has filled it: what it says is not known until then.
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
				applog.Logf("Macro line %d: if wants a condition in parentheses — if (:: … ::) { — skipping its block", lineNo)
				continue
			}
			nodes = append(nodes, macroNode{isIf: true, condition: condition, body: body, line: lineNo, raw: trimmed})
			continue
		}

		if psl.HasSlot(trimmed) {
			// What it says depends on what psl answers, so it is read when the
			// replay gets to it rather than now.
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
// PSL text rather than a value: a :: … :: slot psl fills with true or false, or
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
	// A condition is either asked or written out. Anything else is neither, and
	// the block goes with it rather than running unguarded.
	if expr != "" && !psl.HasSlot(expr) && !isBoolLiteral(expr) {
		return "", true
	}
	return expr, true
}

// isBoolLiteral reports whether a condition is written out rather than asked.
// It is conditionHolds' own answer to that, so a condition Pob would read is
// never one it refuses to parse first — a model asked for `true` sometimes
// answers `"true"`, and both have to travel the same path.
func isBoolLiteral(expr string) bool {
	_, read := conditionHolds(expr)
	return read
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

// hasMacroSlot reports whether any statement holds a :: … :: slot, at any depth.
func hasMacroSlot(nodes []macroNode) bool {
	for _, node := range nodes {
		if node.slots || psl.HasSlot(node.condition) || hasMacroSlot(node.body) {
			return true
		}
	}
	return false
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

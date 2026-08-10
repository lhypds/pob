package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/psl"
)

// macroNode is one statement of a macro: an action call, an if block whose body
// only runs when its condition holds, or a loop block whose body runs again and
// again.
type macroNode struct {
	// raw is the statement as written, with any :: … :: slot still in it. It is
	// the line psl fills, and what is parsed once it has been.
	raw string

	action string   // action call name; empty on a block or an unfilled statement
	args   []string // action arguments

	// slots says the statement holds at least one :: … :: and so cannot be read
	// until the replay reaches it — action and args are empty until then.
	slots bool

	isIf   bool // if only
	isLoop bool // loop only

	// condition is the parenthesised expression, slots unfilled: an if always has
	// one, a loop only when it was written with one.
	condition string
	count     int         // loop only — the most passes it may make
	body      []macroNode // if and loop — the statements the block holds

	line int // 1-based line in macro.psl, for logs and for finding the line back
}

// macroRun carries the state of one file being replayed: the session it writes
// under, the file as it now stands, and how many slots have been filled so far,
// since the count names their log directories.
//
// source is the file itself, and psl is handed it whole and unaltered — no
// preamble, nothing rewritten, the macro as it was typed. That works because
// psl fills the first slot in the file and Pob replays the file top to bottom:
// the two agree on which slot is next as long as the file carries its answers
// forward, which is what record does after every run. A statement the replay is
// finished with and did not fill — a skipped block, a run that failed — is spent
// instead, since a slot left on it would be the one psl reaches for next.
//
// One file, not one run: a call() starts a run of its own over the file it
// names, so that each file is the one psl is handed and each keeps its own line
// numbers. What belongs to the replay as a whole rather than to a file hangs off
// the root — the slot count that numbers the log directories, and whether a stop
// has been reached, which ends every file at once.
type macroRun struct {
	sessionID string
	source    string
	slots     int

	// written is the macro as it was written, kept line by line: what a loop puts
	// back before a pass, so that the statements it is about to replay ask their
	// slots again instead of repeating the answers of the pass before.
	written []string

	// prompt is the briefing that goes over beside the file on every fill, and
	// is empty when this psl is older than the flag that takes it — settled once
	// for the run rather than asked about at each slot.
	prompt string

	// name is what the file is called — what psl is told it is called, and what
	// the log puts in front of a line number once more than one file is in play.
	name string
	// path is the file itself, and dir the directory it was read from: what a
	// relative path in a call() inside it is resolved against, and what says a
	// file is already running when one is reached again.
	path string
	dir  string
	// parent is the run whose call() started this one, nil in the macro itself.
	parent *macroRun

	// stopped says a stop statement has been reached. Root only — read and
	// written through halted and halt, since what it ends is the whole replay
	// and not the file the statement happened to be in.
	stopped bool
}

// newMacroRun starts a replay over the macro as it was written.
//
// The one thing not as it was written is the markers inside its comments, which
// are taken apart before anything else happens: psl fills the first slot in the
// file, and a comment is the one part of a file that is never waiting on an
// answer. It is done here so that written carries it too — otherwise a loop
// would put a comment's markers back into the file on every pass.
func newMacroRun(sessionID, path, source, prompt string) *macroRun {
	source = neutralizeComments(source)
	return &macroRun{
		sessionID: sessionID,
		source:    source,
		written:   strings.Split(source, "\n"),
		prompt:    prompt,
		name:      filepath.Base(path),
		path:      path,
		dir:       filepath.Dir(path),
	}
}

// newCallRun starts a replay over a file a call() named, under the run that
// reached the call.
func newCallRun(parent *macroRun, path, source string) *macroRun {
	run := newMacroRun(parent.sessionID, path, source, parent.prompt)
	run.parent = parent
	return run
}

// root is the run over the macro itself — this one, unless a call() brought it
// in.
func (run *macroRun) root() *macroRun {
	for run.parent != nil {
		run = run.parent
	}
	return run
}

// nextSlot numbers the next fill. The count is the whole replay's, so the slot
// directories of a session run straight through however many files filled them.
func (run *macroRun) nextSlot() int {
	root := run.root()
	root.slots++
	return root.slots
}

// halt ends the replay — this file and every file above it. See runMacroAction's
// stop.
func (run *macroRun) halt() { run.root().stopped = true }

// halted reports whether a stop has been reached, anywhere in the replay.
func (run *macroRun) halted() bool { return run.root().stopped }

// depth is how many call()s deep this file is, 0 in the macro itself.
func (run *macroRun) depth() int {
	n := 0
	for r := run.parent; r != nil; r = r.parent {
		n++
	}
	return n
}

// running reports whether a file is already being replayed further up — what
// says a call() would put the replay back into a file it is standing in, which
// is a macro that never ends rather than one that runs twice.
func (run *macroRun) running(path string) bool {
	for r := run; r != nil; r = r.parent {
		if r.path == path {
			return true
		}
	}
	return false
}

// where names a line for the log. A replay of one file says "line 4", the way it
// always has; once a call() is in play the file is named too, since the numbers
// start again in each one.
func (run *macroRun) where(line int) string {
	if run.parent == nil {
		return fmt.Sprintf("line %d", line)
	}
	return fmt.Sprintf("%s line %d", run.name, line)
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

// restore puts a line back the way the macro was written, slots and all. It is
// the one thing that ever writes a slot into the file rather than out of it, and
// only a loop does it.
//
// A slot filled on the last pass holds an answer read off the screen as it was
// then, and asking again is the whole point of a pass: the offset to a button
// that has moved, a condition that has to be able to turn false. What goes back
// is the one line that was there, so every statement is still found at the line
// number it was parsed at.
func (run *macroRun) restore(n int) {
	lines := strings.Split(run.source, "\n")
	if n < 1 || n > len(lines) || n > len(run.written) {
		return
	}
	lines[n-1] = run.written[n-1]
	run.source = strings.Join(lines, "\n")
}

// restoreBlock restores a block of statements, and the blocks inside it.
func (run *macroRun) restoreBlock(nodes []macroNode) {
	for _, node := range nodes {
		run.restore(node.line)
		run.restoreBlock(node.body)
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
	path := r.cfg.MacroFile()
	source := r.cfg.Macro()
	nodes := parseMacro(source)

	hasSlots := macroNeedsPSL(nodes, filepath.Dir(path), map[string]bool{path: true})

	// Checked before anything moves: a macro whose slots cannot be filled is one
	// Pob cannot run as written, and finding that out halfway through would
	// leave the statements above the slot already played.
	if hasSlots && !r.psl.Available() {
		message := "macro.psl, or a file it calls, has a :: … :: slot, and Pob fills those by running " +
			"the psl compiler. psl was not found — install it (see https://github.com/pob/psl), or set " +
			"\"psl\" in settings.json to the path of the executable, and run the macro again."
		applog.Logf("Macro not run: %s", message)
		r.br.ShowAlert("psl needed", message)
		return
	}

	// What the vocabulary buys is a better answer rather than the run itself, so
	// a psl too old to be told it goes on filling the way it always did — from
	// the statement and the screenshot — with a line in the log saying what it
	// was not given.
	prompt := macroPrompt
	if hasSlots && !r.psl.SupportsPrompt() {
		prompt = ""
		applog.Log("Macro: this psl does not take --prompt, so the slots are filled without a description of the macro vocabulary — update psl to have it sent")
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

	run := newMacroRun(sessionID, path, source, prompt)
	run.spendUncovered(nodes)
	r.runMacroNodes(ctx, run, nodes)

	// The macro with every answer in it, kept beside the one that was written:
	// what the replay actually ran, rather than what it was asked to.
	r.store.SaveCompiledMacro(sessionID, run.source)

	r.store.SaveSessionStartEndTimes(sessionID, macroStart, time.Now())
	applog.Logf("[%s] Macro session times saved", sessionID)
	applog.Log("Macro execution complete")

	// A run that was stopped never reached its end, so nothing is announced:
	// the hook is what says the macro finished. A stop statement is the other
	// thing — the macro naming its own end rather than someone pulling it out
	// from under the cursor — so that one announces like any other finish.
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
		// The two ways a replay ends before its last statement: Stop, and a stop
		// statement it reached. Both are checked here rather than at each kind of
		// statement, so a stop deep inside a block unwinds the blocks around it and
		// the files above them the same way the Stop button does.
		if ctx.Err() != nil || run.halted() {
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

		if node.isLoop {
			r.runMacroLoop(ctx, run, node)
			continue
		}

		if name, args, ok := r.resolveMacroAction(ctx, run, node); ok {
			r.runMacroAction(ctx, run, name, args)
		}
		// A no-op when the statement filled: what it spends is the slot of one
		// that did not, which the replay is nonetheless done with.
		run.spend(node.line)

		// Immediately is what stop says, and the delay between one call and the
		// next is the last thing the statement before it would otherwise cost.
		if run.halted() {
			return
		}

		if delayMs := r.cfg.MacroDefaultDelay(); delayMs > 0 {
			sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond)
		}
	}
}

// runMacroLoop replays a loop's body, up to the count in its header and no
// further.
//
// A loop written with a condition checks it before every pass, the first one
// included, and ends the moment it does not hold: the count is the bound on a
// loop that could otherwise not end, not the number of passes to make. A loop
// written without one makes exactly that many passes.
//
// Every pass after the first puts the loop's statements back the way they were
// written before it starts. That is what makes a pass a pass rather than a
// replay of the first one's answers: the screen the slots are about is the
// screen the pass before them changed, and a condition answered once could never
// turn false.
//
// Restoring is safe in the one way it has to be. psl fills the first slot in the
// file, and what goes back into it is the header of the block the replay is
// standing on and the statements under that header — everything above is filled
// or spent and everything below is untouched — so the first slot left is still
// the one the replay is waiting on.
func (r *Runner) runMacroLoop(ctx context.Context, run *macroRun, node macroNode) {
	label := macroBlockLabel(node)
	passes := 0

	for passes < node.count {
		if ctx.Err() != nil || run.halted() {
			break
		}
		if passes > 0 {
			run.restore(node.line)
			run.restoreBlock(node.body)
		}
		// No condition is a condition that always holds, so `loop (3)` and
		// `loop (true, 3)` are one and the same loop.
		if node.condition != "" && !r.evalMacroCondition(ctx, run, node) {
			break
		}
		passes++
		applog.Logf("[%s] Macro %s — pass %d of %d", run.sessionID, label, passes, node.count)
		r.runMacroNodes(ctx, run, node.body)
	}

	// A loop a stop ended on its last allowed pass did not run out of passes, and
	// saying so would be the log reporting a bound that was never reached.
	if passes == node.count && node.condition != "" && !run.halted() {
		applog.Logf("[%s] Macro %s — %d passes, the count the loop was given", run.sessionID, label, passes)
	}

	// However it ended, the loop is done being asked about. A pass that was
	// restored and then not run holds its slots again, and left there they are
	// what psl would fill next — in place of the statement under the block.
	run.spendBlock(node.body)
	run.spend(node.line)
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

	// The comment on the end of the statement came back with it, and a statement
	// is read the same way whether it was written out or filled in.
	name, args, ok := readMacroStatement(strings.TrimSpace(stripLine(filled)))
	if !ok {
		applog.Logf("[%s] Macro %s: filled to %q, which does not read as a statement — skipping", run.sessionID, run.where(node.line), filled)
		return "", nil, false
	}
	return name, args, true
}

// evalMacroCondition works out whether a block's condition holds — an if's, or
// the one a loop checks before each pass. The condition is ordinary PSL text: a
// :: … :: slot psl fills with true or false, or one of those two written out by
// hand.
//
// Anything that goes wrong — psl failing, an answer that is not true or false —
// reads as false: the block stays unexecuted, or the loop ends, rather than
// running on a guess.
func (r *Runner) evalMacroCondition(ctx context.Context, run *macroRun, node macroNode) bool {
	label := macroBlockLabel(node)
	// What a condition that does not hold means for the block it heads.
	no := "skipping block"
	if node.isLoop {
		no = "the loop ends here"
	}

	expr := node.condition
	if psl.HasSlot(expr) {
		filled, ok := r.fillStatement(ctx, run, node)
		if !ok {
			applog.Logf("[%s] Macro %s — slot unfilled, %s", run.sessionID, label, no)
			return false
		}
		// The whole header line comes back, so the condition is read out of it
		// again rather than assumed to be what replaced the slot.
		expr = readCondition(node, strings.TrimSpace(filled))
	}

	holds, read := conditionHolds(expr)
	switch {
	case !read:
		applog.Logf("[%s] Macro %s -> %q is not true or false — %s", run.sessionID, label, expr, no)
	case holds:
		applog.Logf("[%s] Macro %s -> TRUE", run.sessionID, label)
	default:
		applog.Logf("[%s] Macro %s -> FALSE — %s", run.sessionID, label, no)
	}
	return holds
}

// readCondition reads the condition back out of a filled-in header line, by the
// same parse that read it out of the written one. A header that no longer reads
// as a header hands back nothing, which is not one of the two words and so is
// not a verdict.
func readCondition(node macroNode, header string) string {
	header = strings.TrimSpace(stripLine(header))
	if node.isLoop {
		condition, _, _ := parseLoopHeader(header)
		return condition
	}
	condition, _ := parseIfHeader(header)
	return condition
}

// macroBlockLabel names a block in the log the way it is written, so a line
// about a condition says which block it was the condition of.
func macroBlockLabel(node macroNode) string {
	switch {
	case node.isLoop && node.condition == "":
		return fmt.Sprintf("loop (%d)", node.count)
	case node.isLoop:
		return fmt.Sprintf("loop (%s, %d)", node.condition, node.count)
	default:
		return fmt.Sprintf("if (%s)", node.condition)
	}
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
			applog.Logf("[%s] Macro %s: psl returned the statement unchanged — giving up on it", run.sessionID, run.where(node.line))
			return "", false
		}
		statement = filled
	}
	applog.Logf("[%s] Macro %s: %s -> %s", run.sessionID, run.where(node.line), node.raw, strings.TrimSpace(statement))
	return statement, true
}

// fillOneSlot runs psl once over the macro and returns the statement with its
// first slot filled.
func (r *Runner) fillOneSlot(ctx context.Context, run *macroRun, node macroNode, statement string) (string, bool) {
	seq := run.nextSlot()

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
		applog.Logf("[%s] Macro slot (%s) — psl would fill a slot on line %d, not this statement on %s; not running it",
			run.sessionID, slot.Instruction, live+1, run.where(node.line))
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false,
			"the first slot in the file is not this statement's", nil)
		return "", false
	}

	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Logf("[%s] Macro slot (%s) — no screenshot", run.sessionID, slot.Instruction)
		return "", false
	}

	// What the model is shown, which is the screenshot itself unless image_scale
	// asks for less of it. The shrunken one is what goes in the log too: the
	// picture worth keeping beside an answer is the one it was read off.
	scale := r.cfg.ImageScale()
	if smaller, w, h, err := shrinkPNG(shot, scale); err != nil {
		applog.Logf("[%s] Macro slot (%s) — could not scale the screenshot (%v), sending it whole", run.sessionID, slot.Instruction, err)
		scale = 1
	} else if w > 0 {
		applog.Logf("[%s] Macro slot (%s) — %s", run.sessionID, slot.Instruction, scaleNote(w, h, scale))
		shot = smaller
	} else {
		scale = 1
	}

	applog.Logf("[%s] Macro slot (%s) — running psl...", run.sessionID, slot.Instruction)

	result, err := r.psl.Fill(ctx, psl.Request{Source: source, Name: run.name, Image: shot, Prompt: run.prompt})
	if err != nil {
		applog.Logf("[%s] Macro slot (%s) — psl failed: %v", run.sessionID, slot.Instruction, err)
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false, err.Error(), shot)
		return "", false
	}

	filled, ok := extractLine(source, result.Source, targetLine)
	if !ok {
		applog.Logf("[%s] Macro slot (%s) — could not read the filled statement back", run.sessionID, slot.Instruction)
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", result.Model, false, result.Output, shot)
		return "", false
	}

	// The answer was read off a smaller picture, so the distances in it are that
	// picture's. Grown back here rather than at the click, so that the macro,
	// the log and the compiled file all say the same screen pixels a macro
	// written by hand would.
	if grown, done := rescaleFilled(statement, slot.Start, slot.End, filled, scale); done {
		applog.Logf("[%s] Macro slot (%s) -> %s scaled back to %s", run.sessionID, slot.Instruction,
			strings.TrimSpace(filled), strings.TrimSpace(grown))
		filled = grown
	}

	applog.Logf("[%s] Macro slot (%s) -> %s  [%s, %s]", run.sessionID, slot.Instruction, strings.TrimSpace(filled),
		result.Model, result.Duration.Round(time.Millisecond))
	r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, filled, result.Model, true, result.Output, shot)

	// The answer goes into the macro, which is what the next run is handed: with
	// this slot no longer in the file, the first one left is the next statement's
	// — or the second slot of this one, if it has two.
	run.record(node.line, filled)
	return filled, true
}

// Nothing of Pob's is written into the macro: no preamble, no statement
// rewritten, the file as it was typed. What travels beside it is the screenshot,
// as the slot's image, and macroPrompt, as psl's --prompt — what the calls in
// the file are and what their arguments mean, which is what psl takes a briefing
// on an API for. What a value has to look like is still what the statement
// around the slot says, and psl's own prompting is what says the rest.

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

func (r *Runner) runMacroAction(ctx context.Context, run *macroRun, name string, args []string) {
	sessionID := run.sessionID
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

	case macroStopKeyword:
		// Nothing under this runs — not the statements after it, not the rest of
		// the block it is in, and not the file that called the file it is in. What
		// it leaves behind is a finished run rather than an abandoned one, so the
		// session is written out and stop_hook fires the way it does at the end.
		applog.Logf("[%s] Macro stop — the run ends here", sessionID)
		run.halt()

	case macroCallKeyword:
		if len(args) == 0 {
			applog.Logf("[%s] Macro call wants the path of a PSL file — call(\"other.psl\")", sessionID)
			return
		}
		r.runMacroCall(ctx, run, args[0])

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

// maxCallDepth is how many call()s deep a replay may go. A macro split across a
// few files is a macro someone wrote in pieces; eight files down is past the
// depth anyone meant, and the bound is what turns a mistake nothing else catches
// into a line in the log.
const maxCallDepth = 8

// runMacroCall replays another PSL file where the call() stands, statement by
// statement, and comes back to the statement under it.
//
// The file is read at the moment the call is reached rather than once at the
// start, so a call inside a loop replays the file as it was written on every
// pass — the same thing restoring does for a loop's own statements, and had here
// for nothing, since the file arrives from disk each time.
//
// It is a run of its own, because psl fills the first slot in the file it is
// handed and the file the called statements are in is not the one that called
// them: the called file is what goes over, its own line numbers are what the
// slots come back to, and its own name is what the log and psl are told. What it
// shares with the run above it is the session — the slot directories are
// numbered straight through — and the stop that ends every file at once.
func (r *Runner) runMacroCall(ctx context.Context, run *macroRun, arg string) {
	sessionID := run.sessionID

	path, err := resolveCallPath(run.dir, arg)
	if err != nil {
		applog.Logf("[%s] Macro call(%q) — %v; not running it", sessionID, arg, err)
		return
	}
	// A file that reaches itself, directly or round a ring of calls, is a replay
	// with no end in it. The depth bound catches the rest — a chain of files that
	// each call a new one.
	if run.running(path) {
		applog.Logf("[%s] Macro call(%q) — %s is already running and would call itself; not running it",
			sessionID, arg, path)
		return
	}
	if run.depth() >= maxCallDepth {
		applog.Logf("[%s] Macro call(%q) — %d files deep, which is as far as call goes; not running it",
			sessionID, arg, maxCallDepth)
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		applog.Logf("[%s] Macro call(%q) — cannot read %s: %v; skipping it", sessionID, arg, path, err)
		return
	}

	source := string(data)
	nodes := parseMacro(source)
	called := newCallRun(run, path, source)
	called.spendUncovered(nodes)

	applog.Logf("[%s] Macro call(%q) -> %s (%d actions)", sessionID, arg, path, countMacroNodes(nodes))
	r.runMacroNodes(ctx, called, nodes)
	if !called.halted() {
		applog.Logf("[%s] Macro call(%q) — %s done", sessionID, arg, called.name)
	}
}

// resolveCallPath works out which file a call() names.
//
// A relative path is relative to the directory of the file the call is written
// in, not to wherever Pob happens to be running: `call("../shared.psl")` in
// ~/.pob/<instance>/macro.psl is ~/.pob/shared.psl, and means that whoever
// started the replay. A path beginning with ~/ is under the home directory, the
// way it is everywhere else it is written down.
func resolveCallPath(dir, arg string) (string, error) {
	path := strings.TrimSpace(arg)
	if path == "" {
		return "", errors.New("call wants the path of a PSL file")
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Abs(path)
}

// macroIfKeyword opens a conditional block: `if (<expression>) {`, closed by a
// line holding nothing but `}`. Matched whatever its case, so `IF` opens a block
// too: the alternative to recognising it is running the body unguarded, which is
// the one thing a condition was written to prevent.
const macroIfKeyword = "if"

// macroLoopKeyword opens a block that runs again and again: `loop (<count>) {`,
// or `loop (<condition>, <count>) {`, closed the same way. Matched whatever its
// case for the same reason as `if` — a header that went unrecognised would leave
// the body of the block to run once, unbounded and unguarded.
const macroLoopKeyword = "loop"

// macroStopKeyword ends the replay where it stands. It is written `stop`, and
// `stop()` is read too: the empty parentheses say nothing the bare word does not,
// and a statement that ends a run is the last one to refuse over punctuation.
// Spelled lowercase like the rest of the vocabulary, and read that way — a name
// is a name, and the block keywords are the only two that take any case.
const macroStopKeyword = "stop"

// macroCallKeyword replays another PSL file where it stands:
// `call("../shared.psl")`. See runMacroCall.
const macroCallKeyword = "call"

// parseMacro turns macro.psl into the statements to execute. Lines it cannot
// read are logged and dropped, the way a bad action line always has been — a
// macro runs as far as it makes sense rather than not at all.
//
// A statement holding a slot is kept as it was written and read again once psl
// has filled it: what it says is not known until then.
//
// The comments come out first and the lines stay where they are, so a statement
// is still found at the line number it was written on — see comment.go.
func parseMacro(text string) []macroNode {
	nodes, _ := parseMacroBlock(codeLines(text), 0, 0)
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

		if condition, count, isLoop := parseLoopHeader(trimmed); isLoop {
			// Read either way, like the if: a header Pob cannot read takes its
			// block with it, rather than leaving the body to run once when what was
			// written asks for it to run until something is true.
			var body []macroNode
			body, i = parseMacroBlock(lines, i, depth+1)
			if count < 1 {
				applog.Logf("Macro line %d: loop wants a count in parentheses — loop (3) { or loop (:: … ::, 3) { — skipping its block", lineNo)
				continue
			}
			nodes = append(nodes, macroNode{isLoop: true, condition: condition, count: count, body: body, line: lineNo, raw: trimmed})
			continue
		}

		if psl.HasSlot(trimmed) {
			// What it says depends on what psl answers, so it is read when the
			// replay gets to it rather than now.
			nodes = append(nodes, macroNode{raw: trimmed, slots: true, line: lineNo})
			continue
		}

		name, args, ok := readMacroStatement(trimmed)
		if !ok {
			applog.Logf("Macro: skipping line: %s", trimmed)
			continue
		}
		nodes = append(nodes, macroNode{raw: trimmed, action: name, args: args, line: lineNo})
	}

	if depth > 0 {
		applog.Log("Macro: block left open — the end of the macro closes it")
	}
	return nodes, i
}

// parseIfHeader reads `if (<condition>) {` and returns the condition, which is
// PSL text rather than a value: a :: … :: slot psl fills with true or false, or
// one of those written out. The second return says the line opens a block at all
// — a line that starts with the keyword opens one whether or not the rest of it
// is well formed, and an empty condition is what says it was not.
func parseIfHeader(line string) (string, bool) {
	rest, isIf := cutKeyword(line, macroIfKeyword)
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

// parseLoopHeader reads `loop (<count>) {` and `loop (<condition>, <count>) {`.
//
// It returns the condition — PSL text, the same an `if` takes, and empty when the
// loop was written without one — and the count, which is the most passes the
// loop may make. The last return says the line opens a block at all; a count of
// zero on a line that did is what says the header could not be read.
func parseLoopHeader(line string) (condition string, count int, isLoop bool) {
	rest, isLoop := cutKeyword(line, macroLoopKeyword)
	if !isLoop {
		return "", 0, false
	}
	expr, ok := strings.CutSuffix(strings.TrimSpace(rest), "{")
	if !ok {
		return "", 0, true
	}
	expr, ok = cutParens(strings.TrimSpace(expr))
	if !ok {
		return "", 0, true
	}

	times := expr
	if i := lastArgumentComma(expr); i >= 0 {
		condition, times = strings.TrimSpace(expr[:i]), strings.TrimSpace(expr[i+1:])
		// A condition is either asked or written out — the two an if takes, read
		// here by the same rule. Anything else is neither, and the block goes with
		// it rather than running unguarded.
		if condition == "" || (!psl.HasSlot(condition) && !isBoolLiteral(condition)) {
			return "", 0, true
		}
	}

	// The count is written out rather than asked. It is the bound on a loop that
	// could otherwise not end, and a bound the model picks fresh on every pass is
	// not a bound.
	n, err := strconv.Atoi(times)
	if err != nil || n < 1 {
		return "", 0, true
	}
	return condition, n, true
}

// lastArgumentComma returns the offset of the comma between a loop's condition
// and its count, or -1 when the header holds no such comma.
//
// It is the last one written outside a :: … :: slot. An instruction is a
// sentence for a model to read and may well have a comma in it —
// `loop (:: the list is still loading, not empty ::, 5)` — while the count is
// one number at the end, so the comma that separates them is the last one the
// header has of its own.
func lastArgumentComma(expr string) int {
	last, i := -1, 0
	for i <= len(expr) {
		slot, found := psl.FindSlot(expr, i)
		end := len(expr)
		if found {
			end = slot.Start
		}
		if j := strings.LastIndexByte(expr[i:end], ','); j >= 0 {
			last = i + j
		}
		if !found {
			break
		}
		i = slot.End
	}
	return last
}

// cutKeyword returns what follows a block keyword, and whether the line opened
// with it. The keyword ends at the space, the `(`, the `::` or the `{` after it,
// so `iframe(1, 2)` is the call it looks like rather than a block — while
// `if::x::` keeps being read as the block it was meant to be, and dropped as the
// malformed one it is.
func cutKeyword(line, keyword string) (string, bool) {
	if len(line) < len(keyword) || !strings.EqualFold(line[:len(keyword)], keyword) {
		return "", false
	}
	rest := line[len(keyword):]
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

// readMacroStatement reads one line as a call, whichever of the two shapes a
// statement is written in.
//
// It is where `stop` — the one statement written without parentheses — is read,
// so that a line of the macro and a line psl filled to the same text arrive at
// the same statement. Everything else goes to parseMacroLine unchanged.
func readMacroStatement(line string) (string, []string, bool) {
	if line == macroStopKeyword {
		return macroStopKeyword, nil, true
	}
	return parseMacroLine(line)
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

// macroNeedsPSL reports whether replaying these statements would run psl: a
// slot in one of them, or in a file one of their call()s brings in. seen holds
// the files already accounted for, the macro itself included, so a ring of calls
// is walked once.
//
// The called files are read here rather than found out about at the call,
// because the point of the check is saying so before the cursor moves: a macro
// whose second file cannot be filled is one to refuse at the start, not thirty
// statements in. A call whose path is itself a slot cannot be followed and does
// not need to be — that slot is one of its own, and hasMacroSlot has it already.
func macroNeedsPSL(nodes []macroNode, dir string, seen map[string]bool) bool {
	if hasMacroSlot(nodes) {
		return true
	}
	return calledFilesNeedPSL(nodes, dir, seen)
}

func calledFilesNeedPSL(nodes []macroNode, dir string, seen map[string]bool) bool {
	for _, node := range nodes {
		if calledFilesNeedPSL(node.body, dir, seen) {
			return true
		}
		if node.action != macroCallKeyword || len(node.args) == 0 {
			continue
		}
		path, err := resolveCallPath(dir, node.args[0])
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true

		// Read rather than parsed: parsing logs every line it cannot make sense
		// of, and the file is parsed again when the call is reached. A slot the
		// replay would never arrive at counts here, which is the same way round
		// hasMacroSlot has it for a block that never runs — the check errs towards
		// asking for psl, since the cost of that is a message and the cost of the
		// other way is a macro that stops halfway.
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// The comments go first, so a statement someone commented out rather than
		// deleted is not what says psl is needed.
		text := neutralizeComments(string(data))
		if psl.HasSlot(text) {
			return true
		}
		if calledFilesNeedPSL(parseCallLines(text), filepath.Dir(path), seen) {
			return true
		}
	}
	return false
}

// parseCallLines reads the call() statements out of a file, and nothing else. It
// is what lets the check walk from one file to the next without parsing — and so
// without logging — a file the replay may never reach.
func parseCallLines(text string) []macroNode {
	var nodes []macroNode
	for _, line := range codeLines(text) {
		name, args, ok := parseMacroLine(strings.TrimSpace(line))
		if ok && name == macroCallKeyword {
			nodes = append(nodes, macroNode{action: name, args: args})
		}
	}
	return nodes
}

// countMacroNodes counts every statement, a block header and the statements
// inside it alike — what the log line reports as the size of the macro. A loop
// counts once however many passes it goes on to make: this is the size of the
// macro, not the length of the run.
func countMacroNodes(nodes []macroNode) int {
	n := len(nodes)
	for _, node := range nodes {
		n += countMacroNodes(node.body)
	}
	return n
}

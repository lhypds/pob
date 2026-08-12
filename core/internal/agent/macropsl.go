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
// only runs when its condition holds — and whose else block runs when it does
// not — a loop block whose body runs again and again, or a once block that
// watches the screen and runs its body each time the screen changes into
// something its condition holds of.
type macroNode struct {
	// raw is the statement as written, with any :: … :: slot still in it. It is
	// the line psl fills, and what is parsed once it has been.
	raw string

	action string   // action call name; empty on a block or an unfilled statement
	args   []string // action arguments

	// quoted says the arguments were written as one double-quoted string, which
	// the values themselves no longer show once the quotes are off. It is what
	// tells a call that takes a time it was handed a string instead.
	quoted bool

	// slots says the statement holds at least one :: … :: and so cannot be read
	// until the replay reaches it — action and args are empty until then.
	slots bool

	// isStatementSlot says the line is a slot and nothing else, which is what
	// makes it stand for the statements it fills to rather than for a value
	// inside one. See runStatementSlot.
	isStatementSlot bool

	isIf   bool // if only
	isLoop bool // loop only
	isOnce bool // once only

	// isElseIf says the if was written on the else of the one above it —
	// `} else if (…) {`. It is that same if either way, and what this is for is
	// the log, which names a block the way it is written.
	isElseIf bool

	// condition is the parenthesised expression, slots unfilled: an if and a once
	// always have one, a loop only when it was written with one.
	condition string
	count     int         // loop only — the most passes it may make
	body      []macroNode // if, loop and once — the statements the block holds

	// elseBody is what an if runs when its condition does not hold — the
	// statements under its else — and is empty in an if written without one. A
	// chain is one if written into the else of the if above it, so this holds
	// that single if, which holds its own else in turn.
	elseBody []macroNode

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

// newGeneratedRun starts a replay over the block a statement slot filled to,
// under the run whose line asked for it. See runStatementSlot.
//
// It is a call() with no file behind it, and the two differences are that. path
// stays empty, since there is nothing on disk for a later call() to reach and
// nothing a path could be compared against — and resolveCallPath hands back an
// absolute path or an error, so an empty one is nothing it can collide with. dir
// is the asking file's, so a call() written inside a generated block names files
// from where the macro is, which is the only place the block could have meant.
func newGeneratedRun(parent *macroRun, line int, source string) *macroRun {
	source = neutralizeComments(source)
	return &macroRun{
		sessionID: parent.sessionID,
		source:    source,
		written:   strings.Split(source, "\n"),
		prompt:    parent.prompt,
		name:      generatedName(parent.name, line),
		dir:       parent.dir,
		parent:    parent,
	}
}

// generatedName is what a generated block is called: the file that asked and the
// line it asked on, which is where to go back to. psl is handed a file and names
// it in what it says, and the log puts it in front of the block's own line
// numbers — so it is written as a filename, and one every platform will take.
func generatedName(name string, line int) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "macro"
	}
	return fmt.Sprintf("%s-line%d.psl", base, line)
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
		run.spendBlock(node.elseBody)
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
		run.restoreBlock(node.elseBody)
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
			walk(node.elseBody)
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

	// The grammar first, since it is about the file rather than about the
	// machine: a macro Pob cannot read is one to fix before asking whether the
	// thing that fills its slots is installed. It is also before the parse,
	// which logs what it could not read as it goes — a macro that gets past here
	// has nothing for it to say.
	if problems := checkMacroSource(source, path); len(problems) > 0 {
		r.store.LogInstancef("MACRO NOT STARTED", "reason=%q problems=%d", "macro check failed", len(problems))
		applog.Errorf("Macro not run: %d problem(s) in %s", len(problems), filepath.Base(path))
		for _, p := range problems {
			applog.Logf("Macro %s", p)
		}
		lead := fmt.Sprintf("%s was not run — %d problems to fix first:", filepath.Base(path), len(problems))
		if len(problems) == 1 {
			lead = fmt.Sprintf("%s was not run — one problem to fix first:", filepath.Base(path))
		}
		r.br.ShowAlert(macroProblemsTitle, macroProblemsMessage(problems, lead))
		return
	}

	nodes := parseMacro(source)
	hasSlots := macroNeedsPSL(nodes, path, map[string]bool{path: true})

	// Checked before anything moves: a macro whose slots cannot be filled is one
	// Pob cannot run as written, and finding that out halfway through would
	// leave the statements above the slot already played.
	if hasSlots && !r.psl.Available() {
		message := filepath.Base(path) + ", or a file it calls, has a :: … :: slot, and Pob fills those by running " +
			"the psl compiler. psl was not found — install it (see https://github.com/pob/psl), or set " +
			"\"psl\" in settings.json to the path of the executable, and run the macro again."
		r.store.LogInstancef("MACRO NOT STARTED", "reason=%q", "psl is not available")
		applog.Errorf("Macro not run: %s", message)
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
		r.store.LogInstancef("MACRO NOT STARTED", "reason=%q error=%q", "cursor reset failed", err)
		return
	}

	actionCount := countMacroNodes(nodes)
	applog.Logf("Executing macro (%d actions)", actionCount)

	// Initial capture establishes the screenshot→screen coordinate context on
	// the Swift side before any click lands.
	if _, err := r.br.CaptureScreenshot(true, nil); err != nil {
		r.store.LogInstancef("MACRO NOT STARTED", "reason=%q error=%q", "initial screenshot failed", err)
		applog.Log("Macro: failed to get screenshot context")
		return
	}

	macroStart := time.Now()
	sessionID := r.store.CreateSession()
	r.setCurrentSession(sessionID)
	r.store.SaveMacro(sessionID)
	r.store.LogInstancef("MACRO START", "session=%s file=%q actions=%d", sessionID, path, actionCount)
	applog.Logf("[%s] Macro session started", sessionID)

	run := newMacroRun(sessionID, path, source, prompt)
	run.spendUncovered(nodes)
	r.runMacroNodes(ctx, run, nodes)

	macroEnd := time.Now()
	r.store.SaveSessionStartEndTimes(sessionID, macroStart, macroEnd)
	applog.Logf("[%s] Macro session times saved", sessionID)
	applog.Log("Macro execution complete")
	status := "completed"
	switch {
	case ctx.Err() != nil:
		status = "cancelled"
	case run.halted():
		status = "completed by stop()"
	}
	r.store.LogInstancef("MACRO STOP", "session=%s status=%q duration=%s", sessionID, status, macroEnd.Sub(macroStart).Round(time.Millisecond))

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
			// One block runs and the other is spent, since nothing in a block that
			// does not run is ever asked about. Which of the two is spent first is
			// the file's order and not a choice: psl fills the first slot in the
			// file, so the block above the statements about to be replayed has to be
			// out of the way before they ask anything.
			//
			// A condition that was no verdict at all takes both blocks with it. An
			// else is what runs when the answer is false, and there is no answer.
			holds, read := r.evalMacroCondition(ctx, run, node)
			switch {
			case holds:
				r.runMacroNodes(ctx, run, node.body)
				run.spendBlock(node.elseBody)
			case read:
				run.spendBlock(node.body)
				r.runMacroNodes(ctx, run, node.elseBody)
			default:
				run.spendBlock(node.body)
				run.spendBlock(node.elseBody)
			}
			run.spend(node.line)
			continue
		}

		if node.isLoop {
			r.runMacroLoop(ctx, run, node)
			continue
		}

		if node.isOnce {
			r.runMacroOnce(ctx, run, node)
			continue
		}

		// A slot written on a line of its own is answered with the statements that
		// belong there rather than with a value, so what comes back is replayed
		// rather than read as the one call the line was.
		if node.isStatementSlot {
			statement := run.line(node.line)
			r.logMacroStep("> STEP START", run, node, "statement slot", statement, "")
			r.runStatementSlot(ctx, run, node)
			r.logMacroStep("STEP END", run, node, "statement slot", statement, macroStepStatus(ctx, run))
		} else if name, args, quoted, ok := r.resolveMacroAction(ctx, run, node); ok {
			statement := strings.TrimSpace(stripLine(run.line(node.line)))
			r.logMacroStep("> STEP START", run, node, name, statement, "")
			r.runMacroAction(ctx, run, name, args, quoted)
			r.logMacroStep("STEP END", run, node, name, statement, macroStepStatus(ctx, run))
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

// logMacroStep gives instance.log one uniform row around every statement that
// reaches execution. The existing app log still carries action-specific
// results (cursor positions, branch decisions and validation failures); these
// rows provide the file/line identity and exact resolved statement common to
// every kind of action.
func (r *Runner) logMacroStep(event string, run *macroRun, node macroNode, kind, statement, status string) {
	detail := fmt.Sprintf("line=%d depth=%d kind=%q statement=%q",
		node.line, run.depth(), kind, statement)
	if status != "" {
		detail += fmt.Sprintf(" status=%q", status)
	}
	r.store.LogInstance(event, detail)
}

func macroStepStatus(ctx context.Context, run *macroRun) string {
	switch {
	case ctx.Err() != nil:
		return "cancelled"
	case run.halted():
		return "stopped"
	default:
		return "completed"
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
	r.store.LogInstancef(">> LOOP START", "line=%d loop=%q count=%d",
		node.line, label, node.count)

	for passes < node.count {
		if ctx.Err() != nil || run.halted() {
			break
		}
		if passes > 0 {
			run.restore(node.line)
			run.restoreBlock(node.body)
		}
		// No condition is a condition that always holds, so `loop (3)` and
		// `loop (true, 3)` are one and the same loop. A loop has one block and
		// nothing to run instead of it, so a condition it could not read ends it
		// the same way one that read false does.
		if node.condition != "" {
			if holds, _ := r.evalMacroCondition(ctx, run, node); !holds {
				break
			}
		}
		passes++
		r.store.LogInstancef("LOOP PASS", "line=%d loop=%q pass=%d count=%d",
			node.line, label, passes, node.count)
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
	r.store.LogInstancef("LOOP STOP", "line=%d loop=%q passes=%d status=%q",
		node.line, label, passes, macroStepStatus(ctx, run))
}

// resolveMacroAction fills the statement's slots, if it has any, and reads the
// result as a call. A statement that cannot be read once its slots are filled
// is logged and skipped, the way a bad line always has been.
func (r *Runner) resolveMacroAction(ctx context.Context, run *macroRun, node macroNode) (string, []string, bool, bool) {
	if !node.slots {
		return node.action, node.args, node.quoted, true
	}

	filled, ok := r.fillStatement(ctx, run, node)
	if !ok {
		return "", nil, false, false
	}

	// The comment on the end of the statement came back with it, and a statement
	// is read the same way whether it was written out or filled in.
	name, args, quoted, ok := parseMacroLine(strings.TrimSpace(stripLine(filled)))
	if !ok {
		applog.Logf("[%s] Macro %s: filled to %q, which does not read as a statement — skipping", run.sessionID, run.where(node.line), filled)
		return "", nil, false, false
	}
	return name, args, quoted, true
}

// evalMacroCondition works out whether a block's condition holds — an if's, or
// the one a loop checks before each pass. The condition is ordinary PSL text: a
// :: … :: slot psl fills with true or false, or one of those two written out by
// hand.
//
// Anything that goes wrong — psl failing, an answer that is not true or false —
// is no verdict at all, and the second return is what says so. The block stays
// unexecuted, or the loop ends, rather than running on a guess.
//
// The two returns come apart only where an else is written. False is a verdict
// and picks the other block; a condition Pob could not read has picked nothing,
// so neither block runs — an else that ran on a psl that was not installed would
// be the guess the reading was written to refuse, taken the other way round.
func (r *Runner) evalMacroCondition(ctx context.Context, run *macroRun, node macroNode) (holds, read bool) {
	label := macroBlockLabel(node)
	statement := run.line(node.line)
	r.logMacroStep("> STEP START", run, node, "condition", statement, "")
	// What a condition means for the block it heads: the first when it does not
	// hold, and the second when there was nothing in it to read.
	no, unread := "skipping block", "skipping block"
	switch {
	case node.isLoop:
		no, unread = "the loop ends here", "the loop ends here"
	case node.isOnce:
		// Neither ends the block: a once is asked again at the next change, so a
		// no and a non-answer alike leave it watching where it was.
		no, unread = "back to watching the screen", "back to watching the screen"
	case len(node.elseBody) == 1 && node.elseBody[0].isElseIf:
		no, unread = "the else if is asked next", "skipping the block and the else if under it"
	case len(node.elseBody) > 0:
		no, unread = "the else block runs", "skipping both blocks"
	}

	expr := node.condition
	if psl.HasSlot(expr) {
		filled, ok := r.fillStatement(ctx, run, node)
		if !ok {
			r.logMacroStep("STEP END", run, node, "condition", statement, "unfilled")
			applog.Logf("[%s] Macro %s — slot unfilled, %s", run.sessionID, label, unread)
			return false, false
		}
		// The whole header line comes back, so the condition is read out of it
		// again rather than assumed to be what replaced the slot.
		expr = readCondition(node, strings.TrimSpace(filled))
	}

	holds, read = conditionHolds(expr)
	conditionStatus := "unreadable"
	if read && holds {
		conditionStatus = "true"
	} else if read {
		conditionStatus = "false"
	}
	r.logMacroStep("STEP END", run, node, "condition", statement, conditionStatus)
	switch {
	case !read:
		applog.Logf("[%s] Macro %s -> %q is not true or false — %s", run.sessionID, label, expr, unread)
	case holds:
		applog.Logf("[%s] Macro %s -> TRUE — running the block", run.sessionID, label)
	default:
		applog.Logf("[%s] Macro %s -> FALSE — %s", run.sessionID, label, no)
	}
	return holds, read
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
	if node.isOnce {
		condition, _ := parseOnceHeader(header)
		return condition
	}
	// An if written on an else has the } and the keyword of the block before it
	// in front of the one it opens, and the whole line is what came back from psl.
	if node.isElseIf {
		rest, _, isElse := cutElse(header)
		if !isElse {
			return ""
		}
		header = strings.TrimSpace(rest)
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
	case node.isOnce:
		return fmt.Sprintf("once (%s)", node.condition)
	case node.isElseIf:
		return fmt.Sprintf("else if (%s)", node.condition)
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
	var notes []string
	for psl.HasSlot(statement) {
		if ctx.Err() != nil {
			return "", false
		}
		filled, note, ok := r.fillOneSlot(ctx, run, node, statement)
		if !ok {
			return "", false
		}
		if filled == statement {
			applog.Logf("[%s] Macro %s: psl returned the statement unchanged — giving up on it", run.sessionID, run.where(node.line))
			return "", false
		}
		notes = append(notes, note)
		statement = filled
	}
	// One bracket per slot: a statement with two of them was two model calls, and
	// which model answered and how long it took is a thing about each of them.
	applog.Logf("[%s] Macro %s: %s -> %s%s", run.sessionID, run.where(node.line), node.raw,
		strings.TrimSpace(statement), fillNotes(notes))
	return statement, true
}

// fillNotes puts the model and duration of each fill in brackets after the
// statement they made, and writes nothing at all where there was no fill to
// report — a statement whose slots were already filled costs no model call.
func fillNotes(notes []string) string {
	var kept []string
	for _, note := range notes {
		if note != "" {
			kept = append(kept, note)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "  [" + strings.Join(kept, "; ") + "]"
}

// fillOneSlot runs psl once over the macro and returns the statement with its
// first slot filled, and which model answered and how long it took — what the
// row naming the filled statement puts in brackets after it.
func (r *Runner) fillOneSlot(ctx context.Context, run *macroRun, node macroNode, statement string) (string, string, bool) {
	seq := run.nextSlot()

	slot, found := psl.FindSlot(statement, 0)
	if !found {
		return statement, "", true
	}

	// A `.macro` says in its name that it is replayed without the compiler, so a
	// slot in one is never filled — whatever it asks for. The check names this
	// before the run starts and is the message someone acts on; this is the same
	// rule where the fill would have happened, so that a file reached by a route
	// the check did not walk still cannot start psl behind the name's back.
	if psl.Deterministic(run.name) {
		applog.Logf("[%s] Macro %s: %s holds a :: %s :: and %s files are replayed without psl — skipping the statement",
			run.sessionID, run.where(node.line), run.name, slot.Instruction, psl.MacroExt)
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false,
			"a "+psl.MacroExt+" file is replayed without psl, so its slots are never filled", nil)
		return "", "", false
	}

	// psl fills the first slot in the file, and the file is handed over whole,
	// so the first slot in it has to be this statement's. Checked here rather
	// than found out afterwards: a statement that comes back untouched says only
	// that something else was answered, not what.
	source := run.source
	targetLine := node.line - 1
	if live, found := liveSlotLine(source); !found || live != targetLine {
		applog.Logf("[%s] Macro slot :: %s :: — psl would fill a slot on line %d, not this statement on %s; not running it",
			run.sessionID, slot.Instruction, live+1, run.where(node.line))
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false,
			"the first slot in the file is not this statement's", nil)
		return "", "", false
	}

	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		applog.Logf("[%s] Macro slot :: %s :: — no screenshot", run.sessionID, slot.Instruction)
		return "", "", false
	}

	// What the model is shown, which is the screenshot itself unless image_scale
	// asks for less of it. The shrunken one is what goes in the log too: the
	// picture worth keeping beside an answer is the one it was read off.
	scale := r.cfg.ImageScale()
	if smaller, w, h, err := shrinkPNG(shot, scale); err != nil {
		applog.Logf("[%s] Macro slot :: %s :: — could not scale the screenshot (%v), sending it whole", run.sessionID, slot.Instruction, err)
		scale = 1
	} else if w > 0 {
		applog.Logf("[%s] Macro slot :: %s :: — %s", run.sessionID, slot.Instruction, scaleNote(w, h, scale))
		shot = smaller
	} else {
		scale = 1
	}

	// The file and image the model sees have to describe one pixel grid. Keep
	// the real source in screen pixels, but scale every existing coordinate in
	// the temporary copy handed to psl. The current statement and slot in that
	// copy are retained so the answer can be cut back out of it afterwards.
	modelSource := source
	modelStatement := statement
	modelSlot := slot
	if scale < 1 {
		modelSource = scaleMacroCoordinates(source, scale)
		modelLines := strings.Split(modelSource, "\n")
		if targetLine < 0 || targetLine >= len(modelLines) {
			applog.Logf("[%s] Macro slot :: %s :: — could not find the statement in the scaled model copy", run.sessionID, slot.Instruction)
			r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false,
				"the statement is missing from the scaled model copy", shot)
			return "", "", false
		}
		modelStatement = modelLines[targetLine]
		var modelSlotFound bool
		modelSlot, modelSlotFound = psl.FindSlot(modelStatement, 0)
		if !modelSlotFound {
			applog.Logf("[%s] Macro slot :: %s :: — could not find the slot in the scaled model copy", run.sessionID, slot.Instruction)
			r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false,
				"the slot is missing from the scaled model copy", shot)
			return "", "", false
		}
	}

	applog.Logf("[%s] Macro slot :: %s :: — running psl...", run.sessionID, slot.Instruction)

	result, err := r.psl.Fill(ctx, psl.Request{Source: modelSource, Name: run.name, Image: shot, Prompt: run.prompt})
	if err != nil {
		responseOutput := err.Error()
		if result != nil && result.Output != "" {
			responseOutput = result.Output
		}
		applog.Errorf("[%s] Macro slot :: %s :: — psl failed: %v", run.sessionID, slot.Instruction, err)
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", "", false, responseOutput, shot)
		return "", "", false
	}

	filled, ok := extractLine(modelSource, result.Source, targetLine)
	if !ok {
		applog.Logf("[%s] Macro slot :: %s :: — could not read the filled statement back", run.sessionID, slot.Instruction)
		r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", result.Model, false, result.Output, shot)
		return "", "", false
	}
	if scale < 1 {
		var restored bool
		filled, restored = restoreFilledSurroundings(statement, slot.Start, slot.End,
			modelStatement, modelSlot.Start, modelSlot.End, filled)
		if !restored {
			applog.Logf("[%s] Macro slot :: %s :: — psl rewrote text outside the slot in the scaled model copy; not using its coordinates", run.sessionID, slot.Instruction)
			r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, "", result.Model, false, result.Output, shot)
			return "", "", false
		}
	}

	// What the model wrote, before any of it is grown: the row below says it as
	// the model said it, and the statement it became after.
	answer, hasAnswer := filledAnswer(statement, slot.Start, slot.End, filled)
	if !hasAnswer {
		answer = filled
	}

	// The answer was read off a smaller picture, so the distances in it are that
	// picture's. Grown back here rather than at the click, so that the macro, the
	// log and the slot all say the same screen pixels a macro written by hand
	// would.
	grown, rescaled := rescaleFilled(statement, slot.Start, slot.End, filled, scale)
	filled = closeUnterminatedStrings(grown)
	if rescaled {
		applog.Logf("[%s] Macro slot :: %s :: -> %s, scaled back to %s", run.sessionID, slot.Instruction,
			oneLine(answer), oneLine(filled))
	}
	// The statement itself is not logged again where it did not have to be grown:
	// the `Macro <where>: <written> -> <filled>` row under this one already says
	// what the line became, and carries the model and how long it took.
	r.store.SaveMacroSlot(run.sessionID, seq, run.name, node.line, statement, slot.Instruction, filled, result.Model, true, result.Output, shot)

	// The answer goes into the macro, which is what the next run is handed: with
	// this slot no longer in the file, the first one left is the next statement's
	// — or the second slot of this one, if it has two.
	run.record(node.line, filled)
	return filled, fmt.Sprintf("%s, %s", result.Model, result.Duration.Round(time.Millisecond)), true
}

// Nothing of Pob's is written permanently into the macro: no preamble and no
// existing statement rewritten. When the screenshot is shrunk, the temporary
// file handed to psl has its existing image coordinates shrunk by the same
// amount; after the answer comes back, its surrounding text is restored from
// the real file. What travels beside that copy is the screenshot as the slot's
// image, and macroPrompt as psl's --prompt — what the calls in the file are and
// what their arguments mean, which is what psl takes a briefing on an API for.

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

func (r *Runner) runMacroAction(ctx context.Context, run *macroRun, name string, args []string, quoted bool) {
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
		} else {
			applog.Logf("[%s] Macro move(%d, %d) failed: %v", sessionID, int(dx), int(dy), err)
		}

	case "moveTo":
		x, okX := num(0)
		y, okY := num(1)
		if !okX || !okY {
			return
		}
		// The position, not the distance to it. What comes back is where the
		// cursor ended up, which is the position asked for unless the window
		// edge stopped it short — so it is logged rather than assumed.
		if pos, err := r.br.MoveCursorTo(x, y); err == nil {
			applog.Logf("[%s] Macro moveTo(%d, %d) -> (%d, %d)", sessionID, int(x), int(y), pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro moveTo(%d, %d) failed: %v", sessionID, int(x), int(y), err)
		}

	case "resetCursor":
		// Recorded when something sent the cursor home mid-sequence. Replaying
		// it matters because every move around it is a relative offset: skip the
		// jump back to the origin and each following move starts from the wrong
		// place.
		if pos, err := r.br.ResetCursor(); err == nil {
			applog.Logf("[%s] Macro resetCursor -> (%d, %d)", sessionID, pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro resetCursor failed: %v", sessionID, err)
		}

	case "click", "rightClick", "doubleClick":
		press := r.br.Click
		switch name {
		case "rightClick":
			press = r.br.RightClick
		case "doubleClick":
			press = r.br.DoubleClick
		}
		// Half a target is neither of the two statements, and is the one worth
		// saying so about rather than skipping quietly: read as the bare click
		// it is one argument short of, it would put a real click somewhere
		// nobody aimed it. The check catches a statement written that way, and
		// this catches the one a slot filled to it.
		if len(args) != 0 && len(args) != 2 {
			applog.Logf("[%s] Macro %s takes %s, and %s — skipping", sessionID, name,
				macroVocabulary[name].wants(), argsWritten(len(args)))
			return
		}
		// Written with a target, the click aims first: the cursor goes to that
		// absolute position and the button goes down where it landed, so the
		// two halves of a click on a thing are one statement rather than a move
		// whose next line has to be trusted to still be pointing at it. Written
		// without one it is the click it always was — wherever the cursor is.
		if len(args) == 2 {
			x, okX := num(0)
			y, okY := num(1)
			if !okX || !okY {
				return
			}
			if _, err := r.br.MoveCursorTo(x, y); err != nil {
				applog.Logf("[%s] Macro %s(%d, %d) could not move to its target: %v", sessionID, name, int(x), int(y), err)
				return
			}
			if pos, err := press(); err == nil {
				applog.Logf("[%s] Macro %s(%d, %d) at (%d, %d)", sessionID, name, int(x), int(y), pos.X, pos.Y)
			} else {
				applog.Logf("[%s] Macro %s(%d, %d) failed: %v", sessionID, name, int(x), int(y), err)
			}
			return
		}
		if pos, err := press(); err == nil {
			applog.Logf("[%s] Macro %s at (%d, %d)", sessionID, name, pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro %s failed: %v", sessionID, name, err)
		}

	case "drag":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Drag(dx, dy); err == nil {
			applog.Logf("[%s] Macro drag(%d, %d) -> (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro drag(%d, %d) failed: %v", sessionID, int(dx), int(dy), err)
		}

	case "dragTo":
		x, okX := num(0)
		y, okY := num(1)
		if !okX || !okY {
			return
		}
		// Where the drop goes, not how far it is from the grab. The button goes
		// down where the cursor already is, so a dragTo is still written under
		// whatever put the cursor on the thing being dragged.
		if pos, err := r.br.DragTo(x, y); err == nil {
			applog.Logf("[%s] Macro dragTo(%d, %d) -> (%d, %d)", sessionID, int(x), int(y), pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro dragTo(%d, %d) failed: %v", sessionID, int(x), int(y), err)
		}

	case "scroll":
		dx, okX := num(0)
		dy, okY := num(1)
		if !okX || !okY {
			return
		}
		if pos, err := r.br.Scroll(int(dx), int(dy)); err == nil {
			applog.Logf("[%s] Macro scroll(%d, %d) at (%d, %d)", sessionID, int(dx), int(dy), pos.X, pos.Y)
		} else {
			applog.Logf("[%s] Macro scroll(%d, %d) failed: %v", sessionID, int(dx), int(dy), err)
		}

	case "typeText":
		if len(args) == 0 {
			return
		}
		text := args[0]
		applog.Logf("[%s] Macro typeText(%q)", sessionID, truncate(text, 80))
		if err := r.br.TypeText(text); err != nil {
			applog.Logf("[%s] Macro typeText failed: %v", sessionID, err)
		}

	case "keyPress":
		if len(args) == 0 {
			return
		}
		applog.Logf("[%s] Macro keyPress(%q)", sessionID, args[0])
		if err := r.br.KeyPress(args[0]); err != nil {
			applog.Logf("[%s] Macro keyPress(%q) failed: %v", sessionID, args[0], err)
		}

	case "sleep":
		if len(args) == 0 {
			return
		}
		if quoted {
			applog.Logf("[%s] Macro sleep was written with %q — a time is not a string, so it goes in without the quotes: %s — skipping", sessionID, truncate(args[0], 40), truncate(args[0], 40))
			return
		}
		d, ok := macroTime(args[0])
		if !ok {
			applog.Logf("[%s] Macro sleep was written with %q, which is not %s — skipping", sessionID, truncate(args[0], 40), macroTimeWants)
			return
		}
		applog.Logf("[%s] Macro sleep(%s)", sessionID, d)
		sleepCtx(ctx, d)

	case macroStopKeyword:
		// Nothing under this runs — not the statements after it, not the rest of
		// the block it is in, and not the file that called the file it is in. What
		// it leaves behind is a finished run rather than an abandoned one, so the
		// session is written out and stop_hook fires the way it does at the end.
		applog.Logf("[%s] Macro stop() — the run ends here", sessionID)
		run.halt()

	case macroCallKeyword:
		if len(args) == 0 {
			applog.Logf("[%s] Macro call wants the path of a PSL file — call(\"other.psl\")", sessionID)
			return
		}
		r.runMacroCall(ctx, run, args[0])

	case "takeScreenshot":
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
			applog.Logf("[%s] Macro takeScreenshot(crop: %d, %d, %d, %d)", sessionID, int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
		} else {
			applog.Logf("[%s] Macro takeScreenshot", sessionID)
		}
		r.br.FlashScreenshot()
		if shot, err := r.br.CaptureScreenshot(true, crop); err == nil {
			r.store.SaveScreenshot(shot, sessionID)
		} else {
			applog.Logf("[%s] Macro takeScreenshot failed: %v", sessionID, err)
		}

	default:
		applog.Logf("[%s] Macro: unknown action: %s", sessionID, name)
	}
}

// maxCallDepth is how many files deep a replay may go — the ones a call() named
// and the blocks a statement slot generated, counted together, since both are a
// file replayed inside another. A macro split across a few files is a macro
// someone wrote in pieces; eight down is past the depth anyone meant, and the
// bound is what turns a mistake nothing else catches into a line in the log.
const maxCallDepth = 8

// runStatementSlot fills a slot written on a line of its own and replays what
// comes back, where the line stands.
//
// A slot inside a statement is answered with a value, and the statement around
// it is what says which kind. A slot that is the whole line has no statement
// around it to say anything, so what it stands for is the statements themselves
// — one of them or several, blocks and all — and that is a piece of PSL rather
// than a value.
//
// It is replayed as a run of its own, the way call() replays the file it names,
// and for the reason the line numbers are: psl fills the first slot in the file
// it is handed, and Pob knows which slot that is by replaying the file top to
// bottom and putting every answer back on the line it came from. Statements
// written into the file where this one line was would move every statement under
// them off the number the parse found it at, and the replay, the log and the
// loops all read a macro by those. So the block is a file of its own with line
// numbers of its own, and the line that asked keeps the one line it always had.
//
// What that one line holds while the replay is running is the block folded onto
// it, which is what any answer of several lines does — see record. The block
// whole is written down under logs/<session>/slots/<n>/, where the fill that
// produced it is, and where a pass of a loop leaves its own.
func (r *Runner) runStatementSlot(ctx context.Context, run *macroRun, node macroNode) {
	// Asked before the fill rather than after it: a block this replay would not
	// run is not a block worth spending a model call on.
	if run.depth() >= maxCallDepth {
		applog.Logf("[%s] Macro %s — %d files deep, which is as far as a generated block goes; not filling it",
			run.sessionID, run.where(node.line), maxCallDepth)
		return
	}

	filled, note, ok := r.fillOneSlot(ctx, run, node, run.line(node.line))
	if !ok {
		return
	}

	// The line is done being asked about from here: what it filled to is a file
	// of its own, and a slot the model wrote into the block belongs to that file
	// rather than to this one. Left live here it would be the first slot in this
	// file, and so the one filled in place of the statement below.
	run.spend(node.line)

	block := strings.TrimSpace(filled)
	nodes := parseMacro(block)
	if len(nodes) == 0 {
		applog.Logf("[%s] Macro %s: filled to %q, which holds no statement — skipping",
			run.sessionID, run.where(node.line), truncate(oneLine(block), 60))
		return
	}

	generated := newGeneratedRun(run, node.line, block)
	generated.spendUncovered(nodes)

	applog.Logf("[%s] Macro %s -> %s (%d actions)%s", run.sessionID, run.where(node.line),
		generated.name, countMacroNodes(nodes), fillNotes([]string{note}))
	r.runMacroNodes(ctx, generated, nodes)
	if !generated.halted() {
		applog.Logf("[%s] Macro %s — %s done", run.sessionID, run.where(node.line), generated.name)
	}
}

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
// in, not to wherever Pob happens to be running: `call("shared.macro.psl")` in
// ~/.pob/<instance>/src/main.macro.psl is ~/.pob/<instance>/src/shared.macro.psl,
// and means that whoever started the replay. A path beginning with ~/ is under the home directory, the
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
// line holding nothing but `}` — or by the `}` an else is written on, which
// closes it and opens the block to run instead. Matched whatever its case, so
// `IF` opens a block too: the alternative to recognising it is running the body
// unguarded, which is the one thing a condition was written to prevent.
const macroIfKeyword = "if"

// macroElseKeyword opens the block an if runs when its condition does not hold:
// `} else {`, or `} else if (<expression>) {` to go on asking. Matched whatever
// its case, like the two keywords it belongs to and for the same reason — an
// else Pob failed to recognise would be a block that runs whatever the condition
// above it said.
const macroElseKeyword = "else"

// macroLoopKeyword opens a block that runs again and again: `loop (<count>) {`,
// or `loop (<condition>, <count>) {`, closed the same way. Matched whatever its
// case for the same reason as `if` — a header that went unrecognised would leave
// the body of the block to run once, unbounded and unguarded.
const macroLoopKeyword = "loop"

// macroOnceKeyword opens a block that watches the screen and runs when it has
// changed into something the condition holds of: `once (<condition>) {`, closed
// the same way as the other two. It is written at the top level of a file and
// never inside another block, and nothing under it is ever reached — see
// runMacroOnce. Matched whatever its case, for the reason `if` and `loop` are.
const macroOnceKeyword = "once"

// macroStopKeyword ends the replay where it stands. It is written `stop()`, with
// the parentheses every other call has: they hold nothing, and what they buy is
// that a statement is one shape throughout the language rather than one shape and
// an exception. Spelled lowercase like the rest of the vocabulary, and read that
// way — a name is a name, and the block keywords are the only two that take any
// case.
const macroStopKeyword = "stop"

// macroCallKeyword replays another PSL file where it stands:
// `call("../shared.psl")`. See runMacroCall.
const macroCallKeyword = "call"

// macroTimeWants is what a time is, said once. The check puts it in front of the
// user before the run and the replay logs it at the statement, and the two are
// the same sentence because they are the same rule.
const macroTimeWants = "a time — a number with its unit on the end: 250ms, 3s, 10m, 5h, 10h5m"

// macroTime reads a time, which is PSL's third kind of value: a number carrying
// its own unit, written where the language wants a length rather than a count.
// `3s` is three seconds, `10m` ten minutes, `5h` five hours, and units written
// one after another add up — `10h5m` is ten hours and five minutes. The number
// in front of a unit may be fractional, so `0.5s` and `500ms` are the same time
// said two ways.
//
// The unit is the whole point of the type and is not optional: `sleep(500)` is a
// count of nothing in particular, and a macro that means half a second says
// `500ms` rather than leaving the reader — and the language — to guess. A macro
// written before times existed said `sleep(500)` and meant milliseconds, which is
// exactly the line this refuses so that the check can name it instead of the
// replay quietly waiting eight minutes.
//
// A quoted one is a string and not a time, and is refused as one: `sleep("10m")`
// is a statement the check stops the run over, and a slot filled with `"10m"` is
// a statement the replay logs and skips. The quotes come off in the parse, so
// what carries that as far as here is parseMacroLine's `quoted` — this reads the
// value, and whether the value was a string is not a question the value can
// answer.
//
// Nothing negative: a run cannot wait less than no time.
func macroTime(arg string) (time.Duration, bool) {
	arg = strings.TrimSpace(arg)
	// A number is a number, whichever number it is. Go reads `0` as a duration of
	// none — the one unit it lets go unwritten — and the language does not, since
	// a type with an exception in it is two types to learn.
	if _, err := strconv.ParseFloat(arg, 64); err == nil {
		return 0, false
	}
	d, err := time.ParseDuration(arg)
	if err != nil || d < 0 {
		return 0, false
	}
	return d, true
}

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
	nodes, probs := parseMacroProblems(text)
	for _, p := range probs {
		applog.Logf("Macro %s", p)
	}
	return nodes
}

// parseMacroProblems reads a file and hands back both what it says and
// everything the reading found wrong with it.
//
// The two come out of the one pass on purpose. What the check refuses to run has
// to be what the replay would have skipped, and a second reading written beside
// this one would drift from it the first time either changed — see macrocheck.go.
func parseMacroProblems(text string) ([]macroNode, []macroProblem) {
	var probs []macroProblem
	nodes, _, _ := parseMacroBlock(codeLines(text), 0, 0, 0, &probs)
	return nodes, probs
}

// blockEnd says what ended a block of statements: the `}` written on a line of
// its own to close it, an else that closes it and opens the block to run
// instead, or the end of the file, which closes whatever was still open.
type blockEnd int

const (
	blockEOF blockEnd = iota
	blockClosed
	blockElse
)

// parseMacroBlock reads statements from lines[i:] until the `}` that closes the
// block, or the end of the file at depth 0. It returns the statements, the index
// just past the block, and what ended it — since a block an else closes is one
// the if above it is not finished with.
//
// opened is the line the block's header is on, which is what an unclosed block
// is reported against — the `{` is the half of it that was written, and the line
// to go back to.
func parseMacroBlock(lines []string, i, depth, opened int, probs *[]macroProblem) ([]macroNode, int, blockEnd) {
	var nodes []macroNode
	problem := func(line int, format string, a ...any) {
		*probs = append(*probs, problemf(line, format, a...))
	}

	// The once block this file has, if it has one, and whether the line under it
	// has been reported yet. A once watches until the run is stopped, so
	// everything written after it is a statement nothing will ever reach — said
	// once, at the first of them, since a whole tail of unreachable statements is
	// one mistake and not twenty.
	onceLine, saidUnreachable := 0, false

	for i < len(lines) {
		lineNo := i + 1
		trimmed := strings.TrimSpace(lines[i])
		i++

		if trimmed == "" {
			continue
		}

		if trimmed == "}" {
			if depth == 0 {
				problem(lineNo, "} closes a block that was never opened")
				continue
			}
			return nodes, i, blockClosed
		}

		if onceLine > 0 && !saidUnreachable {
			problem(lineNo, "nothing here runs — the once block opened on line %d watches the screen until the run is stopped, so the statements under it are never reached", onceLine)
			saidUnreachable = true
		}

		if _, braced, isElse := cutElse(trimmed); isElse {
			// The `}` in front of the keyword closes this block, and what follows it
			// is the if's to read — see parseIfBlocks. An else written under the `}`
			// instead has been taken by the if already, so one reaching here belongs
			// to nothing: there is no block open above it, or none the `}` it needs
			// has closed.
			if braced && depth > 0 {
				return nodes, i, blockElse
			}
			problem(lineNo, "else belongs to the if whose block the } above it closes — } else {, or the else on a line of its own under the } — so its whole block is dropped")
			var dropped []macroNode
			dropped, i, _ = parseMacroBlock(lines, i, depth+1, lineNo, probs)
			*probs = append(*probs, checkStatements(dropped)...)
			continue
		}

		if condition, isIf := parseIfHeader(trimmed); isIf {
			// The blocks are read either way, so that what a broken if was written
			// to guard is dropped with it rather than left to run unguarded.
			var node macroNode
			node, i = parseIfBlocks(macroNode{isIf: true, condition: condition, line: lineNo, raw: trimmed}, lines, i, depth, probs)
			if condition == "" {
				problem(lineNo, "if wants a condition in parentheses and a { at the end of the line — if (:: … ::) { — so its whole block is dropped")
				// The statements inside it are dropped with it and so are never
				// run, but they are still statements someone wrote and will still
				// be there once the header is fixed. Everything wrong with a macro
				// goes up at once or the fix takes as many rounds as it has
				// mistakes.
				*probs = append(*probs, checkStatements(node.body)...)
				*probs = append(*probs, checkStatements(node.elseBody)...)
				continue
			}
			nodes = append(nodes, node)
			continue
		}

		if condition, count, isLoop := parseLoopHeader(trimmed); isLoop {
			// Read either way, like the if: a header Pob cannot read takes its
			// block with it, rather than leaving the body to run once when what was
			// written asks for it to run until something is true.
			var body []macroNode
			var end blockEnd
			body, i, end = parseMacroBlock(lines, i, depth+1, lineNo, probs)
			// A loop has no condition left to be the other half of once its passes
			// are over, so the block under the else is dropped and the loop stands.
			if end == blockElse {
				elseLine := i
				problem(elseLine, "else belongs to an if, and the block this one closes is a loop — so its whole block is dropped")
				var dropped []macroNode
				dropped, i, _ = parseMacroBlock(lines, i, depth+1, elseLine, probs)
				*probs = append(*probs, checkStatements(dropped)...)
			}
			if count < 1 {
				problem(lineNo, "loop wants a whole count of 1 or more in parentheses and a { at the end of the line — loop (3) { or loop (:: … ::, 3) { — so its whole block is dropped")
				// Checked though it is dropped, for the same reason an if's body is.
				*probs = append(*probs, checkStatements(body)...)
				continue
			}
			nodes = append(nodes, macroNode{isLoop: true, condition: condition, count: count, body: body, line: lineNo, raw: trimmed})
			continue
		}

		if condition, isOnce := parseOnceHeader(trimmed); isOnce {
			// Read either way, like the if and the loop: a header Pob cannot read
			// takes its block with it, rather than leaving the body to run once when
			// what was written asks for it to run every time the screen changes.
			var body []macroNode
			var end blockEnd
			body, i, end = parseMacroBlock(lines, i, depth+1, lineNo, probs)
			// A once has no other half either: the condition it asks is asked again
			// at the next change rather than answered once, so there is no moment an
			// else would be the thing that runs instead.
			if end == blockElse {
				elseLine := i
				problem(elseLine, "else belongs to an if, and the block this one closes is a once — so its whole block is dropped")
				var dropped []macroNode
				dropped, i, _ = parseMacroBlock(lines, i, depth+1, elseLine, probs)
				*probs = append(*probs, checkStatements(dropped)...)
			}
			// Where it is written is half of what it means. A once never hands the
			// run back, so one inside an if would be a block that runs to the end of
			// the run in place of the statements after it, and one inside a loop or
			// another once would be a second pass that never comes — neither of which
			// is what the file looks like it says. At the top level it is what it
			// reads as: the file runs down to it and then watches.
			if depth > 0 {
				problem(lineNo, "once watches the screen until the run is stopped and is written at the top level of a file, not inside another block — so its whole block is dropped")
				// Checked though it is dropped, for the same reason an if's body is.
				*probs = append(*probs, checkStatements(body)...)
				continue
			}
			if condition == "" {
				problem(lineNo, "once wants a condition in parentheses and a { at the end of the line — once (:: … ::) { — so its whole block is dropped")
				*probs = append(*probs, checkStatements(body)...)
				continue
			}
			nodes = append(nodes, macroNode{isOnce: true, condition: condition, body: body, line: lineNo, raw: trimmed})
			onceLine = lineNo
			continue
		}

		if psl.HasSlot(trimmed) {
			// What it says depends on what psl answers, so it is read when the
			// replay gets to it rather than now. Whether it is a statement or a
			// value that comes back is settled here, by what is written around the
			// slot — which is the one thing about it that is written down.
			nodes = append(nodes, macroNode{raw: trimmed, slots: true, isStatementSlot: statementSlot(trimmed), line: lineNo})
			continue
		}

		name, args, quoted, ok := parseMacroLine(trimmed)
		if !ok {
			// Why it is not one is checkStatement's to say: it reads the line
			// against the vocabulary, which is more than the parse knows about.
			if p, bad := checkStatement(trimmed, lineNo); bad {
				*probs = append(*probs, p)
			}
			continue
		}
		nodes = append(nodes, macroNode{raw: trimmed, action: name, args: args, quoted: quoted, line: lineNo})
	}

	if depth > 0 {
		problem(opened, "the block opened here is never closed by a } of its own — the end of the file closes it")
	}
	return nodes, i, blockEOF
}

// parseIfBlocks reads the blocks an if is written with — the one its condition
// guards, and the one under the else that runs when the condition does not hold
// — and hands back the statement and the index just past the } that closes the
// last of them.
//
// A chain is an if written into the else of the if above it, and is kept that
// way: `} else if (…) {` becomes an else block holding one if, which holds its
// own else in turn. A chain of any length is then read, run, spent, restored and
// checked by everything that already knows what an if is, and nothing has to
// learn what a chain is.
//
// One } closes the whole of it, which is what makes the recursion right: every
// block in a chain was opened at the depth of the if that starts it, so each is
// read at depth+1 and the last of them is the one that meets the }.
func parseIfBlocks(node macroNode, lines []string, i, depth int, probs *[]macroProblem) (macroNode, int) {
	problem := func(line int, format string, a ...any) {
		*probs = append(*probs, problemf(line, format, a...))
	}
	// drop reads a block that is not going to run and checks what is inside it,
	// the way a malformed header's is: the statements are still there once the
	// line above them is fixed.
	drop := func(i, opened int) int {
		dropped, next, _ := parseMacroBlock(lines, i, depth+1, opened, probs)
		*probs = append(*probs, checkStatements(dropped)...)
		return next
	}

	var end blockEnd
	node.body, i, end = parseMacroBlock(lines, i, depth+1, node.line, probs)

	rest, line, next, found := nextElse(lines, i, end)
	if !found {
		return node, i
	}
	i, rest = next, strings.TrimSpace(rest)

	if condition, isIf := parseIfHeader(rest); isIf {
		chained := macroNode{isIf: true, isElseIf: true, condition: condition, line: line, raw: strings.TrimSpace(lines[line-1])}
		chained, i = parseIfBlocks(chained, lines, i, depth, probs)
		if condition == "" {
			problem(line, "else if wants a condition in parentheses and a { at the end of the line — } else if (:: … ::) { — so its whole block is dropped")
			*probs = append(*probs, checkStatements(chained.body)...)
			*probs = append(*probs, checkStatements(chained.elseBody)...)
			return node, i
		}
		node.elseBody = []macroNode{chained}
		return node, i
	}

	// A plain else is the keyword and the { and nothing else. It asks nothing —
	// the condition it runs on was written above it — so a line with more on it
	// is one Pob has no reading for.
	if rest != "{" {
		problem(line, "else takes no condition of its own and is written } else { or } else if (:: … ::) { — so its whole block is dropped")
		return node, drop(i, line)
	}

	node.elseBody, i, end = parseMacroBlock(lines, i, depth+1, line, probs)

	// One else is all an if has: the condition it is the other half of has been
	// answered by the time the second one is reached.
	for end == blockElse {
		extra := i
		problem(extra, "the if above this one already has an else — so its whole block is dropped")
		var dropped []macroNode
		dropped, i, end = parseMacroBlock(lines, i, depth+1, extra, probs)
		*probs = append(*probs, checkStatements(dropped)...)
	}
	return node, i
}

// nextElse reports whether an else continues the if whose block has just been
// read, and hands back what follows the keyword — `{`, or the `if (…) {` of a
// chain — the line it is written on, and the index just past that line.
//
// It is written two ways, and they are one statement either way. `} else {`
// closes one block and opens the next on the one line, which is the shape the
// language documents and the one C put in everybody's hands; an `else {` under a
// `}` of its own is the other, and keeps the rule that a } closes a block on a
// line of its own. Only blank lines may come between the two of them — and a
// comment is a blank line by the time the parse sees it.
func nextElse(lines []string, i int, end blockEnd) (rest string, line, next int, found bool) {
	// The block ended on the else itself, so the line is the one just read: what
	// parseMacroBlock stopped at, and hands back the index past.
	if end == blockElse {
		rest, _, _ = cutElse(lines[i-1])
		return rest, i, i, true
	}
	if end != blockClosed {
		return "", 0, i, false
	}
	for j := i; j < len(lines); j++ {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" {
			continue
		}
		// A second } in front of it is one nothing opened, and is left to be
		// reported as that rather than quietly read as part of an else.
		rest, braced, isElse := cutElse(trimmed)
		if !isElse || braced {
			break
		}
		return rest, j + 1, j + 1, true
	}
	return "", 0, i, false
}

// cutElse returns what follows the else on a line that continues an if — `{`, or
// the `if (…) {` of a chain — whether the } that closes the block before it is
// written in front of the keyword, and whether the line is an else at all.
func cutElse(line string) (rest string, braced, isElse bool) {
	line = strings.TrimSpace(line)
	if after, closed := strings.CutPrefix(line, "}"); closed {
		braced = true
		line = strings.TrimSpace(after)
	}
	rest, isElse = cutKeyword(line, macroElseKeyword)
	return rest, braced, isElse
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

// parseOnceHeader reads `once (<condition>) {`.
//
// The condition is PSL text, the same an `if` takes and read the same way, and
// a once is never written without one: the whole statement is a question asked
// of every screen that arrives, so a once with nothing to ask is a block with
// no reason to run. The second return says the line opens a block at all; an
// empty condition on a line that did is what says the header could not be read.
func parseOnceHeader(line string) (string, bool) {
	rest, isOnce := cutKeyword(line, macroOnceKeyword)
	if !isOnce {
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
	if expr == "" || (!psl.HasSlot(expr) && !isBoolLiteral(expr)) {
		return "", true
	}
	return expr, true
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

// parseMacroLine parses `name(arg1, arg2)` or `name("quoted string")`.
//
// Every statement is this shape, `stop()` included, so a line of the macro and a
// line psl filled to the same text arrive at the same statement by going through
// the one reader.
//
// quoted says the argument list was one double-quoted string, which is the only
// thing about a value that the parse knows and the value itself does not: the
// quotes come off here, and `"10m"` and `10m` are the same three characters
// afterwards. A time is not a string — see macroTime — so the one statement that
// takes one is told which it was given.
func parseMacroLine(line string) (name string, args []string, quoted, ok bool) {
	openParen := strings.Index(line, "(")
	if openParen < 0 || !strings.HasSuffix(line, ")") {
		return "", nil, false, false
	}
	name = strings.TrimSpace(line[:openParen])
	if name == "" {
		return "", nil, false, false
	}

	argsStr := strings.TrimSpace(line[openParen+1 : len(line)-1])
	if argsStr == "" {
		return name, []string{}, false, true
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
		return name, []string{result.String()}, true, true
	}

	parts := strings.Split(argsStr, ",")
	args = make([]string, len(parts))
	for i, p := range parts {
		args[i] = strings.TrimSpace(p)
	}
	return name, args, false, true
}

// closeUnterminatedStrings writes back the closing quote a filled statement
// left out, so that what is logged, recorded and replayed is the statement Pob
// reads it as.
//
// psl answers in the shape of the file it is handed, so a macro written
// `typeText("::what to say::)` — a quote nobody closed — comes back
// `typeText("hello)`. parseMacroLine has always read that as `hello`: the
// argument list is what stands between the first parenthesis and the one the
// line ends with, and a string inside it that nothing closes runs to the end of
// that list. The quote therefore goes in front of that closing parenthesis, and
// the statement then says on paper what it does on screen.
//
// A line the parse would not read as a statement is left exactly as it came:
// nothing here guesses at what a line that does not end in `)` was meant to be.
func closeUnterminatedStrings(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = closeUnterminatedString(line)
	}
	return strings.Join(lines, "\n")
}

func closeUnterminatedString(line string) string {
	end := len(strings.TrimRight(line, " \t"))
	if end == 0 || line[end-1] != ')' {
		return line
	}
	open := strings.IndexByte(line[:end], '(')
	if open < 0 || !unterminatedString(line[open+1:end-1]) {
		return line
	}
	return line[:end-1] + `"` + line[end-1:]
}

// statementSlot reports whether a line is one slot and nothing else, which is
// what makes it a statement slot rather than a value written inside a statement.
//
// Nothing else at all, once the comments are off and the line is trimmed. A slot
// with a call around it — `move(:: … ::, 0)` — is an argument and is answered
// with one, and a line holding a slot beside something that is neither a
// statement nor part of one is neither kind and is refused before the run.
func statementSlot(line string) bool {
	line = strings.TrimSpace(line)
	slot, found := psl.FindSlot(line, 0)
	return found && slot.Start == 0 && slot.End == len(line)
}

// hasMacroSlot reports whether any statement holds a :: … :: slot, at any depth.
func hasMacroSlot(nodes []macroNode) bool {
	for _, node := range nodes {
		if node.slots || psl.HasSlot(node.condition) || hasMacroSlot(node.body) || hasMacroSlot(node.elseBody) {
			return true
		}
	}
	return false
}

// macroNeedsPSL reports whether replaying these statements would run psl: a
// slot in one of them, or in a file one of their call()s brings in. path is the
// file the statements were read from, and seen holds the files already accounted
// for, that one included, so a ring of calls is walked once.
//
// The called files are read here rather than found out about at the call,
// because the point of the check is saying so before the cursor moves: a macro
// whose second file cannot be filled is one to refuse at the start, not thirty
// statements in. A call whose path is itself a slot cannot be followed and does
// not need to be — that slot is one of its own, and hasMacroSlot has it already.
//
// A `.macro` is a macro with no compiler behind it, so nothing in one is counted
// however it is written: a slot in a file that says in its name it holds none is
// a contradiction for the check to name, not a reason to go asking for psl. See
// psl.Deterministic and checkDeterministic.
func macroNeedsPSL(nodes []macroNode, path string, seen map[string]bool) bool {
	if !psl.Deterministic(path) && hasMacroSlot(nodes) {
		return true
	}
	return calledFilesNeedPSL(nodes, filepath.Dir(path), seen)
}

func calledFilesNeedPSL(nodes []macroNode, dir string, seen map[string]bool) bool {
	for _, node := range nodes {
		if calledFilesNeedPSL(node.body, dir, seen) || calledFilesNeedPSL(node.elseBody, dir, seen) {
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
		if !psl.Deterministic(path) && psl.HasSlot(text) {
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
		name, args, _, ok := parseMacroLine(strings.TrimSpace(line))
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
		n += countMacroNodes(node.body) + countMacroNodes(node.elseBody)
	}
	return n
}

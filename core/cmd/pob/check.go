// The check before a run (`pob check`): the macro Pob would replay, and whether
// this machine has what replaying it takes.
//
// It reads files and looks commands up on the PATH and talks to nothing, which
// is what makes it the command that answers with Pob closed — the state a macro
// is written in, and the state a new install is in. Everything it finds is
// printed at once, and in two groups, because the two are fixed in different
// places: a macro's problems are fixed by opening the file, and a machine's by
// installing or configuring something. The exit status is what a script goes
// by: 0 when there is nothing to fix, 1 when there is.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"pob/core/internal/agent"
	"pob/core/internal/config"
	"pob/core/internal/psl"
)

// pslRepo is where psl comes from, for the machine that has not got it.
const pslRepo = "https://github.com/lhypds/psl"

// checkReport is everything the check looked at, gathered before a line of it
// is printed: the summary names what was found, and the problems are counted
// before the first one goes out.
type checkReport struct {
	inst     *Instance
	macro    string
	settings map[string]any
	compiler psl.Compiler
	// needsPSL is whether this macro is one psl is started for at all. It
	// decides whether a missing compiler is a problem or beside the point.
	needsPSL bool
	app      string
	appErr   error
	core     string

	// macroProblems are the lines to fix, already formatted; machine is what to
	// install or configure, one sentence each.
	macroProblems []string
	machine       []string
}

func cmdCheck(root string) {
	r := newCheckReport(root)
	r.printSummary()
	r.printProblems()
}

// newCheckReport runs every check there is. Nothing here fails the command: a
// check that cannot be made is a problem to print beside the others, since a
// report that stopped at the first thing wrong would be read as the only thing
// wrong.
func newCheckReport(root string) *checkReport {
	inst := theInstance(root)
	r := &checkReport{
		inst:  inst,
		macro: filepath.Join(inst.Dir, "src", config.MainMacroName),
	}

	settings, problem := readSettings(root)
	r.settings = settings
	if problem != "" {
		r.machine = append(r.machine, problem)
	}

	r.compiler = psl.Compiler{Binary: pslBinary(settings), Dir: root}
	r.needsPSL = agent.MacroFileNeedsPSL(r.macro)
	r.app, r.appErr = findApp()
	if r.appErr == nil {
		r.core, _ = findCore(r.app)
	}

	r.checkMacro()
	r.checkPSL(root)
	r.checkApp()
	r.checkStopHook()
	return r
}

// checkMacro reads src/main.macro.psl and the files it calls.
func (r *checkReport) checkMacro() {
	problems, err := agent.CheckMacroFile(r.macro)
	switch {
	case os.IsNotExist(err):
		// Not an error to report from: an instance that has never been opened
		// has no src/ tree yet, and both the app and `pob new` write one.
		r.macroProblems = append(r.macroProblems, fmt.Sprintf(
			"there is no %s yet — the app writes one when it first starts an instance, and so does `pob new`",
			config.MainMacroName))
	case err != nil:
		r.macroProblems = append(r.macroProblems, fmt.Sprintf("cannot be read: %v", err))
	default:
		for _, p := range problems {
			r.macroProblems = append(r.macroProblems, p.String())
		}
	}
}

// checkPSL looks for the compiler that fills the macro's slots, and for the
// configuration it fills them with — but only for a macro that has slots. A
// macro with none is replayed as written and psl is never started, so whether
// it is installed is not a question about this machine's ability to run it.
func (r *checkReport) checkPSL(root string) {
	if !r.needsPSL {
		return
	}
	binary := r.compiler.Binary
	if !r.compiler.Available() {
		r.machine = append(r.machine, fmt.Sprintf(
			"%s, and %s (or a file it calls) has a :: … :: slot that only psl can fill — install it (%s), or set \"psl\" in settings.json to the path of the executable",
			pslMissing(binary), config.MainMacroName, pslRepo))
		return
	}
	// Only asked once psl is there: what a compiler nobody has is configured
	// with is not the thing to fix first.
	if home, ok := homeDir(); ok && !pslConfigured(root, home) {
		r.machine = append(r.machine, fmt.Sprintf(
			"psl has nothing to fill a slot with: no .pslrc in %s or %s, and no OPENAI_API_KEY or ANTHROPIC_API_KEY in the environment — the model and the key are psl's own, see docs/Pob/06_Settings.md",
			root, home))
	}
}

// checkApp looks for the app the macro would be carried out by, the core behind
// it, and — on Linux, where they are the machine's rather than the bundle's —
// the libraries it is linked against.
func (r *checkReport) checkApp() {
	if r.appErr != nil {
		r.machine = append(r.machine, fmt.Sprintf("%v", r.appErr))
		return
	}
	if r.core == "" {
		r.machine = append(r.machine, fmt.Sprintf(
			"pob-core is not where %s would look for it — the app is the window and core is what runs the macro, so it does nothing at all without it. `pob update` reinstalls the release; in a checkout ./setup.sh builds it",
			filepath.Base(r.app)))
	}
	if missing := missingLibraries(r.app); len(missing) > 0 {
		r.machine = append(r.machine, fmt.Sprintf(
			"the app is linked against libraries this machine does not have — %s. Debian/Ubuntu/Raspberry Pi OS: sudo apt install libgtk-3-0 libjson-glib-1.0-0 libxtst6; Fedora: sudo dnf install gtk3 json-glib libXtst",
			strings.Join(missing, ", ")))
	}
}

// checkStopHook checks the command settings.json runs when a macro finishes.
//
// It is worth checking because of how quietly it fails: the hook is started
// with a shell and never waited for, so one naming something that is not
// installed leaves a run that ends with nothing announcing it and no line
// anywhere saying the command was not there.
func (r *checkReport) checkStopHook() {
	hook, _ := r.settings["stop_hook"].(string)
	hook = strings.TrimSpace(hook)
	if hook == "" {
		return
	}
	command := strings.Fields(hook)[0]
	// A pipeline, a subshell, an assignment in front of the command: what runs
	// then is the shell's business rather than this word, and guessing at it
	// would report a working hook as a broken one.
	if strings.ContainsAny(command, "=$\"'`|&;<>(){}*?") {
		return
	}
	if strings.ContainsRune(command, os.PathSeparator) {
		if info, err := os.Stat(command); err == nil && !info.IsDir() {
			return
		}
		r.machine = append(r.machine, fmt.Sprintf(
			"stop_hook runs %s, and there is no such file — the end of a run would announce nothing, and say nothing about why", command))
		return
	}
	if _, err := exec.LookPath(command); err != nil {
		r.machine = append(r.machine, fmt.Sprintf(
			"stop_hook runs %q, which is not on the PATH — the end of a run would announce nothing, and say nothing about why", command))
	}
}

// printSummary says what was checked and where each piece of it was found. It
// is most of the answer when there is nothing to fix — that psl is the one in
// /opt/homebrew and the app is the one in /Applications is what "nothing to
// fix" is about — and it prints whether or not anything is wrong.
func (r *checkReport) printSummary() {
	label := r.inst.ID
	if r.inst.Name != "" {
		label += " (" + r.inst.Name + ")"
	}
	fmt.Printf("Instance:   %s\n", label)
	fmt.Printf("Macro:      %s\n", r.macro)
	fmt.Printf("psl:        %s\n", r.pslSummary())
	fmt.Printf("App:        %s\n", or(r.app, "not found"))
	fmt.Printf("Core:       %s\n", or(r.core, "not found"))
}

func (r *checkReport) pslSummary() string {
	if !r.needsPSL {
		// Said rather than left as a bare path: a macro with no slots in it is one
		// psl has no part in, and that is worth knowing about a run that is about
		// to be started without it.
		return r.compiler.Describe() + " — not needed: nothing in this macro has a slot"
	}
	return r.compiler.Describe()
}

// printProblems prints the two groups and leaves with 1 when there was
// anything in them. Problems go to stderr and the report to stdout, so a
// script can keep the list and drop the rest.
func (r *checkReport) printProblems() {
	total := len(r.macroProblems) + len(r.machine)
	if total == 0 {
		fmt.Println("\nNothing to fix.")
		return
	}

	if len(r.macroProblems) > 0 {
		fmt.Fprintf(os.Stderr, "\n%s — %s:\n", filepath.Base(r.macro), problemCount(len(r.macroProblems)))
		for _, p := range r.macroProblems {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
	}
	if len(r.machine) > 0 {
		fmt.Fprintf(os.Stderr, "\nThis machine — %s:\n", problemCount(len(r.machine)))
		for _, p := range r.machine {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s to fix.\n", problemCount(total))
	os.Exit(1)
}

// readSettings reads <root>/settings.json, and says so when the file is there
// and is not JSON.
//
// That case earns a problem of its own. The app and the core both read this
// file forgivingly — one they cannot parse is read as an empty one — so a stray
// comma silently puts every setting back to its default, the psl path and the
// ports and the image scale together, with nothing anywhere saying why. A file
// that is simply not there yet is no problem at all: first run writes it.
func readSettings(root string) (map[string]any, string) {
	path := filepath.Join(root, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Sprintf(
			"%s is not valid JSON (%v) — Pob reads a file it cannot parse as an empty one, so every setting in it is being ignored and the built-in default used instead",
			path, err)
	}
	return out, ""
}

// pslBinary is the compiler settings.json names, or the bare name Pob looks for
// on the PATH when it names none. The same answer config.PSLBinary gives, read
// here without going through config, which would create the files it reads.
func pslBinary(settings map[string]any) string {
	if s, ok := settings["psl"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return psl.DefaultBinary
}

// pslMissing says why psl was not found, in the words that fit how it was
// named: a bare name is looked for on the PATH, and a path either is an
// executable or is not one. The distinction is psl.Compiler's own — see its
// resolve — and it is the difference between installing something and fixing a
// line in settings.json.
func pslMissing(binary string) string {
	if strings.ContainsRune(binary, os.PathSeparator) {
		return fmt.Sprintf("there is no executable at %s, which settings.json names as psl", binary)
	}
	return fmt.Sprintf("%s is not on the PATH", binary)
}

// pslConfigured reports whether psl has anything to fill a slot with: a .pslrc
// where it looks for one — the directory Pob runs it in, which is <root>, and
// then the home directory — or a key in the environment, which is what psl
// falls back on without a file.
//
// Whether what is in that file works is psl's own business and nothing here can
// tell. What this can tell is that there is nothing at all, which is the case
// worth a line: every slot then fails at the moment the replay reaches it.
func pslConfigured(root, home string) bool {
	for _, dir := range []string{root, home} {
		if exists(filepath.Join(dir, ".pslrc")) {
			return true
		}
	}
	return os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("ANTHROPIC_API_KEY") != ""
}

// findCore is where the shell app will look for pob-core: beside its own
// executable, and failing that core/bin/pob-core in one of the six directories
// above it, which is how a shell built in a checkout finds the core the dev
// scripts built. It is the rule all three shells use — CoreBridge in
// macos/Sources/Services/CoreBridge.swift, linux-x11/src/core_bridge.c and
// win/src/Services/CoreBridge.cs — read here so the check answers with what the
// app would find rather than with where it ought to be.
func findCore(app string) (string, bool) {
	dir := filepath.Dir(app)
	if strings.HasSuffix(app, ".app") {
		// The bundle's own executable is Contents/MacOS/Pob, and core is beside it.
		dir = filepath.Join(app, "Contents", "MacOS")
	}
	name := "pob-core"
	if runtime.GOOS == "windows" {
		name = "pob-core.exe"
	}
	if beside := filepath.Join(dir, name); exists(beside) {
		return beside, true
	}
	for i := 0; i < 6; i++ {
		if candidate := filepath.Join(dir, "core", "bin", name); exists(candidate) {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// missingLibraries is the shared libraries the app is linked against and cannot
// find — the same reading get.sh takes right after an install, repeated here
// because they also go missing later: a distribution upgrade takes one away and
// the app stops starting with nothing on screen to say why.
//
// Linux only, and by asking ldd, which is the tool that knows. A macOS bundle
// links against frameworks that ship with the system, and the Windows build
// carries its runtime with it.
func missingLibraries(app string) []string {
	if runtime.GOOS != "linux" || app == "" {
		return nil
	}
	ldd, err := exec.LookPath("ldd")
	if err != nil {
		return nil
	}
	// Output, not CombinedOutput: ldd exits non-zero on a file it will not read
	// at all, and that is not a missing library to report.
	out, err := exec.Command(ldd, app).Output()
	if err != nil {
		return nil
	}
	var missing []string
	for _, line := range strings.Split(string(out), "\n") {
		if name, _, found := strings.Cut(strings.TrimSpace(line), "=> not found"); found {
			missing = append(missing, strings.TrimSpace(name))
		}
	}
	return missing
}

func homeDir() (string, bool) {
	home, err := os.UserHomeDir()
	return home, err == nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

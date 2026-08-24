// Launching Pob inside a microVM (`pob launch --msb`). The machine it runs on
// is built and started by vm/msb/launch.sh — microsandbox, Docker, the guest's
// desktop and the mounts this machine's ~/.pob goes in on are all its business,
// and this file's is the one thing the CLI knows that it does not: which
// instance is being launched, and what the launch was asked to do with it.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// msbScriptPath is where the launcher lives, relative to a checkout.
var msbScriptPath = filepath.Join("vm", "msb", "launch.sh")

// msbCountFlag says how many machines a launch is, and msbCountMax is as many
// as one command will start. The cap is not a limit on the host — it is a
// misplaced keystroke: every machine holds its own memory and its own disk (4G
// and 12G by default), so a 100 typed for a 10 would take the host down while
// looking like a launch. Another command starts another twenty.
const (
	msbCountFlag = "--count"
	msbCountMax  = 20
)

// cmdLaunchMSB starts the VM and, with start, the macro on it. Unlike
// cmdLaunch it does not mind a Pob already running here: what it launches is
// another machine's, and the two are only related by the ~/.pob one of them
// copies from the other.
func cmdLaunchMSB(root string, opts launchOptions) {
	if runtime.GOOS == "windows" {
		fail("--msb needs microsandbox, which runs on macOS and Linux — see docs/Pob/16_Microsandbox.md")
	}
	script, err := msbScript()
	if err != nil {
		fail("%v", err)
	}
	// A file named here is read inside the VM, against the copy of the instance
	// that went in — so a bare name is the macro beside main.macro.psl there,
	// and a path is a path on this machine, which is not a place the guest can
	// look. Said rather than passed on: a run of something else, or of nothing,
	// is not what was asked for.
	if strings.ContainsRune(opts.macroPSL, filepath.Separator) || strings.HasPrefix(opts.macroPSL, "~") {
		fail("%s %s names a file on this machine, and with --msb the macro is read inside the VM —\n"+
			"      name one in the instance's src/ instead: %s %s",
			macroPSLFlag, opts.macroPSL, macroPSLFlag, filepath.Base(opts.macroPSL))
	}

	// Which instance goes in, decided here and written to INSTANCE before the
	// VM boots: the guest copies ~/.pob at boot and reads that file to know
	// which of the instances in it is the one to start.
	selectInstance(root, opts.instance)

	// And then how many machines that instance goes into — the second question
	// of the same launch, asked once for all of them.
	count := msbCount(opts)

	for i := 1; i <= count; i++ {
		if count > 1 {
			fmt.Printf("\n── VM %d of %d ──\n", i, count)
		}
		if err := runMSBScript(script, opts); err != nil {
			// The script has said what went wrong itself — everything it
			// refuses, it refuses with a line about how to fix it. Only the
			// exit status is worth carrying up, so a `pob launch --msb && …`
			// reads correctly.
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				// What did come up is still up, and is worth a line: the ones
				// before this were machines, not attempts, and nothing else
				// will say they are there to be stopped.
				if i > 1 {
					fmt.Fprintf(os.Stderr, "pob: VM %d of %d did not start — %s before it still up, listed by `pob`.\n",
						i, count, machines(i-1))
				}
				os.Exit(exit.ExitCode())
			}
			fail("could not run %s: %v", script, err)
		}
	}
	if count > 1 {
		fmt.Printf("\n%d VMs are up — `pob` lists them, `pob kill <name>` stops one.\n", count)
	}
}

// runMSBScript is one machine: the launcher, run against the instance INSTANCE
// now names, with what the launch was told in its environment. Called once per
// machine and never in parallel — the script picks the host ports it publishes
// by looking for free ones, so a second launch has to be able to see the first
// one's before it chooses its own.
func runMSBScript(script string, opts launchOptions) error {
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"POB_MSB_FULLSCREEN="+boolEnv(opts.fullscreen),
		"POB_MSB_START="+boolEnv(opts.start),
		"POB_MSB_MACROPSL="+opts.macroPSL,
		// Whether a viewer opens on the guest's screen when it is up. Which
		// viewer is the script's business — POB_MSB_VIEWER also names one, and
		// a name already in the environment is left alone rather than turned
		// back into a 1 that would find a different viewer than was asked for.
		"POB_MSB_VIEWER="+viewerEnv(opts.viewer),
		// Which release the guest's Linux app is fetched from, when there is no
		// checkout to build one in. This CLI's own version is the answer: the
		// app in the VM should be the app out here.
		"POB_MSB_VERSION="+version,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// msbCount settles how many machines this launch is: the number --count named,
// the number typed at the prompt, or one.
//
// Asked whenever there is somebody to ask, the way the instance list is shown
// whenever there is somebody to show it to — a launch says what it is about to
// do rather than assuming, and enter is the answer for the one machine that is
// almost always wanted. --count is that answer given in advance, and no
// terminal is the way past the question for a scheduled or scripted run.
//
// A name pinned with POB_MSB_NAME is the one exception, and it is refused
// rather than asked: that launch replaces the machine under that name, so ten
// of them would be one machine built ten times over.
func msbCount(opts launchOptions) int {
	pinned := os.Getenv("POB_MSB_NAME") != ""
	if opts.count > 0 {
		if pinned && opts.count > 1 {
			fail("POB_MSB_NAME is %q, and a named launch takes that one machine over — so %s %d would\n"+
				"      build the same machine %d times. Unset it for %d machines of their own",
				os.Getenv("POB_MSB_NAME"), msbCountFlag, opts.count, opts.count, opts.count)
		}
		return opts.count
	}
	if pinned || !isTerminal(os.Stdin) {
		return 1 // one machine by name, or nobody to ask
	}

	answer := prompt("How many VMs? [1]: ")
	if answer == "" {
		return 1
	}
	return msbCountValue(answer)
}

// msbCountValue reads what was typed or given as a number of machines. Nothing
// is guessed from an answer that is not one: a launch that took "ten" for a 1
// would start a single machine in answer to a command that asked for ten, and
// the nine missing ones would only be noticed later.
func msbCountValue(value string) int {
	count, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || count < 1 {
		fail("%q is not a number of VMs — %s takes 1 or more", value, msbCountFlag)
	}
	if count > msbCountMax {
		fail("%d VMs is more than one launch starts: each holds its own memory and disk, so %d is the\n"+
			"      most at a time. Run the launch again for the rest", count, msbCountMax)
	}
	return count
}

// machines is how the line above counts what came up: "the machine" for one,
// "the 4 machines" for more.
func machines(n int) string {
	if n == 1 {
		return "the machine"
	}
	return fmt.Sprintf("the %d machines", n)
}

// takeMSBCount pulls --count out of a launch's arguments the way takeMacroPSL
// pulls --macropsl out of them, and hands back the number it named. 0 is "not
// said", which is the launch that asks.
func takeMSBCount(args []string) (rest []string, count int) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == msbCountFlag:
			// The next argument is the number unless it is another flag, which
			// is what `pob launch --msb --count --start` is: taking it would
			// leave the launch reading "--start" as a number of machines, and
			// the answer would be about that rather than about the number
			// nobody typed.
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				fail("%s is how many VMs to start — `pob launch --msb %s 10`", msbCountFlag, msbCountFlag)
			}
			i++
			value = args[i]
		case strings.HasPrefix(arg, msbCountFlag+"="):
			value = strings.TrimPrefix(arg, msbCountFlag+"=")
		default:
			rest = append(rest, arg)
			continue
		}
		count = msbCountValue(value)
	}
	return rest, count
}

// viewerEnv decides what POB_MSB_VIEWER goes into the launch. --vncviewer turns
// one on; the variable can also name which viewer to open, and a name is the
// more specific of the two — so --vncviewer with one set opens that viewer, and
// --vncviewer with nothing set opens whichever the script finds. Set without the
// flag it is still an opt-in, since the script reads the same variable.
func viewerEnv(on bool) string {
	if named := os.Getenv("POB_MSB_VIEWER"); named != "" && named != "0" {
		return named
	}
	if on {
		return "1"
	}
	return "0"
}

func boolEnv(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// msbScript finds vm/msb/launch.sh — beside the app in an install, or in the
// checkout, whichever this CLI belongs to. Every install ships it: the launch
// needs the Dockerfile and the guest's run script, which are three small files,
// and shipping them is what makes `pob launch --msb` work on a machine that has
// never cloned anything.
func msbScript() (string, error) {
	var tried []string
	look := func(dir string) (string, bool) {
		script := filepath.Join(dir, msbScriptPath)
		if exists(script) {
			return script, true
		}
		tried = append(tried, script)
		return "", false
	}

	if exe, err := os.Executable(); err == nil {
		// The command on the PATH is a symlink into the install; the install is
		// where it points, not where the link is.
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "Helpers" {
			// Pob.app/Contents/Helpers/pob — the scripts are in the bundle's
			// Resources, which is where a bundle keeps files that are not code.
			if script, ok := look(filepath.Join(filepath.Dir(dir), "Resources")); ok {
				return script, nil
			}
			// <install>/Helpers/pob — the Linux install tree, the scripts
			// beside the app rather than under it.
			if script, ok := look(filepath.Dir(dir)); ok {
				return script, nil
			}
		}
		// core/bin/pob — the repository is two directories above the binary,
		// the same place `pob launch` finds the shell builds in.
		if script, ok := look(filepath.Dir(filepath.Dir(dir))); ok {
			return script, nil
		}
	}

	// And a checkout the command was typed inside, which is how a `pob` from
	// somewhere else runs the scripts of the tree being worked on.
	if dir, err := os.Getwd(); err == nil {
		for {
			script := filepath.Join(dir, msbScriptPath)
			if exists(script) {
				return script, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		tried = append(tried, filepath.Join("…", msbScriptPath)+", above the current directory")
	}

	return "", fmt.Errorf("--msb runs %s, and this install has no copy of it:\n"+
		"      looked in %s.\n"+
		"      Reinstall Pob (`pob update`) to get it, or run --msb from a checkout:\n"+
		"      git clone https://github.com/%s",
		msbScriptPath, strings.Join(tried, "\n              "), repoSlug)
}

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
	"strings"
)

// msbScriptPath is where the launcher lives, relative to a checkout.
var msbScriptPath = filepath.Join("vm", "msb", "launch.sh")

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

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"POB_MSB_FULLSCREEN="+boolEnv(opts.fullscreen),
		"POB_MSB_START="+boolEnv(opts.start),
		"POB_MSB_MACROPSL="+opts.macroPSL,
		// Which release the guest's Linux app is fetched from, when there is no
		// checkout to build one in. This CLI's own version is the answer: the
		// app in the VM should be the app out here.
		"POB_MSB_VERSION="+version,
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// The script has said what went wrong itself — everything it refuses,
		// it refuses with a line about how to fix it. Only the exit status is
		// worth carrying up, so a `pob launch --msb && …` reads correctly.
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		fail("could not run %s: %v", script, err)
	}
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

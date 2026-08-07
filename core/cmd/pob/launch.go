// Launching new app instances from the CLI (`pob launch`). The app is
// located relative to this binary — the surrounding .app bundle for the
// packaged macOS CLI, or the shell build outputs under the repo checkout
// for core/bin/pob — started detached, then watched until the new
// instance's control API answers.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// launchInstance starts the Pob app and returns the instance once its control
// API is up; exits with an error when the app is already running, cannot be
// found, or never answers.
func launchInstance(root string) *Instance {
	if inst := theInstance(root); inst.Running {
		fail("Pob is already running (%s)", inst.ID)
	}

	app, err := findApp()
	if err != nil {
		fail("%v", err)
	}
	fmt.Printf("Launching %s…\n", app)
	if err := startApp(app, root); err != nil {
		fail("launch failed: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if inst := theInstance(root); inst.Running {
			fmt.Printf("Instance %s is running.\n", inst.ID)
			return inst
		}
		time.Sleep(300 * time.Millisecond)
	}
	fail("Pob was launched but did not come up within 30s — check %s", filepath.Join(root, "app.log"))
	return nil
}

// findApp locates the Pob app relative to this CLI binary: the packaged
// macOS CLI lives at Pob.app/Contents/Helpers/pob, the dev CLI at
// <repo>/core/bin/pob next to the shell build outputs.
func findApp() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	if filepath.Base(dir) == "Helpers" {
		if bundle := filepath.Dir(filepath.Dir(dir)); strings.HasSuffix(bundle, ".app") {
			return bundle, nil
		}
	}

	repo := filepath.Dir(filepath.Dir(dir))
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			filepath.Join(repo, "macos", ".build", "debug", "Pob"),
			filepath.Join(repo, "macos", ".build", "release", "Pob"),
			filepath.Join(repo, "macos", "macos_app", "Pob.app"),
		}
	case "linux":
		candidates = []string{filepath.Join(repo, "linux-x11", "bin", "pob")}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Pob app found near %s — build it first (./start.sh) or use the packaged app's CLI", dir)
}

// startApp launches the app fully detached from this CLI: .app bundles go
// through `open`, bare binaries get their own session with output appended to
// <root>/app.log like the dev start scripts do.
//
// Plain `open`, not `open -n`: the caller has already established that Pob is
// not running, and -n exists to start a second copy alongside a first, which
// is the thing that is no longer wanted.
func startApp(app, root string) error {
	if strings.HasSuffix(app, ".app") {
		return exec.Command("open", app).Run()
	}
	logFile, err := os.OpenFile(filepath.Join(root, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(app)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachSysProcAttr
	return cmd.Start()
}

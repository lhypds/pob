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

// shellName is what the shell app is built as beside an installed CLI:
// Pob.app on macOS, and a bare executable on the other two — capitalized on
// Windows, where the CLI's own pob.exe would otherwise be the same filename.
func shellName() string {
	switch runtime.GOOS {
	case "windows":
		return "Pob.exe"
	default:
		return "pob"
	}
}

// findApp locates the Pob app relative to this CLI binary. Installed, the CLI
// lives in a Helpers/ directory beside the app on every platform —
// Pob.app/Contents/Helpers/pob in the macOS bundle, <install>/Helpers/pob and
// <install>\Helpers\pob.exe in the Linux and Windows trees. In a checkout it
// is <repo>/core/bin/pob, next to the shell build outputs.
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
		// macOS: Pob.app/Contents/Helpers/pob — the app is the bundle itself.
		if bundle := filepath.Dir(filepath.Dir(dir)); strings.HasSuffix(bundle, ".app") {
			return bundle, nil
		}
		// Linux and Windows: the shell sits one level up, beside Helpers/.
		if app := filepath.Join(filepath.Dir(dir), shellName()); exists(app) {
			return app, nil
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
		candidates = []string{
			filepath.Join(repo, "linux-x11", "bin", "pob"),
			filepath.Join(repo, "linux-x11", "dist", "Pob", "pob"),
		}
	case "windows":
		// The dev build (dotnet build) and the packaged one (dotnet publish),
		// in the order a developer would want them picked.
		candidates = []string{
			filepath.Join(repo, "win", "bin", "Debug", "net8.0-windows", "Pob.exe"),
			filepath.Join(repo, "win", "bin", "Release", "net8.0-windows", "Pob.exe"),
			filepath.Join(repo, "win", "dist", "Pob", "Pob.exe"),
		}
	}
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Pob app found near %s — build it first (./start.sh) or use the packaged app's CLI", dir)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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

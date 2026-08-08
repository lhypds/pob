// Stopping the running instance (`pob kill`). An instance is a pair of
// processes — the shell app and the pob-core child it spawned — and it is the
// shell that has to go: core exits on its own when the pipe to the shell
// closes, writing the instance's end time on its way out. Killing core alone
// would leave a shell with no brain behind it.
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// cmdKill stops the instance this machine is running. Nothing running is not
// a failure: the command's business is that Pob is stopped, and it is.
func cmdKill(root string) {
	inst := theInstance(root)
	if !inst.Running {
		fmt.Printf("Pob is not running (instance %s).\n", inst.ID)
		return
	}

	// Ask the instance itself for the pid rather than trusting instance.json:
	// the same answer that established it is running.
	status, err := inst.get("/status", 3*time.Second)
	if err != nil {
		fail("cannot reach instance %s: %v", inst.ID, err)
	}
	core := int(intField(status, "pid"))
	if core <= 0 {
		fail("instance %s did not report its pid", inst.ID)
	}

	target := core
	if shell := shellPID(core); shell > 0 {
		target = shell
	}

	if err := signalStop(target); err != nil {
		fail("could not stop pid %d: %v", target, err)
	}
	if waitUntilStopped(root, 10*time.Second) {
		fmt.Printf("Instance %s stopped.\n", inst.ID)
		return
	}

	// Still answering after being asked: there is nothing left to ask with.
	// Core goes too, in case it outlived the shell it was asked to follow.
	_ = forceStop(target)
	if target != core {
		_ = forceStop(core)
	}
	if !waitUntilStopped(root, 5*time.Second) {
		fail("instance %s is still answering after pid %d was killed", inst.ID, target)
	}
	fmt.Printf("Instance %s killed.\n", inst.ID)
}

// shellPID is the pid of the shell app that spawned this core, or 0 when the
// parent is something else. The check matters: a pob-core started by hand has
// the terminal for a parent, and killing that is not what `pob kill` means.
func shellPID(core int) int {
	parent, _, ok := processInfo(core)
	if !ok || parent <= 1 {
		return 0
	}
	_, name, ok := processInfo(parent)
	if !ok || !isShellName(name) {
		return 0
	}
	return parent
}

// isShellName recognises the shell binary by the name it is built under: Pob
// on macOS, pob on Linux, Pob.exe on Windows.
func isShellName(name string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	return base == "pob" || base == "pob.exe"
}

// waitUntilStopped polls until the instance stops answering its control port,
// which is what every other command reads as "not running" too.
func waitUntilStopped(root string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !theInstance(root).Running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

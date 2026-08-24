// Stopping the running instance (`pob kill`, `pob shutdown`) and starting it
// again (`pob relaunch`). An instance is a pair of processes — the shell app
// and the pob-core child it spawned — and it is the shell that has to go: core
// exits on its own when the pipe to the shell closes, writing the instance's
// end time on its way out. Killing core alone would leave a shell with no
// brain behind it.
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"pob/core/internal/storage"
)

// killOptions is what `pob kill` was told to stop: nothing, which is the Pob on
// this machine, or a name — and --all, which is every microVM that is up.
type killOptions struct {
	all bool
	// target is an instance, wherever it is running, or a VM by the name of
	// its sandbox. Both are in the listing a bare `pob` prints, which is where
	// a name typed here comes from.
	target string
}

// cmdKill stops the instance this machine is running — `pob kill`, and `pob
// shutdown`, which is the same command under the word for it that reads like
// what it does to an app rather than what it does to a process. Nothing
// running is not a failure: the command's business is that Pob is stopped, and
// it is.
//
// A name stops what that name is running, here or on a machine of its own —
// killNamed settles which. --all is every VM.
func cmdKill(root string, opts killOptions) {
	if opts.all {
		killEveryVM(root)
		return
	}
	if opts.target != "" {
		killNamed(root, opts.target)
		return
	}
	inst := theInstance(root)
	if !inst.Running {
		fmt.Printf("Pob is not running (instance %s).\n", inst.ID)
		return
	}
	stopInstance(root, inst)
}

// killNamed stops what the name names. An instance is stopped wherever it is
// running — this machine, a microVM, or more than one of them — because that is
// what naming an instance asks for: the id is the thing being run, and where it
// happens to be running is an answer this can find rather than a question to
// put back to whoever typed it.
//
// A sandbox name is the machine itself and is looked for first, being the more
// specific of the two: one machine, rather than an instance wherever it is.
func killNamed(root, target string) {
	vms := msbVMs(root)
	for _, vm := range vms {
		if vm.Name != target {
			continue
		}
		if !vm.Running {
			fmt.Printf("The VM %s is not running.\n", vm.Name)
			return
		}
		stopVM(vm)
		return
	}

	instances := storage.ListInstances(root)
	info, ok := findInstance(instances, target)
	if !ok {
		failNoSuchTarget(target, instances, vms)
	}

	// Every place it is up: the machines running a copy of it, and this one.
	var inVMs []msbVM
	for _, vm := range vms {
		if vm.Running && vm.Instance == info.ID {
			inVMs = append(inVMs, vm)
		}
	}
	local := loadInstance(root, info.ID)
	here := local != nil && local.Running

	places := len(inVMs)
	if here {
		places++
	}
	switch places {
	case 0:
		fmt.Printf("Nothing is running instance %s.\n", info.ID)
		return
	case 1:
	default:
		// Said before rather than found out afterwards: one word is about to
		// end more than one thing, and the lines below say which as they go.
		fmt.Printf("Instance %s is running in %d places — stopping all of them.\n", info.ID, places)
	}
	for _, vm := range inVMs {
		stopVM(vm)
	}
	if here {
		stopInstance(root, local)
	}
}

// killEveryVM is `pob kill --all`: every microVM that is up. The Pob on this
// machine is not one of them — that one is `pob kill`, which is the word for it
// with nothing added.
func killEveryVM(root string) {
	if !msbInstalled() {
		fail("microsandbox is not installed, so there is no VM to stop — see docs/Pob/16_Microsandbox.md")
	}
	stopped := 0
	for _, vm := range msbVMs(root) {
		if vm.Running {
			stopVM(vm)
			stopped++
		}
	}
	if stopped == 0 {
		fmt.Println("No Pob VM is running.")
	}
}

// failNoSuchTarget answers a name that is neither, with what the names are: it
// was a near miss or a memory, and both are answered by the list rather than by
// being told the name was wrong.
func failNoSuchTarget(target string, instances []storage.InstanceInfo, vms []msbVM) {
	var known []string
	for _, info := range instances {
		known = append(known, info.ID)
	}
	for _, vm := range vms {
		if vm.Running {
			known = append(known, vm.Name)
		}
	}
	if len(known) == 0 {
		fail("no instance or VM called %q, and there is nothing running — `pob launch` starts one", target)
	}
	fail("no instance or VM called %q. There is: %s", target, strings.Join(known, ", "))
}

func stopVM(vm msbVM) {
	where := ""
	if vm.Instance != "" {
		where = " (instance " + vm.Instance + ")"
	}
	fmt.Printf("Stopping the VM %s%s…\n", vm.Name, where)
	if err := msbStop(vm.Name); err != nil {
		fail("could not stop %s: %v", vm.Name, err)
	}
	fmt.Printf("%s is stopped. `pob launch --msb` starts another.\n", vm.Name)
}

// cmdRelaunch is `pob relaunch`: the instance quit and started again, on the
// same instance and the same settings.
//
// It is the one way back from a shell that is up but no longer doing what it
// should — a window left somewhere unreachable, a permission granted since it
// started — and it is a single command because the pair it replaces has a
// wait between them that has to be right: a launch started before the old
// instance has let go of its port would be refused as one already running.
// stopInstance does not return until the port has stopped answering.
//
// It is also how a Pob changes between fullscreen and a window, since neither
// is anything the running app can be talked into: --fullscreen brings it back
// over the whole screen, and a relaunch without it brings a fullscreen one back
// as an ordinary window — the way out, from the only place there is to type.
func cmdRelaunch(root string, fullscreen bool) {
	inst := theInstance(root)
	if inst.Running {
		stopInstance(root, inst)
	} else {
		fmt.Printf("Pob was not running (instance %s).\n", inst.ID)
	}
	launchInstance(root, fullscreen)
}

// stopInstance takes down an instance already established to be running, and
// does not return until it has stopped answering its control port. A stop that
// cannot be made is the end of the command: `kill` and `relaunch` are about
// this one instance, and there is nothing else for either to go on and do.
func stopInstance(root string, inst *Instance) {
	if err := stopRunning(root, inst); err != nil {
		fail("%v", err)
	}
}

// stopRunning is the stop itself, for the caller that has more to do after one
// that fails — `purge`, which is taking down every instance on the machine and
// should not be left half finished by the one that will not go.
func stopRunning(root string, inst *Instance) error {
	// Ask the instance itself for the pid rather than trusting instance.json:
	// the same answer that established it is running.
	status, err := inst.get("/status", 3*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach instance %s: %v", inst.ID, err)
	}
	core := int(intField(status, "pid"))
	if core <= 0 {
		return fmt.Errorf("instance %s did not report its pid", inst.ID)
	}

	target := core
	if shell := shellPID(core); shell > 0 {
		target = shell
	}

	if err := signalStop(target); err != nil {
		return fmt.Errorf("could not stop pid %d: %v", target, err)
	}
	if waitUntilStopped(root, inst.ID, 10*time.Second) {
		fmt.Printf("Instance %s stopped.\n", inst.ID)
		return nil
	}

	// Still answering after being asked: there is nothing left to ask with.
	// Core goes too, in case it outlived the shell it was asked to follow.
	_ = forceStop(target)
	if target != core {
		_ = forceStop(core)
	}
	if !waitUntilStopped(root, inst.ID, 5*time.Second) {
		return fmt.Errorf("instance %s is still answering after pid %d was killed", inst.ID, target)
	}
	fmt.Printf("Instance %s killed.\n", inst.ID)
	return nil
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
// waitUntilStopped polls the instance that was stopped — by id, not whichever
// one INSTANCE names: `pob kill <instance>` can be aimed at another, and asking
// after the current one would call that stopped the moment this one is.
func waitUntilStopped(root, id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if inst := loadInstance(root, id); inst == nil || !inst.Running {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

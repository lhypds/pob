// Clearing this machine (`pob purge`). Everything Pob has here goes: every
// microVM microsandbox is holding for it — running or stopped — and every
// instance under ~/.pob, with its src/ macros and its whole logs/ tree.
//
// It is `del` for all of them at once, and it does the one thing `del` refuses
// to: what is running is stopped rather than reported, since "all of it" is not
// an instruction that can be finished around whatever happens to be up. That
// makes the question before it the whole of the safeguard, so it is asked with
// the list of what is about to go rather than as a bare yes or no — and, like
// del's, it is refused outright when there is nobody there to answer it.
//
// What stays is the machine's own: settings.json beside INSTANCE, and the
// microsandbox image and the guest's app under ~/.pob/msb, which are a download
// rather than anything of this machine's to lose.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"pob/core/internal/storage"
)

// purgeOptions is what `pob purge` was told. There is no name to take — purge
// is everything on the machine, and one instance is what `del` takes.
type purgeOptions struct {
	yes bool
}

// cmdPurge removes every Pob microVM and deletes every instance.
//
// The order is the point of it, the same way it is in `del`: the VMs go first,
// each stopped and then removed, and only afterwards is anything on this disk
// touched. Then each instance is stopped where it is running and deleted, and
// INSTANCE is left naming something that is actually there.
//
// Nothing here stops at the first thing that will not go. A sandbox microsandbox
// refuses to remove is not a reason to leave the instances in place, so every
// failure is said as it happens and counted, and the exit status at the end is
// what tells a script that something was left behind.
func cmdPurge(root string, opts purgeOptions) {
	// Read before the question is asked, and used for both: the list a person
	// says yes to is the list this then goes through.
	//
	// Not theInstance: resolving the current instance creates one when INSTANCE
	// names nothing, and a purge that made an instance on its way to deleting
	// them all would leave the machine with exactly one.
	vms := msbVMs(root)
	instances := storage.ListInstances(root)
	if len(vms) == 0 && len(instances) == 0 {
		fmt.Printf("Nothing to purge — no instance in %s, and no Pob VM on this machine.\n", root)
		return
	}

	if !opts.yes && !confirmPurge(root, instances, vms) {
		fmt.Println("Nothing deleted.")
		return
	}

	var problems []string
	note := func(format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		fmt.Fprintf(os.Stderr, "pob: %s\n", message)
		problems = append(problems, message)
	}

	// Removed rather than stopped: a stopped sandbox is still a sandbox — still
	// in `msb list`, still holding the disk it was given — and what purge means
	// for a machine is that it is not there any more. `msb rm -f` is the pair,
	// the guest brought down first and then taken away.
	removed := 0
	for _, vm := range vms {
		fmt.Printf("Removing the VM %s…\n", vm.Name)
		if err := msbRemove(vm.Name); err != nil {
			note("could not remove %s: %v", vm.Name, err)
			continue
		}
		forgetVM(root, vm.Name)
		removed++
	}

	deleted := 0
	for _, info := range instances {
		// Stopped first, always: this is about to remove the directory a live
		// Pob is writing into, which is the case `del` refuses outright.
		if inst := loadInstance(root, info.ID); inst != nil && inst.Running {
			if err := stopRunning(root, inst); err != nil {
				note("%v — %s was left in place, since deleting it under a running Pob would leave one writing into a directory that is gone", err, info.ID)
				continue
			}
		}
		if err := storage.DeleteInstance(root, info.ID); err != nil {
			note("could not delete %s: %v", info.ID, err)
			continue
		}
		fmt.Printf("Deleted instance %s.\n", info.ID)
		deleted++
	}

	// INSTANCE last, and only ever pointed at something that is there. Left
	// naming a directory that has gone it is not an error anywhere — just every
	// later command quietly working on nothing.
	remaining := storage.ListInstances(root)
	if len(remaining) == 0 {
		if err := storage.ClearInstanceID(root); err != nil {
			note("deleted every instance, but could not clear INSTANCE: %v", err)
		}
	} else if err := storage.SetInstanceID(root, remaining[0].ID); err != nil {
		note("could not point INSTANCE at %s: %v", remaining[0].ID, err)
	}

	fmt.Printf("Purged %s and %s.\n", countOf(removed, "VM"), countOf(deleted, "instance"))
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "pob: %s left behind — see above.\n", countOf(len(problems), "thing"))
		os.Exit(1)
	}
	if len(remaining) == 0 {
		fmt.Println("Nothing of Pob's is left here but settings.json — `pob launch` starts a fresh instance.")
	}
}

// confirmPurge shows everything that is about to go and waits for a yes. No on
// anything else, including nothing at all: a piped or scripted `pob purge` reads
// as no answer, and no answer is not consent to delete every machine and every
// session on this one.
func confirmPurge(root string, instances []storage.InstanceInfo, vms []msbVM) bool {
	if !isTerminal(os.Stdin) {
		fail("purge asks before deleting, and there is nobody to ask — `pob purge --yes` says it outright")
	}

	fmt.Println("Purge takes everything Pob has on this machine.")

	if len(vms) > 0 {
		fmt.Printf("\n%s:\n", countOf(len(vms), "VM"))
		for _, vm := range vms {
			where := "—"
			if vm.Instance != "" {
				where = "instance " + vm.Instance
			}
			fmt.Printf("  %-10s %-9s %s\n", vm.Name, pick(vm.Running, "running", "stopped"), where)
		}
	}

	if len(instances) > 0 {
		fmt.Printf("\n%s:\n", countOf(len(instances), "instance"))
		for _, info := range instances {
			dir := filepath.Join(root, info.ID)
			name := info.Name
			if name == "" {
				name = "—"
			}
			running := false
			if inst := loadInstance(root, info.ID); inst != nil && inst.Running {
				running = true
			}
			sessions := listSessions(filepath.Join(dir, "logs"))
			fmt.Printf("  %-10s %-20s %-9s %-12s %s\n",
				info.ID, name, pick(running, "running", "stopped"),
				countOf(len(sessions), "session"), dirSize(dir))
		}
	}

	fmt.Println()
	fmt.Println("  The VMs go with their disks, the instances with their macros and every")
	fmt.Println("  session in them, and what is running is stopped on the way. There is no")
	fmt.Println("  undoing it. The machine's settings.json stays.")

	return yesish(prompt("Purge all of it? [y/N]: "))
}

// forgetVM removes the record a launch left for one machine —
// ~/.pob/msb/vms/<name>.json, which is only ever a note about a sandbox that
// exists, naming the instance that went into it. vm/msb/launch.sh clears the
// records of machines microsandbox no longer has at the next launch; this is
// the same tidying, done when the machine is taken away rather than hours later.
func forgetVM(root, name string) {
	_ = os.Remove(filepath.Join(root, "msb", "vms", name+".json"))
}

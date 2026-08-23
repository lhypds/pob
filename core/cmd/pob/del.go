// Deleting an instance (`pob del`). The one command here that destroys
// something: an instance directory is its macros and every session ever
// recorded against it, and there is no copy of that anywhere else — ~/.pob is
// the copy. So this asks first, refuses while anything is running it, and says
// what is about to go rather than only that something is.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"pob/core/internal/storage"
)

// delOptions is what `pob del` was told: which instance, and whether the
// question has already been answered.
type delOptions struct {
	target string
	yes    bool
	// word is del or delete, whichever was typed. What this command answers
	// with is a line to type next, and it should be the word already in use
	// rather than the other name for it.
	word string
}

// cmdDel deletes an instance: the directory, its src/ macros and its whole
// logs/ tree.
//
// The order is the point of it. What the name means is settled first, then
// whether anything is running it — an instance deleted from under a running Pob
// leaves an app writing into a directory that is not there — and only then is
// the question asked. --yes answers it in advance, for the script that meant it.
func cmdDel(root string, opts delOptions) {
	if opts.target == "" {
		fail("%s takes the instance to delete — `pob %s <instance>`, from the list a bare `pob` prints",
			opts.word, opts.word)
	}

	instances := storage.ListInstances(root)
	info, ok := findInstance(instances, opts.target)
	if !ok {
		// A sandbox name reaching here is worth answering as itself: it is a
		// name off the same listing, and what it names is not this machine's to
		// delete — the machine holds its own copy of an instance.
		for _, vm := range msbVMs(root) {
			if vm.Name == opts.target {
				fail("%s is a VM, not an instance — `pob kill %s` stops it, and `msb rm %s` is how a sandbox goes",
					vm.Name, vm.Name, vm.Name)
			}
		}
		failNoSuchTarget(opts.target, instances, nil)
	}

	// Running here, or on a machine of its own. Either way it is stopped first:
	// this deletes the directory a Pob is working in, and the VM has a copy of
	// that directory it would go on writing to for as long as it is up.
	if inst := loadInstance(root, info.ID); inst != nil && inst.Running {
		fail("%s is running here — `pob kill %s` first", info.ID, info.ID)
	}
	for _, vm := range msbVMs(root) {
		if vm.Running && vm.Instance == info.ID {
			fail("%s is running in the VM %s — `pob kill %s` first", info.ID, vm.Name, vm.Name)
		}
	}

	dir := filepath.Join(root, info.ID)
	if !opts.yes && !confirmDelete(root, info, dir, opts.word) {
		fmt.Println("Nothing deleted.")
		return
	}

	if err := storage.DeleteInstance(root, info.ID); err != nil {
		fail("could not delete %s: %v", info.ID, err)
	}
	fmt.Printf("Deleted instance %s.\n", info.ID)

	// The pointer, if it was pointing here. Left as it was, INSTANCE would name
	// a directory that is not there — which is not an error anywhere, just
	// every later command quietly working on nothing.
	if storage.ResolveInstanceID(root) != info.ID {
		return
	}
	remaining := storage.ListInstances(root)
	if len(remaining) == 0 {
		if err := storage.ClearInstanceID(root); err != nil {
			fail("deleted %s, but could not clear INSTANCE: %v", info.ID, err)
		}
		fmt.Println("That was the last instance — `pob launch` starts a fresh one.")
		return
	}
	// The most recently run of what is left: ListInstances is newest first, and
	// the instance someone was last working in is the one they meant next.
	next := remaining[0]
	if err := storage.SetInstanceID(root, next.ID); err != nil {
		fail("deleted %s, but could not point INSTANCE at %s: %v", info.ID, next.ID, err)
	}
	fmt.Printf("%s is now the current instance.\n", next.Label())
}

// confirmDelete shows what is about to go and waits for a yes. No on anything
// else, including nothing at all: a piped or scripted `pob del` reads as no
// answer, and no answer is not consent to delete a week of logs.
func confirmDelete(root string, info storage.InstanceInfo, dir, word string) bool {
	if !isTerminal(os.Stdin) {
		fail("%s asks before deleting, and there is nobody to ask — `pob %s %s --yes` says it outright",
			word, word, info.ID)
	}

	name := ""
	if info.Name != "" {
		name = " (" + info.Name + ")"
	}
	fmt.Printf("Instance %s%s\n", info.ID, name)
	sessions := listSessions(filepath.Join(root, info.ID, "logs"))
	fmt.Printf("  %s, %s in %s\n", countOf(len(sessions), "session"), dirSize(dir), dir)
	fmt.Println("  Its macros and every session in it go with it, and there is no undoing it.")

	return yesish(prompt("Delete it? [y/N]: "))
}

func yesish(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

func countOf(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// dirSize is what the directory comes to, for the line before the question. A
// tree it cannot walk is reported as what was reached rather than as an error:
// this is a number to inform a yes or no, not one anything depends on.
func dirSize(dir string) string {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	switch {
	case total >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(total)/(1<<30))
	case total >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(total)/(1<<20))
	case total >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(total)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", total)
	}
}

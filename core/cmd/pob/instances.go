// Choosing and creating instances (`pob launch`, `pob new`). An instance is
// a directory under ~/.pob holding its own src/ macros and logs; ~/.pob/INSTANCE
// names the one in use. These commands are how that pointer is moved without
// editing the file by hand.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"pob/core/internal/config"
	"pob/core/internal/storage"
)

// cmdNew creates an instance, names it, and makes it the current one. The
// name is what `pob launch` lists it by; without one on the command line it
// is asked for, since an unnamed instance is only an id again.
func cmdNew(root, name string) {
	if name == "" {
		name = prompt("Name for the new instance: ")
	}
	if name == "" {
		fail("a name is needed: pob new \"Work laptop\"")
	}
	// Names are how instances are told apart on the command line, so two of
	// them answering to the same one would make `pob launch <name>` a guess.
	if existing, taken := findInstance(storage.ListInstances(root), name); taken {
		fail("%s is already called %q — pick another name", existing.ID, name)
	}

	info, err := storage.CreateInstance(root, name)
	if err != nil {
		fail("could not create the instance: %v", err)
	}
	// Seed its src/main.macro.psl, so the directory is ready to be opened and
	// edited before the app has ever started it. Settings are the machine's and
	// are already there — a new instance starts configured.
	config.New(root, info.ID)

	running := theInstance(root)
	if err := storage.SetInstanceID(root, info.ID); err != nil {
		fail("could not point INSTANCE at %s: %v", info.ID, err)
	}

	fmt.Printf("Created instance %s (%s).\n", info.Name, info.ID)
	if running.Running {
		fmt.Printf("Pob is still running as %s — quit it first, then `pob launch`.\n", running.ID)
		return
	}
	fmt.Println("It is now the current instance — start it with `pob launch`.")
}

// cmdLaunch picks the instance to start and starts it. The running check
// comes first: a Pob that is already up would refuse the launch anyway, and
// refusing it after INSTANCE had been repointed would leave the machine
// naming an instance it is not running.
func cmdLaunch(root, wanted string) {
	if inst := theInstance(root); inst.Running {
		fail("Pob is already running (%s)", inst.ID)
	}
	selectInstance(root, wanted)
	launchInstance(root)
}

// selectInstance decides which instance `pob launch` should start: the one
// named on the command line, the one picked from the list, or — with nothing
// to choose between, or nobody at the keyboard — whichever INSTANCE already
// names. The chosen id is written to INSTANCE before the app is started,
// since that is what the app reads to know which directory is its own.
func selectInstance(root, wanted string) {
	instances := storage.ListInstances(root)

	if wanted != "" {
		info, ok := findInstance(instances, wanted)
		if !ok {
			fail("no instance called %q — run `pob launch` to pick from the list", wanted)
		}
		setInstance(root, info)
		return
	}

	if len(instances) < 2 || !isTerminal(os.Stdin) {
		return // nothing to choose between, or no one to ask
	}

	current := indexOfInstance(instances, storage.ResolveInstanceID(root))

	// Arrow keys where the terminal can be put into raw mode; a typed number
	// where it cannot.
	if choice, handled := chooseFromList(instances, current); handled {
		if choice < 0 {
			// Backing out of the list is an answer, not a failure: nothing is
			// launched and INSTANCE stays where it was pointing.
			fmt.Println("Cancelled.")
			os.Exit(0)
		}
		setInstance(root, instances[choice])
		return
	}

	plainList(instances, instances[current].ID)
	answer := prompt(fmt.Sprintf("Which instance? [%d]: ", current+1))
	choice, ok := answerToIndex(instances, answer, current)
	if !ok {
		fail("%q is not one of the instances listed", answer)
	}
	setInstance(root, instances[choice])
}

func setInstance(root string, info storage.InstanceInfo) {
	if err := storage.SetInstanceID(root, info.ID); err != nil {
		fail("could not point INSTANCE at %s: %v", info.ID, err)
	}
	config.New(root, info.ID)
}

// findInstance matches what was typed against the ids first and the names
// after, so an id always means the instance it names.
func findInstance(instances []storage.InstanceInfo, wanted string) (storage.InstanceInfo, bool) {
	for _, info := range instances {
		if info.ID == wanted {
			return info, true
		}
	}
	for _, info := range instances {
		if strings.EqualFold(info.Name, wanted) && info.Name != "" {
			return info, true
		}
	}
	return storage.InstanceInfo{}, false
}

func lastRun(info storage.InstanceInfo) string {
	if info.StartTime == 0 {
		return "never run"
	}
	return "last run " + formatTime(info.StartTime)
}

// prompt asks one question on stdout and reads the answer; "" when there is
// nothing to read (a closed or non-interactive stdin), which every caller
// treats as "no answer given".
func prompt(question string) string {
	fmt.Print(question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		fmt.Println()
		return ""
	}
	return strings.TrimSpace(line)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

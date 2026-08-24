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
// name is what `pob launch` lists it by; without one on the command line it is
// asked for, and a blank answer is an answer.
//
// An instance with no name is not a half-made one. Every instance has an id
// from the moment it exists, that id is what INSTANCE holds and what the
// directory under ~/.pob is called, and everything that shows a person an
// instance already falls back to it — the listing prints a dash in the name
// column, the picker and the status line go by Label(). What a name buys is
// `pob launch <name>` and nothing else, and somebody making the one instance
// they will ever have on this machine does not need to think of one first.
func cmdNew(root, name string) {
	if name == "" {
		name = prompt("Name for the new instance (blank for none): ")
	}
	// Names are how instances are told apart on the command line, so two of
	// them answering to the same one would make `pob launch <name>` a guess.
	// Blank is not a name and cannot clash: findInstance never matches one, so
	// an unnamed instance is only ever reachable by the id it is unique by.
	if name != "" {
		if existing, taken := findInstance(storage.ListInstances(root), name); taken {
			fail("%s is already called %q — pick another name", existing.ID, name)
		}
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

	if info.Name != "" {
		fmt.Printf("Created instance %s (%s).\n", info.Name, info.ID)
	} else {
		fmt.Printf("Created instance %s.\n", info.ID)
	}
	if running.Running {
		fmt.Printf("Pob is still running as %s — quit it first, then `pob launch`.\n", running.ID)
		return
	}
	fmt.Println("It is now the current instance — start it with `pob launch`.")
}

// launchOptions is everything a launch was told: which instance, whether the
// macro runs after it, which macro that is, the two modes — the whole screen,
// and a machine of its own — and whether to open a window on that machine.
type launchOptions struct {
	instance   string
	start      bool
	macroPSL   string
	fullscreen bool
	msb        bool
	viewer     bool
}

// cmdLaunch picks the instance to start and starts it. The running check
// comes first: a Pob that is already up would refuse the launch anyway, and
// refusing it after INSTANCE had been repointed would leave the machine
// naming an instance it is not running.
//
// With start, the macro runs as soon as the app is up — the two commands a
// scheduled or scripted run is otherwise a wait between, since `pob start` on
// its own has nothing to talk to until the launch has finished. macroPSL is
// what --macropsl named, "" for the instance's own macro.
//
// With fullscreen, the app comes up over the whole screen with none of its own
// chrome on it — see startApp, which hands the flag to the app itself.
//
// With msb none of this machine is involved past the instance: the launch is a
// microVM's, and cmdLaunchMSB is where it goes.
func cmdLaunch(root string, opts launchOptions) {
	if opts.msb {
		cmdLaunchMSB(root, opts)
		return
	}
	if inst := theInstance(root); inst.Running {
		fail("Pob is already running (%s)", inst.ID)
	}
	selectInstance(root, opts.instance)
	// Resolved before the app is started, and against the instance selectInstance
	// has just settled on, since a bare name is looked for in that instance's
	// src/. A path that is not there is then answered on the spot, rather than
	// after a launch that has nothing left to run.
	macro := ""
	if opts.macroPSL != "" {
		macro = resolveMacroPSL(theInstance(root), opts.macroPSL)
	}
	inst := launchInstance(root, opts.fullscreen)
	if opts.start {
		cmdMacro(inst, macro)
	}
}

// parseLaunchArgs splits what follows `launch` into the instance wanted, the
// flags that came with it, and the file --macropsl named. Everything that is not
// a flag is the name, joined back up: an instance called "Work laptop" arrives
// as two arguments. A mistyped flag is said rather than taken for a name, which
// would otherwise be reported as an instance nobody has.
//
// --macropsl says which macro to run, so on a launch that is not running one it
// is an instruction with nothing to carry it out: said rather than ignored,
// since what was asked for was a run.
func parseLaunchArgs(args []string) launchOptions {
	args, file := takeMacroPSL(args)
	opts := launchOptions{macroPSL: file}
	var name []string
	for _, arg := range args {
		switch {
		case arg == "--start":
			opts.start = true
		case arg == "--fullscreen":
			opts.fullscreen = true
		case arg == "--msb":
			opts.msb = true
		case arg == "--vncviewer":
			opts.viewer = true
		case strings.HasPrefix(arg, "-"):
			fail("unknown launch option %q — run `pob help`", arg)
		default:
			name = append(name, arg)
		}
	}
	if opts.macroPSL != "" && !opts.start {
		fail("%s names the macro to run, so it goes with --start — `pob launch --start %s %s`", macroPSLFlag, macroPSLFlag, opts.macroPSL)
	}
	// The screen --vncviewer opens a window on is the microVM's. A launch on this
	// desktop has its window on this screen already, so the flag there is an
	// instruction with nothing to carry it out: said rather than ignored.
	if opts.viewer && !opts.msb {
		fail("--vncviewer opens a window on the microVM's screen, so it goes with --msb — `pob launch --msb --vncviewer`")
	}
	opts.instance = strings.TrimSpace(strings.Join(name, " "))
	return opts
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

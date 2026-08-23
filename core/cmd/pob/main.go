// pob is the command-line interface to Pob. A machine runs one instance at a
// time — the one ~/.pob/INSTANCE names — and the commands drive it over the
// localhost control API served by pob-core (see internal/ctlserver), found
// through <instance>/instance.json. Log and session inspection reads the
// logs/ tree directly, so it also works when the app is not running.
//
// `pob new` makes another instance and `pob launch` picks which one to start;
// everything else works on whichever is current.
//
// Usage examples:
//
//	pob                              show what is on this machine
//	pob new "Work laptop"            create an instance and switch to it
//	pob launch                       start the app (asks which, with several)
//	pob launch --start               start the app and run its macro
//	pob launch --fullscreen          start it covering the whole screen, with
//	                                 no toolbar — driven from here on
//	pob launch --msb                 start it in a microVM of its own, on a
//	                                 copy of this ~/.pob, with Firefox in it
//	pob launch --msb --vncviewer     …and open a VNC window on its screen
//	pob check                        read the macro and this machine, and say
//	                                 what is wrong with either
//	pob start                        run src/main.macro.psl (pob stop stops it)
//	pob start --macropsl F           run F instead of src/main.macro.psl
//	pob restart                      stop that run and start it again
//	pob record start                 record what you do into the macro
//	pob lock on                      hold the window to its size
//	pob relaunch                     quit the app and start it again
//	pob --session Y                  show one session's details
//	pob mcp start                    register the MCP server with the agent CLIs
//	pob update                       install the latest release over this one
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// version is stamped by the build scripts via -ldflags "-X main.version=…".
var version = "dev"

const usage = `pob — control and inspect Pob from the command line

Everything lives in ~/.pob, created on first use and shared with the Pob app:
settings.json is the machine's, INSTANCE names the instance directory, and that
directory holds its src/ macros and logs/.

Usage: pob [flags] [command] [args]

Flags:
  -v, --version      Print the Pob version, the same as the version command
  --session <id>     Target session; with no command, shows its details

Macro options (on start, check, and launch --start):
  --macropsl <file>  Work on that PSL file instead of the instance's own
                     src/main.macro.psl. A relative path is from the current
                     directory; a bare name not found there is looked for in
                     the instance's src/

Commands:
  (none)             Show what is on this machine: the version, ~/.pob and psl,
                     then every instance — the one a bare launch would start
                     marked * — and every microVM that is up, each with the
                     instance inside it and the vnc:// address of its screen.
                     What the running instance is doing is status; with
                     --session, show that session instead
  launch [instance]  Start the app. With more than one instance and nothing
                     named, lists them and asks which to start — arrow keys to
                     move, enter to start. <instance> is a name or an id
  launch --start     Start the app and run its macro as soon as it is up.
                     --macropsl runs that file in its place
  launch --fullscreen
                     Start the app over the whole screen, with no toolbar and
                     no window buttons — nothing on screen is Pob's to click,
                     so the commands here are what drives it: start, stop,
                     screenshot, status, and kill to quit it again
  launch --msb       Start it in a microVM of its own instead of on this
                     desktop: a Linux machine with a screen nobody is looking
                     at, Firefox installed, and a copy of this machine's ~/.pob
                     inside it. Prints the address to watch it at (VNC) and the
                     web UI's. Every launch is another machine, named msb-xxxx
                     the way an instance is named pb-xxxx, so several run side by
                     side; POB_MSB_NAME=<name> takes one over instead. Needs
                     microsandbox and Docker —
                     see docs/Pob/16_Microsandbox.md
  launch --msb --vncviewer
                     The same launch, with a VNC viewer opened on the guest's
                     screen once Pob is up — TigerVNC where it is installed,
                     macOS's Screen Sharing otherwise. POB_MSB_VIEWER names
                     another. Only with --msb: it is that machine's screen
  new [name]         Create an instance — its own src/ macros and logs — and make
                     it the one Pob starts next
  del, delete        Delete an instance, named after the word: its macros and
                     every session in it. Asks first, unless --yes; refuses
                     while anything is running it, here or in a VM
  status             Live status of the instance
  sessions           List every instance's sessions, under a heading each
  check              Read src/main.macro.psl and the files it calls, and look
                     over what a run needs of this machine — psl, what psl fills
                     slots with, the app and the core behind it, settings.json.
                     Prints everything wrong and runs nothing. Works with
                     nothing running, and exits 1 when there is anything to fix.
                     --macropsl reads that file in its place
  start              Execute src/main.macro.psl (the toolbar Execute button) —
                     the run stop stops. --macropsl runs that file in its place
  stop               Stop the running session
  restart            Stop the running session and start it again, once it has
                     actually stopped. --macropsl runs that file in its place
  reset              Stop the running session and send the cursor back to the
                     corner every replay starts from
  record start       Record what you do into src/main.macro.psl — the toolbar
                     Record button. It appends, and never clears
  record stop        Stop recording
  lock on            Hold the window to its size; dragging it carries the
                     windows underneath along, so a macro's coordinates keep
                     landing where they were recorded
  lock off           Let the window be resized again
  clickthrough on    Pass clicks on the overlay down to the window underneath
  clickthrough off   Keep them; the overlay takes the clicks itself
  kill, shutdown     Quit the running instance: the app and its core
  kill <name>        Stop what that name is running, from the listing a bare pob
                     prints: an instance id or name, stopped wherever it is
                     running — this machine, a microVM, or both — or a sandbox
                     name, which is that VM and the Pob on it. --all is every VM
  relaunch           Quit the running instance and start it again. This is also
                     how fullscreen is left and entered on a Pob that is up:
                     --fullscreen brings it back over the whole screen, and a
                     plain relaunch brings a fullscreen one back as a window
  screenshot         Capture a screenshot; prints the saved file path
  mcp status         Show MCP server info (URL, tools, client config)
  mcp start [port]   Register the MCP server in the user settings of installed
                     agent CLIs (claude, gemini) and print its info. The server
                     itself starts with the instance; [port] moves it there
  mcp stop           Stop the MCP server and remove those registrations
  update             Install the latest release over this one — the same
                     installer the one-line install uses, pointed at this
                     install. Pob has to be closed first
  update --check     Say what is installed and what the latest release is, and
                     install nothing; exits 1 when there is a newer one
  version            Print the Pob version
  help               Show this help

Update options (after "update"):
  --version VER      Install that release instead of the latest — how a release
                     is reinstalled over itself, or an older one gone back to
  --prefix DIR       Install there rather than over this install
  --bin DIR          Where the pob symlink goes (Linux and macOS)

Examples:
  pob                          # what is on this machine?
  pob new "Work laptop"        # create an instance and switch to it
  pob launch                   # start the app (asks which, with several)
  pob launch --start           # start it and run the macro straight away
  pob launch --fullscreen      # start it over the whole screen, no toolbar
  pob launch --msb             # start it in a Linux microVM of its own
  pob launch --msb --start     # …and run the macro in there
  pob launch --msb --vncviewer # …and open a VNC window on its screen
  pob check                    # is the macro sound, and can this machine run it?
  pob start                    # replay this instance's main macro
  pob start --macropsl login.macro.psl   # replay that file instead
  pob restart                  # stop that run and start it again
  pob reset                    # stop it and put the cursor back in its corner
  pob record start             # record what you do into src/main.macro.psl
  pob lock on                  # hold the window to its size
  pob clickthrough off         # let the overlay take clicks again
  pob relaunch                 # quit the app and start it again
  pob --session 1752712400
  pob mcp start
  pob update --check           # is there a newer release?
  pob update                   # install it
`

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pob: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	// -v and --version are what every other command answers its version to, so
	// they answer here too. They are read before the flag package sees them:
	// it knows only the flags declared below and would refuse these two as
	// undefined, printing the whole usage over a question with a one-line
	// answer.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			fmt.Println(version)
			return
		}
	}

	// --fullscreen, --msb and --vncviewer are options of the launch, not of the CLI: they
	// choose how the app is started, so they are read only after the word that
	// starts it. Before the command word they are said rather than left to the
	// flag package, whose "flag provided but not defined" and whole usage
	// answer a near-miss with everything except where the flag goes. The scan
	// stops where the flag package's own does, at the first word that is not a
	// flag, so the same flags after `launch` are the launch's to read.
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			break
		}
		switch arg {
		case "--fullscreen", "-fullscreen", "--msb", "-msb", "--vncviewer", "-vncviewer":
			name := "--" + strings.TrimLeft(arg, "-")
			fail("%s says how to start the app, so it goes after launch — `pob launch %s`", name, name)
		}
	}

	sessionFlag := flag.String("session", "", "target session ID")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	args := flag.Args()
	command := ""
	if len(args) > 0 {
		command = args[0]
	}

	if command == "help" {
		fmt.Print(usage)
		return
	}
	if command == "version" {
		fmt.Println(version)
		return
	}

	root := projectRoot()

	switch command {
	case "":
		if *sessionFlag != "" {
			showSession(root, theInstance(root).ID, *sessionFlag)
			return
		}
		showInstance(root)

	case "launch":
		cmdLaunch(root, parseLaunchArgs(args[1:]))

	case "new":
		cmdNew(root, strings.TrimSpace(strings.Join(args[1:], " ")))

	// del, and delete for whoever types the word out. Not rm: what this takes
	// is an instance rather than a path, and rm reads like the one that takes a
	// path.
	case "del", "delete":
		cmdDel(root, parseDelArgs(args[1:], command))

	case "sessions":
		listSessionsCmd(root)

	case "status":
		showStatus(runningInstance(root))

	// Not runningInstance: the check reads files and talks to nothing, which is
	// the point of it — a macro is checked while it is being written, and an
	// install is checked before it has ever been started.
	case "check":
		cmdCheck(root, macroPSLOnly(args[1:], "check"))

	// Not runningInstance either: `start` is the one command whose "nothing is
	// running" is worth more than the standard line, since it is also what
	// `launch --start` is for.
	case "start":
		cmdStart(root, macroPSLOnly(args[1:], "start"))

	case "stop":
		cmdStop(runningInstance(root))

	// Not runningInstance either, and for the same reason as start: what it
	// starts is a run, so what it says with nothing running is start's line.
	case "restart":
		cmdRestart(root, macroPSLOnly(args[1:], "restart"))

	case "reset":
		cmdReset(runningInstance(root))

	// The word is read before the instance is looked for, so `pob lock onn` is
	// answered as the typo it is whether or not Pob happens to be running —
	// otherwise the reply to a mistyped state would be about something else
	// entirely.
	case "lock":
		locked := onOffWord(args[1:], "lock", "on", "off")
		cmdLock(runningInstance(root), locked)

	case "clickthrough":
		enabled := onOffWord(args[1:], "clickthrough", "on", "off")
		cmdClickThrough(runningInstance(root), enabled)

	case "record":
		recording := onOffWord(args[1:], "record", "start", "stop")
		cmdRecord(runningInstance(root), recording)

	// Not runningInstance: cmdKill decides for itself what a stopped Pob
	// means, and an already-stopped one is an answer rather than an error.
	// shutdown is the same command said the way an app is quit rather than the
	// way a process is.
	case "kill", "shutdown":
		cmdKill(root, parseKillArgs(args[1:], command))

	// Not runningInstance either: half of what it does is a launch, and a
	// stopped Pob is something it can still finish rather than refuse.
	case "relaunch":
		cmdRelaunch(root, fullscreenOnly(args[1:], "relaunch"))

	case "screenshot":
		cmdScreenshot(runningInstance(root))

	// Not runningInstance either: an update replaces the app, so a running one
	// is what stops it — cmdUpdate says so itself, once it knows there is
	// anything to install.
	case "update":
		cmdUpdate(root, args[1:])

	case "mcp":
		sub := ""
		if len(args) > 1 {
			sub = args[1]
		}
		port := 0
		if sub == "start" && len(args) > 2 {
			n, err := strconv.Atoi(args[2])
			if err != nil || n < 1 || n > 65535 {
				fail("bad port %q — expected a number between 1 and 65535", args[2])
			}
			port = n
		}
		cmdMCP(runningInstance(root), sub, port)

	default:
		fail("unknown command %q — run `pob help`", command)
	}
}

// projectRoot returns ~/.pob — the single project root shared by every Pob
// component. Only the directory itself is made here: which instance inside it
// is in use is a question for the command that needs one, and `pob new` needs
// it not to have been answered already.
func projectRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fail("cannot determine home directory: %v", err)
	}
	root := filepath.Join(home, ".pob")
	if err := os.MkdirAll(root, 0o755); err != nil {
		fail("cannot create %s: %v", root, err)
	}
	return root
}

// runningInstance is the instance for the commands that drive a live app,
// which is every command that isn't reading the logs.
func runningInstance(root string) *Instance {
	inst := theInstance(root)
	if !inst.Running {
		fail("Pob is not running — start it with `pob launch`")
	}
	return inst
}

// onOffWord reads the one word a switch takes — `lock on`, `record stop` — and
// answers true for the first of the two.
//
// Nothing is assumed for a missing word. `pob lock` could only be read as one
// of the two states or as a question about which it is in, and guessing either
// would move the window in answer to a command that did not say to.
// fullscreenOnly reads the arguments of a command whose only option is
// --fullscreen and says whether it was given. Anything else is said rather than
// passed over: a mistyped flag taken for nothing would start the app in the
// mode the command was typed to change.
func fullscreenOnly(args []string, command string) bool {
	fullscreen := false
	for _, arg := range args {
		if arg != "--fullscreen" {
			fail("%s takes no arguments besides --fullscreen — %q is not one, run `pob help`", command, arg)
		}
		fullscreen = true
	}
	return fullscreen
}

// parseKillArgs splits what follows `kill` into what is being stopped: nothing,
// which is the Pob on this machine, or a name — an instance, or the sandbox a
// microVM runs under. Everything that is not a flag is that name, joined back
// up, since an instance called "Work laptop" arrives as two arguments.
func parseKillArgs(args []string, command string) killOptions {
	opts := killOptions{}
	var names []string
	for _, arg := range args {
		switch arg {
		case "--all", "-all":
			opts.all = true
		case "--msb", "-msb":
			// It was the flag for this a version ago, and it reads like it
			// still should be — so it is answered with the shape that replaced
			// it rather than as an unknown option.
			fail("%s takes the name itself — `pob %s <instance or VM>`, or `pob %s` for the Pob on this machine",
				command, command, command)
		default:
			if strings.HasPrefix(arg, "-") {
				fail("unknown %s option %q — run `pob help`", command, arg)
			}
			names = append(names, arg)
		}
	}
	opts.target = strings.TrimSpace(strings.Join(names, " "))
	if opts.all && opts.target != "" {
		fail("--all is every VM, so it goes without a name — `pob %s --all`, or `pob %s %s`",
			command, command, opts.target)
	}
	return opts
}

// parseDelArgs splits what follows `del` into the instance to delete and
// whether the confirmation has been answered in advance. Everything that is not
// a flag is the name, joined up, the same way `kill` and `launch` take one.
func parseDelArgs(args []string, command string) delOptions {
	opts := delOptions{word: command}
	var names []string
	for _, arg := range args {
		switch arg {
		case "--yes", "-yes", "-y":
			opts.yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				fail("unknown %s option %q — run `pob help`", command, arg)
			}
			names = append(names, arg)
		}
	}
	opts.target = strings.TrimSpace(strings.Join(names, " "))
	return opts
}

func onOffWord(args []string, command, on, off string) bool {
	if len(args) == 0 {
		fail("%s takes %s or %s — `pob %s %s`", command, on, off, command, on)
	}
	if len(args) > 1 {
		fail("%s takes one word, %s or %s — %q is a second", command, on, off, args[1])
	}
	switch args[0] {
	case on:
		return true
	case off:
		return false
	}
	fail("%s takes %s or %s, and %q is neither", command, on, off, args[0])
	return false
}

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
//	pob                              show the instance
//	pob new "Work laptop"            create an instance and switch to it
//	pob launch                       start the app (asks which, with several)
//	pob launch --start               start the app and run its macro
//	pob check                        read the macro and this machine, and say
//	                                 what is wrong with either
//	pob start                        run src/main.macro.psl (pob stop stops it)
//	pob start --macropsl F           run F instead of src/main.macro.psl
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
  (none)             Show the instance and its sessions; with --session show
                     that session
  launch [instance]  Start the app. With more than one instance and nothing
                     named, lists them and asks which to start — arrow keys to
                     move, enter to start. <instance> is a name or an id
  launch --start     Start the app and run its macro as soon as it is up.
                     --macropsl runs that file in its place
  new [name]         Create an instance — its own src/ macros and logs — and make
                     it the one Pob starts next
  status             Live status of the instance
  sessions           List the instance's sessions
  check              Read src/main.macro.psl and the files it calls, and look
                     over what a run needs of this machine — psl, what psl fills
                     slots with, the app and the core behind it, settings.json.
                     Prints everything wrong and runs nothing. Works with
                     nothing running, and exits 1 when there is anything to fix.
                     --macropsl reads that file in its place
  start              Execute src/main.macro.psl (the toolbar Execute button) —
                     the run stop stops. --macropsl runs that file in its place
  stop               Stop the running session
  kill               Quit the running instance: the app and its core
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
  pob                          # what's running?
  pob new "Work laptop"        # create an instance and switch to it
  pob launch                   # start the app (asks which, with several)
  pob launch --start           # start it and run the macro straight away
  pob check                    # is the macro sound, and can this machine run it?
  pob start                    # replay this instance's main macro
  pob start --macropsl login.macro.psl   # replay that file instead
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
		wanted, start, macro := parseLaunchArgs(args[1:])
		cmdLaunch(root, wanted, start, macro)

	case "new":
		cmdNew(root, strings.TrimSpace(strings.Join(args[1:], " ")))

	case "sessions":
		listSessionsCmd(root, theInstance(root).ID)

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

	// Not runningInstance: cmdKill decides for itself what a stopped Pob
	// means, and an already-stopped one is an answer rather than an error.
	case "kill":
		cmdKill(root)

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

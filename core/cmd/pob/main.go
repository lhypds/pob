// pob is the command-line interface to Pob. A machine runs one instance and
// it keeps one id, so there is nothing to pick between: the commands drive it
// over the localhost control API served by pob-core (see internal/ctlserver),
// found through logs/<instance>/control.json. Log and session inspection reads
// the logs/ tree directly, so it also works when the app is not running.
//
// Usage examples:
//
//	pob                              show the instance
//	pob launch                       start the app
//	pob --session Y                  show one session's details
//	pob start                        run instruction.txt
//	pob run "open the settings"      replace instruction.txt, then run it
//	pob mcp start                    start the MCP server and print its info
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pob/core/internal/config"
)

// version is stamped by the build scripts via -ldflags "-X main.version=…".
var version = "dev"

const usage = `pob — control and inspect Pob from the command line

All project files (settings.json, instruction.txt, macro.txt, logs/) live in
~/.pob, created on first use and shared with the Pob app.

Usage: pob [flags] [command] [args]

Flags:
  --session <id>     Target session; with no command, shows its details

Commands:
  (none)             Show the instance and its sessions; with --session show
                     that session
  launch             Start the app (alias: new)
  status             Live status of the instance
  sessions           List the instance's sessions
  start              Execute instruction.txt (the toolbar Execute button)
  run <text...>      Replace instruction.txt with <text>, then execute it
  macro              Execute macro.txt
  stop               Stop the running session
  screenshot         Capture a screenshot; prints the saved file path
  mcp status         Show MCP server info (URL, tools, client config)
  mcp start [port]   Start the MCP server and print its info (port defaults
                     to 8032). Also registers the server in the user settings
                     of installed agent CLIs (claude, gemini).
  mcp stop           Stop the MCP server and remove those registrations
  version            Print the Pob version
  help               Show this help

Examples:
  pob                          # what's running?
  pob launch                   # start the app
  pob run "click the Save button and close the dialog"
  pob --session 1752712400
  pob mcp start
`

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pob: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
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

	case "launch", "new":
		launchInstance(root)

	case "sessions":
		listSessionsCmd(root, theInstance(root).ID)

	case "status":
		showStatus(runningInstance(root))

	case "start":
		cmdStart(runningInstance(root), "")

	case "run":
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			fail("run needs the instruction text: pob run \"open the settings\"")
		}
		cmdStart(runningInstance(root), text)

	case "macro":
		cmdMacro(runningInstance(root))

	case "stop":
		cmdStop(runningInstance(root))

	case "screenshot":
		cmdScreenshot(runningInstance(root))

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
// component — created and seeded with the standard first-run files
// (settings.json, instruction.txt, macro.txt, logs/) on first use.
func projectRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fail("cannot determine home directory: %v", err)
	}
	root := filepath.Join(home, ".pob")
	config.New(root, "")
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

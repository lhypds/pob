// pob is the command-line interface to Pob. It discovers running instances
// through logs/<instance>/control.json and drives them over the localhost
// control API served by pob-core (see internal/ctlserver); log and session
// inspection reads the logs/ tree directly, so it also works for stopped
// instances.
//
// Usage examples:
//
//	pob                              list instances
//	pob --instance 1752712345        show that instance
//	pob --instance X --session Y     show one session's details
//	pob start                        run instruction.txt on the running instance
//	pob run "open the settings"      replace instruction.txt, then run it
//	pob --instance X mcp start       start the MCP server and print its info
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const usage = `pob — control and inspect Pob from the command line

Usage: pob [flags] [command] [args]

Flags:
  --root <dir>       Project root (default: $POB_ROOT, else searched upward from
                     the current directory for settings.json + logs/)
  --instance <id>    Target instance (default: the only running one)
  --session <id>     Target session; with no command, shows its details

Commands:
  (none)             List instances; with --instance show that instance;
                     with --session show that session
  list               List instances (aliases: ls, instances)
  status             Live status of the target instance
  sessions           List the target instance's sessions
  start              Execute instruction.txt (the toolbar Execute button)
  run <text...>      Replace instruction.txt with <text>, then execute it
  macro              Execute macro.txt
  stop               Stop the running session
  screenshot         Capture a screenshot; prints the saved file path
  mcp status         Show MCP server info (URL, tools, client config)
  mcp start [port]   Start the MCP server and print its info (requires --instance;
                     port defaults to 8032). Also registers the server in the
                     user settings of installed agent CLIs (claude, gemini).
  mcp stop           Stop the MCP server and remove those registrations
  version            Print the Pob version
  help               Show this help

Examples:
  pob                          # what's running?
  pob run "click the Save button and close the dialog"
  pob --instance 1752712345 start
  pob --instance 1752712345 --session 1752712400
  pob --instance 1752712345 mcp start
`

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pob: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	rootFlag := flag.String("root", "", "project root holding settings.json and logs/")
	instanceFlag := flag.String("instance", "", "target instance ID")
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
		root := resolveRoot(*rootFlag)
		data, err := os.ReadFile(filepath.Join(root, "VERSION"))
		if err != nil {
			fail("cannot read VERSION: %v", err)
		}
		fmt.Println(strings.TrimSpace(string(data)))
		return
	}

	root := resolveRoot(*rootFlag)

	switch command {
	case "":
		switch {
		case *sessionFlag != "":
			showSession(root, resolveAnyInstance(root, *instanceFlag), *sessionFlag)
		case *instanceFlag != "":
			showInstance(root, *instanceFlag)
		default:
			listInstances(root)
		}

	case "list", "ls", "instances":
		listInstances(root)

	case "sessions":
		listSessionsCmd(root, resolveAnyInstance(root, *instanceFlag))

	case "status":
		showStatus(resolveRunningInstance(root, *instanceFlag))

	case "start":
		cmdStart(resolveRunningInstance(root, *instanceFlag), "")

	case "run":
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" {
			fail("run needs the instruction text: pob run \"open the settings\"")
		}
		cmdStart(resolveRunningInstance(root, *instanceFlag), text)

	case "macro":
		cmdMacro(resolveRunningInstance(root, *instanceFlag))

	case "stop":
		cmdStop(resolveRunningInstance(root, *instanceFlag))

	case "screenshot":
		cmdScreenshot(resolveRunningInstance(root, *instanceFlag))

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
		// Starting MCP binds the instance to the shared MCP port, so the
		// choice must be explicit — no defaulting to "the only running one".
		if sub == "start" && *instanceFlag == "" {
			fail("mcp start needs an explicit target: pob --instance <id> mcp start (see `pob list`)")
		}
		cmdMCP(resolveRunningInstance(root, *instanceFlag), sub, port)

	default:
		fail("unknown command %q — run `pob help`", command)
	}
}

// resolveRoot finds the project root: --root flag, $POB_ROOT, else walk up
// from the current directory looking for settings.json next to logs/.
func resolveRoot(flagValue string) string {
	if flagValue != "" {
		abs, err := filepath.Abs(flagValue)
		if err != nil {
			fail("bad --root: %v", err)
		}
		return abs
	}
	if env := os.Getenv("POB_ROOT"); env != "" {
		return env
	}
	dir, err := os.Getwd()
	if err != nil {
		fail("cannot determine working directory: %v", err)
	}
	for {
		if isProjectRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	fail("no Pob project found here — run from the project directory, set $POB_ROOT, or pass --root")
	return ""
}

func isProjectRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "logs"))
	return err == nil && info.IsDir()
}

// resolveRunningInstance returns the live instance to control: the one named
// by --instance (which must be running), or the only running one.
func resolveRunningInstance(root, id string) *Instance {
	if id != "" {
		inst := loadInstance(root, id)
		if inst == nil {
			fail("instance %s not found under %s", id, filepath.Join(root, "logs"))
		}
		if !inst.Running {
			fail("instance %s is not running", id)
		}
		return inst
	}
	var running []*Instance
	for _, inst := range discoverInstances(root) {
		if inst.Running {
			running = append(running, inst)
		}
	}
	switch len(running) {
	case 0:
		fail("no running Pob instance found — start the app first")
	case 1:
		return running[0]
	}
	ids := make([]string, len(running))
	for i, inst := range running {
		ids[i] = inst.ID
	}
	fail("multiple running instances (%s) — pick one with --instance", strings.Join(ids, ", "))
	return nil
}

// resolveAnyInstance returns an instance ID for inspection commands, which
// work on stopped instances too: --instance if given, else the only running
// instance, else the only instance on disk.
func resolveAnyInstance(root, id string) string {
	if id != "" {
		if loadInstance(root, id) == nil {
			fail("instance %s not found under %s", id, filepath.Join(root, "logs"))
		}
		return id
	}
	instances := discoverInstances(root)
	if len(instances) == 0 {
		fail("no instances found under %s", filepath.Join(root, "logs"))
	}
	var running []*Instance
	for _, inst := range instances {
		if inst.Running {
			running = append(running, inst)
		}
	}
	if len(running) == 1 {
		return running[0].ID
	}
	if len(running) == 0 && len(instances) == 1 {
		return instances[0].ID
	}
	fail("several instances match — pick one with --instance (see `pob list`)")
	return ""
}

// pob-core is the platform-independent brain of Pob. It is spawned by the
// native shell (macos/) as a child process and speaks line-delimited JSON-RPC
// over stdin/stdout. It owns the LLM client, session logs, Prompt Script
// Language parsing and replay, and the MCP server; all screen perception and
// operation is delegated back to the shell.
//
// Usage: pob-core --root <project-root>
package main

import (
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"pob/core/internal/agent"
	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/config"
	"pob/core/internal/ctlserver"
	"pob/core/internal/ipc"
	"pob/core/internal/llm"
	"pob/core/internal/mcpserver"
	"pob/core/internal/storage"
	"pob/server"
)

// macroRecorder lets the MCP server write to macro.psl under the one toggle
// everything else obeys — the shell's record button, which flips
// runner.SetRecording. An MCP client drives the same machine a replay does, so
// what it did belongs in the same recording.
type macroRecorder struct {
	runner *agent.Runner
	cfg    *config.Config
}

func (m macroRecorder) Recording() bool           { return m.runner.Recording() }
func (m macroRecorder) AppendToMacro(line string) { m.cfg.AppendToMacro(line) }

func main() {
	root := flag.String("root", "", "project root holding INSTANCE and the <instance>/ directories")
	instance := flag.String("instance", "", "<instance> directory resolved by the shell; holds this instance's macro.psl and logs/")
	flag.Parse()
	if *root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			os.Exit(1)
		}
		*root = filepath.Join(home, ".pob")
	}

	// A machine has one instance and it keeps its id for good. The shell
	// resolves it before spawning us, because it needs it for the toolbar;
	// without one — running headless, or from the CLI — it is worked out here
	// the same way, and both arrive at the same answer.
	instanceID := *instance
	if instanceID == "" {
		instanceID = storage.ResolveInstanceID(*root)
	}

	applog.Init(*root)
	cfg := config.New(*root, instanceID)
	store := storage.New(*root, instanceID, cfg.SettingsDict, cfg.Macro)

	client := ipc.NewStdio()
	// Frames go down their own connection rather than sharing the JSON-RPC
	// line with every mouse move — opened here, before anything can ask for
	// one. A shell that does not take up the offer keeps answering the old
	// way, so this failing is not worth refusing to start over.
	if err := client.ServeFrames(); err != nil {
		applog.Logf("IPC: no frame channel (%v); frames will come down the JSON-RPC line", err)
	}
	br := bridge.New(client)
	runner := agent.NewRunner(cfg, store, llm.New(cfg), br)

	client.Handle("run.macro", func(params map[string]any) {
		runner.RunMacro()
	})
	client.Handle("run.stop", func(params map[string]any) {
		runner.Stop()
	})
	client.Handle("recording.changed", func(params map[string]any) {
		recording, _ := params["recording"].(bool)
		runner.SetRecording(recording)
	})
	client.Handle("screenshot.take", func(params map[string]any) {
		runner.TakeScreenshot()
	})

	// The MCP server starts with the instance, on the port a client's config
	// names: registering it is a thing done once, and a client that then finds
	// nothing listening reports a broken server rather than a stopped one. So
	// it is up whenever the app is, and "mcp": false in settings.json is what
	// keeps the port closed. `pob mcp start` still moves it to another port.
	mcp := mcpserver.New(br)
	mcp.SetRecorder(macroRecorder{runner: runner, cfg: cfg})
	if cfg.MCPEnabled() {
		if err := mcp.Start(cfg.MCPHost(), cfg.MCPPort()); err != nil {
			applog.Logf("MCPServer: not started: %v", err)
		}
	}

	// The Pob server does start with the instance, so that reaching for a
	// phone is all it takes: it serves the API and the web UI at
	// http://<machine>:<port> for anyone on the same network, which is why
	// "server": false in settings.json turns it off.
	srv := server.New(store.InstanceID(), br.Remote(), applog.Logf)
	// What the index page reports. The server knows its own address and
	// nothing else about the instance running behind it, so the rest is read
	// here, where it is known — and read on each request, since the whole
	// point of the page is what is true now.
	srv.SetStatus(func() server.Status {
		return server.Status{
			Root:      cfg.Root,
			Model:     cfg.Model(),
			Executing: runner.Running(),
			Session:   runner.CurrentSession(),
			Recording: runner.Recording(),
			ViewFPS:   cfg.ViewFPS(),
			// Every address it answers on, not a fixed loopback one: this page
			// is read from another machine as often as from this one, and the
			// address that machine needs is the address it has to be told.
			MCP: mcp.URLs(),
		}
	})
	if cfg.ServerEnabled() {
		if err := srv.Start(cfg.ServerPort()); err != nil {
			applog.Logf("Server: not started: %v", err)
		}
	}
	// Told once, here: the server is started with the instance and runs for as
	// long as it does, so this address is the address until the app is quit.
	br.NotifyServerState(srv.Running(), srv.URL())

	// The control server lets the `pob` CLI drive this instance; it always
	// runs, on an ephemeral port advertised in <instance>/instance.json.
	ctl := ctlserver.New(cfg, store, runner, mcp, srv, br)
	_ = ctl.Start()

	applog.Logf("pob-core started (instance %s)", store.InstanceID())
	store.WriteInstanceStart()

	// Record the end time when killed directly (e.g. stop.sh straggler cleanup).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		ctl.Stop()
		srv.Stop()
		store.WriteInstanceEnd()
		os.Exit(0)
	}()

	// Blocks until stdin closes — i.e. the shell exits — then we exit too.
	client.Run()
	ctl.Stop()
	srv.Stop()
	store.WriteInstanceEnd()
}

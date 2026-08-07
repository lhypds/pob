// pob-core is the platform-independent brain of Pob. It is spawned by the
// native shell (macos/) as a child process and speaks line-delimited JSON-RPC
// over stdin/stdout. It owns the agent loop, the LLM client, session logs,
// macro parsing and the MCP server; all screen perception and operation is
// delegated back to the shell.
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
	"pob/webui"
)

func main() {
	root := flag.String("root", "", "project root holding settings.json, instruction.txt, macro.txt and logs/")
	instance := flag.String("instance", "", "logs/<instance> directory resolved by the shell; holds this instance's settings.json and session logs")
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
	store := storage.New(cfg.LogsDir(), instanceID, cfg.SettingsDict, cfg.Instruction, cfg.Macro)

	client := ipc.NewStdio()
	br := bridge.New(client)
	runner := agent.NewRunner(cfg, store, llm.New(cfg), br)

	client.Handle("run.instruction", func(params map[string]any) {
		if recording, ok := params["recording"].(bool); ok {
			runner.SetRecording(recording)
		}
		runner.RunInstruction()
	})
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

	// The MCP server never starts with the app — it is started on demand via
	// `pob mcp --instance <id> start` (see internal/ctlserver).
	mcp := mcpserver.New(br)

	// The web UI does start with the app, so that reaching for a phone is all
	// it takes: it serves at http://<machine>:<port> for anyone on the same
	// network, which is why "webui": false in settings.json turns it off.
	web := webui.New(store.InstanceID(), br.Remote(), applog.Logf)
	if cfg.WebUI() {
		if err := web.Start(cfg.WebUIPort()); err != nil {
			applog.Logf("WebUI: not started: %v", err)
		}
	}

	// The control server lets the `pob` CLI drive this instance; it always
	// runs, on an ephemeral port advertised in logs/<instance>/control.json.
	ctl := ctlserver.New(cfg, store, runner, mcp, web, br)
	_ = ctl.Start()

	applog.Logf("pob-core started (instance %s)", store.InstanceID())
	store.WriteInstanceStart()

	// Record the end time when killed directly (e.g. stop.sh straggler cleanup).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		ctl.Stop()
		web.Stop()
		store.WriteInstanceEnd()
		os.Exit(0)
	}()

	// Blocks until stdin closes — i.e. the shell exits — then we exit too.
	client.Run()
	ctl.Stop()
	web.Stop()
	store.WriteInstanceEnd()
}

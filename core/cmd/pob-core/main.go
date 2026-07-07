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
	"syscall"

	"pob/core/internal/agent"
	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/config"
	"pob/core/internal/ipc"
	"pob/core/internal/llm"
	"pob/core/internal/mcpserver"
	"pob/core/internal/storage"
)

func main() {
	root := flag.String("root", "", "project root holding settings.json, instruction.txt, macro.txt and logs/")
	instance := flag.String("instance", "", "logs/<instance> directory allocated by the shell; holds this instance's settings.json and session logs")
	flag.Parse()
	if *root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		*root = cwd
	}

	applog.Init(*root)
	cfg := config.New(*root, *instance)
	store := storage.New(cfg.LogsDir(), *instance, cfg.SettingsDict, cfg.Instruction, cfg.Macro)

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

	if cfg.StartMCP() {
		mcpserver.New(br).Start(cfg.MCPPort())
	}

	applog.Logf("pob-core started (instance %s)", store.InstanceID())
	store.WriteInstanceStart()

	// Record the end time when killed directly (e.g. stop.sh straggler cleanup).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		store.WriteInstanceEnd()
		os.Exit(0)
	}()

	// Blocks until stdin closes — i.e. the shell exits — then we exit too.
	client.Run()
	store.WriteInstanceEnd()
}

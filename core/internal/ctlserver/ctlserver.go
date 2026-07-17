// Package ctlserver exposes a small localhost HTTP control API so the `pob`
// CLI can drive a running instance. Unlike the MCP server it always starts,
// on an ephemeral 127.0.0.1 port, and advertises itself by writing
// logs/<instance>/control.json ({pid, port}) which the CLI scans to discover
// live instances. Endpoints:
//
//	GET  /status           — instance id, executing/recording state, MCP state
//	GET  /mcp              — MCP server info (running, port, url, tools)
//	POST /mcp/start        — start the MCP server; body: none
//	POST /mcp/stop         — stop the MCP server
//	POST /run/instruction  — run instruction.txt; optional body {"instruction": "..."} replaces it first
//	POST /run/macro        — run macro.txt
//	POST /run/stop         — stop the running session
//	POST /screenshot       — capture and save a screenshot, returns its path
package ctlserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"pob/core/internal/agent"
	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	"pob/core/internal/config"
	"pob/core/internal/mcpserver"
	"pob/core/internal/storage"
)

type Server struct {
	cfg    *config.Config
	store  *storage.Storage
	runner *agent.Runner
	mcp    *mcpserver.Server
	br     *bridge.Bridge

	server *http.Server
	port   int
}

func New(cfg *config.Config, store *storage.Storage, runner *agent.Runner, mcp *mcpserver.Server, br *bridge.Bridge) *Server {
	return &Server{cfg: cfg, store: store, runner: runner, mcp: mcp, br: br}
}

func (s *Server) controlFile() string {
	return filepath.Join(s.store.InstanceDir(), "control.json")
}

// Start binds an ephemeral localhost port, writes control.json and serves in
// a background goroutine.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/mcp", s.handleMCPInfo)
	mux.HandleFunc("/mcp/start", s.handleMCPStart)
	mux.HandleFunc("/mcp/stop", s.handleMCPStop)
	mux.HandleFunc("/run/instruction", s.handleRunInstruction)
	mux.HandleFunc("/run/macro", s.handleRunMacro)
	mux.HandleFunc("/run/stop", s.handleRunStop)
	mux.HandleFunc("/screenshot", s.handleScreenshot)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		applog.Logf("CtlServer: listen failed: %v", err)
		return err
	}
	s.port = listener.Addr().(*net.TCPAddr).Port

	data, _ := storage.PrettyJSON(map[string]any{
		"pid":        os.Getpid(),
		"port":       s.port,
		"start_time": time.Now().Unix(),
	})
	if err := os.WriteFile(s.controlFile(), data, 0o644); err != nil {
		applog.Logf("CtlServer: cannot write control.json: %v", err)
	}

	s.server = &http.Server{Handler: mux}
	go func() {
		applog.Logf("CtlServer: listening on port %d", s.port)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			applog.Logf("CtlServer: listener failed: %v", err)
		}
	}()
	return nil
}

// Stop closes the listener and removes control.json so the instance stops
// advertising itself.
func (s *Server) Stop() {
	_ = os.Remove(s.controlFile())
	if s.server != nil {
		_ = s.server.Close()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return false
	}
	return true
}

func (s *Server) mcpInfo() map[string]any {
	port := s.mcp.Port()
	if port == 0 {
		port = s.cfg.MCPPort()
	}
	return map[string]any{
		"running": s.mcp.Running(),
		"port":    port,
		"url":     fmt.Sprintf("http://127.0.0.1:%d/sse", port),
		"tools":   mcpserver.ToolNames(),
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"instance":  s.store.InstanceID(),
		"pid":       os.Getpid(),
		"root":      s.cfg.Root,
		"executing": s.runner.Running(),
		"session":   s.runner.CurrentSession(),
		"recording": s.runner.Recording(),
		"model":     s.cfg.Model(),
		"mcp":       s.mcpInfo(),
	})
}

func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mcpInfo())
}

func (s *Server) handleMCPStart(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.mcp.Start(s.cfg.MCPPort()); err != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("MCP server failed to start: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, s.mcpInfo())
}

func (s *Server) handleMCPStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.mcp.Stop()
	writeJSON(w, http.StatusOK, s.mcpInfo())
}

func (s *Server) handleRunInstruction(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var body struct {
		Instruction string `json:"instruction"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Instruction != "" {
		if err := s.cfg.WriteInstruction(body.Instruction); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot write instruction.txt: "+err.Error())
			return
		}
	}
	if !s.runner.RunInstruction() {
		writeError(w, http.StatusConflict, "a session is already running")
		return
	}
	applog.Log("CtlServer: instruction session started")
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

func (s *Server) handleRunMacro(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if !s.runner.RunMacro() {
		writeError(w, http.StatusConflict, "a session is already running")
		return
	}
	applog.Log("CtlServer: macro session started")
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}

func (s *Server) handleRunStop(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.runner.Stop()
	writeJSON(w, http.StatusOK, map[string]any{"stopped": true})
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if s.runner.Running() {
		writeError(w, http.StatusConflict, "a session is running — it owns the capture pipeline")
		return
	}
	s.br.FlashScreenshot()
	png, err := s.br.CaptureScreenshot(true, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "screenshot capture failed: "+err.Error())
		return
	}
	path := s.store.SaveUserScreenshot(png)
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

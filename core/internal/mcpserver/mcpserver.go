// Package mcpserver implements the MCP SSE transport (JSON-RPC over
// HTTP+SSE) on the configured port, replacing the hand-rolled Swift
// MCPServer. Endpoints:
//
//	GET  /sse                        — SSE stream; emits endpoint event, then JSON-RPC responses
//	POST /messages?sessionId=<uuid>  — client sends JSON-RPC requests here
package mcpserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
)

type Server struct {
	br *bridge.Bridge

	mu       sync.Mutex
	sessions map[string]chan []byte
}

func New(br *bridge.Bridge) *Server {
	return &Server{br: br, sessions: map[string]chan []byte{}}
}

// Start listens on the given port in a background goroutine.
func (s *Server) Start(port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/messages", s.handleMessages)

	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: withCORS(mux)}
	go func() {
		applog.Logf("MCPServer: listening on port %d", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applog.Logf("MCPServer: listener failed: %v", err)
		}
	}()
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := newSessionID()
	events := make(chan []byte, 16)
	s.mu.Lock()
	s.sessions[sessionID] = events
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case data := <-events:
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	sessionID := r.URL.Query().Get("sessionId")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Acknowledge immediately — the response arrives via SSE.
	w.WriteHeader(http.StatusAccepted)

	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		applog.Log("MCPServer: bad JSON in POST body")
		return
	}

	method, _ := msg["method"].(string)
	params, _ := msg["params"].(map[string]any)
	requestID, hasID := msg["id"]
	// Notifications have no id and need no response.
	if !hasID || requestID == nil {
		return
	}

	if method == "tools/call" {
		name, _ := params["name"].(string)
		applog.Logf("MCPServer: %s → %s", method, name)
	} else {
		applog.Logf("MCPServer: %s", method)
	}

	s.mu.Lock()
	events := s.sessions[sessionID]
	s.mu.Unlock()
	if events == nil {
		applog.Logf("MCPServer: no SSE session %s", sessionID)
		return
	}

	response := s.processRPC(method, requestID, params)
	data, err := json.Marshal(response)
	if err != nil {
		return
	}
	select {
	case events <- data:
	case <-time.After(5 * time.Second):
	}
}

func rpcResult(id any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcError(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}

func (s *Server) processRPC(method string, id any, params map[string]any) map[string]any {
	switch method {
	case "initialize":
		return rpcResult(id, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "pob", "version": "1.0.0"},
		})

	case "ping":
		return rpcResult(id, map[string]any{})

	case "tools/list":
		return rpcResult(id, map[string]any{
			"tools": []any{map[string]any{
				"name": "take_screenshot",
				"description": "Capture a screenshot of the Pob window and return it as a PNG image. " +
					"All crop parameters are optional. When all four are provided, only that region is " +
					"captured. Coordinates are in screen points (logical pixels), origin at top-left.",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"crop_x":      map[string]any{"type": "integer", "description": "Left edge in screen points."},
						"crop_y":      map[string]any{"type": "integer", "description": "Top edge in screen points."},
						"crop_width":  map[string]any{"type": "integer", "description": "Width in screen points."},
						"crop_height": map[string]any{"type": "integer", "description": "Height in screen points."},
					},
				},
			}},
		})

	case "tools/call":
		name, _ := params["name"].(string)
		arguments, _ := params["arguments"].(map[string]any)
		if name == "take_screenshot" {
			return s.takeScreenshot(id, arguments)
		}
		return rpcError(id, -32601, "Unknown tool: "+name)

	default:
		return rpcError(id, -32601, "Method not found: "+method)
	}
}

func (s *Server) takeScreenshot(id any, arguments map[string]any) map[string]any {
	var crop *bridge.CropRect
	x, okX := arguments["crop_x"].(float64)
	y, okY := arguments["crop_y"].(float64)
	cw, okW := arguments["crop_width"].(float64)
	ch, okH := arguments["crop_height"].(float64)
	if okX && okY && okW && okH {
		crop = &bridge.CropRect{X: x, Y: y, W: cw, H: ch}
	}

	png, err := s.br.CaptureScreenshot(false, crop)
	if err != nil {
		return rpcError(id, -32603, "Screenshot capture failed")
	}

	return rpcResult(id, map[string]any{
		"content": []any{map[string]any{
			"type":     "image",
			"data":     base64.StdEncoding.EncodeToString(png),
			"mimeType": "image/png",
		}},
	})
}

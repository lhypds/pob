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
	"net"
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
	server   *http.Server
	port     int
}

// DefaultPort is used when `pob mcp start` is not given an explicit port.
const DefaultPort = 8032

func New(br *bridge.Bridge) *Server {
	return &Server{br: br, sessions: map[string]chan []byte{}}
}

// Start binds the listener synchronously (so callers see port conflicts) and
// serves in a background goroutine. Starting an already-running server is a
// no-op.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/messages", s.handleMessages)

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		applog.Logf("MCPServer: listen failed: %v", err)
		return err
	}

	server := &http.Server{Handler: withCORS(mux)}
	s.server = server
	s.port = port
	go func() {
		applog.Logf("MCPServer: listening on port %d", port)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			applog.Logf("MCPServer: listener failed: %v", err)
		}
	}()
	return nil
}

// Stop shuts the listener down, dropping any connected SSE clients. Stopping
// a stopped server is a no-op.
func (s *Server) Stop() {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.mu.Unlock()
	if server == nil {
		return
	}
	// Close (not Shutdown): SSE streams are long-lived, so a graceful drain
	// would block until every client disconnects.
	_ = server.Close()
	applog.Log("MCPServer: stopped")
}

// Running reports whether the listener is up.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.server != nil
}

// Port returns the port the server was last started on.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// ToolNames lists the MCP tool names this server exposes.
func ToolNames() []string {
	var names []string
	for _, t := range toolDefinitions() {
		if m, ok := t.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
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
		return rpcResult(id, map[string]any{"tools": toolDefinitions()})

	case "tools/call":
		name, _ := params["name"].(string)
		arguments, _ := params["arguments"].(map[string]any)
		return s.callTool(id, name, arguments)

	default:
		return rpcError(id, -32601, "Method not found: "+method)
	}
}

func toolDefinitions() []any {
	tool := func(name, description string, properties map[string]any, required []string) map[string]any {
		if properties == nil {
			properties = map[string]any{}
		}
		schema := map[string]any{"type": "object", "properties": properties}
		if required != nil {
			schema["required"] = required
		}
		return map[string]any{"name": name, "description": description, "inputSchema": schema}
	}
	num := func(description string) map[string]any {
		return map[string]any{"type": "number", "description": description}
	}
	str := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}

	return []any{
		tool("take_screenshot",
			"Capture a screenshot of the Pob window and return it as a PNG image. "+
				"All crop parameters are optional. When all four are provided, only that region is "+
				"captured. Coordinates are in screenshot pixels, origin at top-left. Set with_cursor "+
				"to true to draw the virtual cursor into the image.",
			map[string]any{
				"crop_x":      map[string]any{"type": "integer", "description": "Left edge in screenshot pixels."},
				"crop_y":      map[string]any{"type": "integer", "description": "Top edge in screenshot pixels."},
				"crop_width":  map[string]any{"type": "integer", "description": "Width in screenshot pixels."},
				"crop_height": map[string]any{"type": "integer", "description": "Height in screenshot pixels."},
				"with_cursor": map[string]any{"type": "boolean", "description": "Draw the virtual cursor into the image. Default false."},
			}, nil),
		tool("reset_cursor",
			"Reset the virtual cursor to its home position and return the new position. "+
				"Use this to get to a known state before a sequence of relative moves.",
			nil, nil),
		tool("move_cursor",
			"Move the virtual cursor by a relative pixel offset in screenshot space "+
				"(origin = top-left, x increases right, y increases down) and return the new position. "+
				"Take a screenshot with with_cursor=true to see where the cursor is.",
			map[string]any{
				"dx": num("Horizontal offset in screenshot pixels. Positive = right, negative = left."),
				"dy": num("Vertical offset in screenshot pixels. Positive = down, negative = up."),
			}, []string{"dx", "dy"}),
		tool("click",
			"Left-click at the current virtual cursor position.",
			nil, nil),
		tool("right_click",
			"Right-click at the current virtual cursor position.",
			nil, nil),
		tool("double_click",
			"Double-click at the current virtual cursor position.",
			nil, nil),
		tool("drag",
			"Drag from the current virtual cursor position by (dx, dy) screenshot pixels. "+
				"The cursor ends at the new position.",
			map[string]any{
				"dx": num("Horizontal drag offset in screenshot pixels. Positive = right."),
				"dy": num("Vertical drag offset in screenshot pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		tool("scroll",
			"Scroll at the current virtual cursor position. dy > 0 = scroll down, dy < 0 = scroll up, "+
				"dx > 0 = scroll right.",
			map[string]any{
				"dx": num("Horizontal scroll amount in pixels."),
				"dy": num("Vertical scroll amount in pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		tool("type_text",
			"Type text at the current keyboard focus.",
			map[string]any{
				"text": str("The text to type."),
			}, []string{"text"}),
		tool("key_press",
			"Press a special key. Supported: return, tab, space, delete, escape, left, right, up, down, "+
				"home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.",
			map[string]any{
				"key": str("Key name, e.g. \"return\", \"escape\", \"cmd+v\"."),
			}, []string{"key"}),
	}
}

func (s *Server) callTool(id any, name string, arguments map[string]any) map[string]any {
	numArg := func(key string) float64 {
		v, _ := arguments[key].(float64)
		return v
	}
	position := func(pos bridge.Point, err error, action string) map[string]any {
		if err != nil {
			return rpcError(id, -32603, action+" failed: "+err.Error())
		}
		return textResult(id, fmt.Sprintf("%s. Cursor at (%d, %d).", action, pos.X, pos.Y))
	}

	switch name {
	case "take_screenshot":
		return s.takeScreenshot(id, arguments)

	case "reset_cursor":
		pos, err := s.br.ResetCursor()
		return position(pos, err, "Cursor reset")

	case "move_cursor":
		pos, err := s.br.MoveCursor(numArg("dx"), numArg("dy"))
		return position(pos, err, "Cursor moved")

	case "click":
		pos, err := s.br.Click()
		return position(pos, err, "Clicked")

	case "right_click":
		pos, err := s.br.RightClick()
		return position(pos, err, "Right-clicked")

	case "double_click":
		pos, err := s.br.DoubleClick()
		return position(pos, err, "Double-clicked")

	case "drag":
		pos, err := s.br.Drag(numArg("dx"), numArg("dy"))
		return position(pos, err, "Dragged")

	case "scroll":
		pos, err := s.br.Scroll(int(numArg("dx")), int(numArg("dy")))
		return position(pos, err, "Scrolled")

	case "type_text":
		text, _ := arguments["text"].(string)
		if err := s.br.TypeText(text); err != nil {
			return rpcError(id, -32603, "Type failed: "+err.Error())
		}
		return textResult(id, fmt.Sprintf("Typed %d characters.", len([]rune(text))))

	case "key_press":
		key, _ := arguments["key"].(string)
		if err := s.br.KeyPress(key); err != nil {
			return rpcError(id, -32603, "Key press failed: "+err.Error())
		}
		return textResult(id, "Pressed "+key+".")

	default:
		return rpcError(id, -32601, "Unknown tool: "+name)
	}
}

func textResult(id any, text string) map[string]any {
	return rpcResult(id, map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
	})
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

	withCursor, _ := arguments["with_cursor"].(bool)
	png, err := s.br.CaptureScreenshot(withCursor, crop)
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

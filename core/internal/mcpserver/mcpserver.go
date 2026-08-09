// Package mcpserver implements the MCP SSE transport (JSON-RPC over
// HTTP+SSE) on the configured port, replacing the hand-rolled Swift
// MCPServer. Endpoints:
//
//	GET  /sse                        — SSE stream; emits endpoint event, then JSON-RPC responses
//	POST /messages?sessionId=<uuid>  — client sends JSON-RPC requests here
package mcpserver

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pob/core/internal/applog"
	"pob/core/internal/bridge"
	// For the machine's own addresses only — the Pob server answers the same
	// question for the page it serves, and both mean the same list.
	"pob/server"
)

type Server struct {
	br  *bridge.Bridge
	rec MacroRecorder

	mu       sync.Mutex
	sessions map[string]chan []byte
	server   *http.Server
	host     string
	port     int
}

// MacroRecorder is the macro.psl sink. An action driven over MCP is an action
// the machine performed, so a recording that is running captures it the same
// as one the agent loop performed: same file, same grammar, one stream in the
// order things happened.
type MacroRecorder interface {
	Recording() bool
	AppendToMacro(line string)
}

// DefaultPort is used when `pob mcp start` is not given an explicit port.
const DefaultPort = 8032

// DefaultHost is the interface bound when settings name none: every one of
// them, so a client on another machine reaches it without anything being
// configured first. Loopback keeps working — a wildcard bind holds 127.0.0.1
// along with every other address — so a client pointed at localhost is not
// affected by this either way.
//
// These tools move the machine's pointer and type on its keyboard, and take no
// credentials, which reads like a reason to bind loopback instead. It isn't
// one: the Pob server has bound every interface since it existed, and its
// Operation API types and clicks on the same machine. Closing
// this port while that one is open protects nothing — so the choice is made
// once, for the machine, by putting it on a network you trust or by setting
// `mcp_host` to "127.0.0.1" (and `"server": false`) on one you don't.
const DefaultHost = "0.0.0.0"

// loopbackHost is the address a client on this machine is given. Named apart
// from DefaultHost because they answer different questions — what to bind, and
// what to tell a local client — and a wildcard bind is not an address anyone
// can dial.
const loopbackHost = "127.0.0.1"

func New(br *bridge.Bridge) *Server {
	return &Server{br: br, sessions: map[string]chan []byte{}}
}

// SetRecorder attaches the macro sink. Called once before Start; a server
// without one simply records nothing.
func (s *Server) SetRecorder(rec MacroRecorder) { s.rec = rec }

func (s *Server) recording() bool { return s.rec != nil && s.rec.Recording() }

func (s *Server) record(format string, args ...any) {
	if s.recording() {
		s.rec.AppendToMacro(fmt.Sprintf(format, args...))
	}
}

// originForMove reads where the cursor is before an absolute move, so the move
// can be written down as the relative move(dx, dy) that replay understands.
// The origin is read back from the shell every time rather than remembered:
// the cursor is moved by the agent loop, the `pob` CLI and the user's own hand
// too, and a delta measured from a stale origin would send replay somewhere
// else entirely. Off the recording path this costs nothing — it does not run.
func (s *Server) originForMove() (bridge.Point, bool) {
	if !s.recording() {
		return bridge.Point{}, false
	}
	pos, err := s.br.CursorPosition()
	return pos, err == nil
}

// Start binds the listener synchronously (so callers see port conflicts) and
// serves in a background goroutine. Starting a server that is already up on
// the same host and port is a no-op; asked for another one it moves, since the
// address that was asked for is the address a client is about to be handed —
// and with the server starting with the instance, `pob mcp start <port>` always
// arrives at one that is already running. An empty host means DefaultHost —
// a settings file with nothing to say about the interface gets the default one.
func (s *Server) Start(host string, port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if host == "" {
		host = DefaultHost
	}
	if s.server != nil && s.port == port && s.host == host {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", s.handleSSE)
	mux.HandleFunc("/messages", s.handleMessages)

	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		applog.Logf("MCPServer: listen failed: %v", err)
		return err
	}
	// The old listener goes only once the new port is held, so a move to a port
	// something else already has leaves the server where it was, still
	// answering the client that has its address.
	s.stopLocked()

	server := &http.Server{Handler: withCORS(mux)}
	s.server = server
	s.host = host
	// The bound port, not the requested one: port 0 means "any free port", and
	// the address handed to a client has to be the one it can reach.
	s.port = listener.Addr().(*net.TCPAddr).Port
	bound := s.port
	go func() {
		applog.Logf("MCPServer: listening on %s", net.JoinHostPort(host, strconv.Itoa(bound)))
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
	defer s.mu.Unlock()
	s.stopLocked()
}

// stopLocked closes the listener with s.mu held. Closing does not wait for the
// SSE handlers, so each drops its own session as it wakes — which is also what
// releases the virtual cursor.
func (s *Server) stopLocked() {
	server := s.server
	s.server = nil
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

// Port returns the port the listener is bound to, or 0 before the first start.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// Host returns the interface the listener is bound to, or "" before the first
// start. It is the one fact that decides whether a client on another machine
// can connect at all, so everything that reports on this server reports it.
func (s *Server) Host() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host
}

// wildcardHosts are the bind addresses that mean "every interface". One of
// them names no single address, so it is not an address to hand a client:
// what to report there is where the machine can actually be dialled.
var wildcardHosts = map[string]bool{"": true, "0.0.0.0": true, "::": true, "[::]": true}

// URLsFor is every address an MCP client can reach a server bound to
// host:port at.
//
// Loopback comes first, and is the whole list for the default bind. A wildcard
// bind adds one URL per network the machine is on: those are what a client on
// another machine is given, and printing them is also how someone who has just
// set `mcp_host` sees that it took — a status line that says 127.0.0.1 while
// the listener is open to the network, or the reverse, is the one thing a
// client that will not connect needs to be told.
func URLsFor(host string, port int) []string {
	sse := func(host string) string {
		return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/sse"
	}
	if !wildcardHosts[host] {
		return []string{sse(host)}
	}
	urls := []string{sse(loopbackHost)}
	for _, ip := range server.Addresses() {
		// Addresses falls back to loopback on a machine that is on no network,
		// which is already the first entry.
		if ip.IsLoopback() {
			continue
		}
		urls = append(urls, sse(ip.String()))
	}
	return urls
}

// URLs is every address this server can be reached at, or nil when it is not
// running.
func (s *Server) URLs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	return URLsFor(s.host, s.port)
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
	first := len(s.sessions) == 1
	s.mu.Unlock()
	// Show the virtual cursor for as long as a client is connected — it can
	// move the cursor at any time, and an invisible cursor makes those moves
	// look like nothing happened. Connected, not merely listening: the server
	// is up from the moment the instance starts, and a cursor that is always
	// on says nothing about anything.
	if first {
		s.br.NotifyRemoteControl("mcp", true)
	}
	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		last := len(s.sessions) == 0
		s.mu.Unlock()
		// Only once the last client has gone: a second one still holding the
		// cursor must not have it taken away.
		if last {
			s.br.NotifyRemoteControl("mcp", false)
		}
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

	// Absolute coordinates are screenshot pixels as reported by take_screenshot,
	// which is also what the position line of every result refers to.
	absX := num("Target x in screenshot pixels, measured from the left edge.")
	absY := num("Target y in screenshot pixels, measured from the top edge.")
	xy := func() map[string]any { return map[string]any{"x": absX, "y": absY} }

	return []any{
		tool("take_screenshot",
			"Capture a screenshot of the Pob window and return it as a PNG image, plus a text line "+
				"giving the image size. Start here: the returned pixel dimensions are the coordinate "+
				"space every other tool uses. All crop parameters are optional. When all four are "+
				"provided, only that region is captured — note that crops shift the origin, so read "+
				"coordinates for later clicks off an uncropped shot. Coordinates are in screenshot "+
				"pixels, origin at top-left. Set with_cursor to true to draw the virtual cursor into "+
				"the image.",
			map[string]any{
				"crop_x":      map[string]any{"type": "integer", "description": "Left edge in screenshot pixels."},
				"crop_y":      map[string]any{"type": "integer", "description": "Top edge in screenshot pixels."},
				"crop_width":  map[string]any{"type": "integer", "description": "Width in screenshot pixels."},
				"crop_height": map[string]any{"type": "integer", "description": "Height in screenshot pixels."},
				"with_cursor": map[string]any{"type": "boolean", "description": "Draw the virtual cursor into the image. Default false."},
			}, nil),
		tool("get_cursor_position",
			"Return the current virtual cursor position in screenshot pixels without moving or "+
				"clicking anything.",
			nil, nil),
		tool("reset_cursor",
			"Reset the virtual cursor to its home position and return the new position. "+
				"Use this to get to a known state before a sequence of relative moves.",
			nil, nil),
		tool("move_cursor",
			"Move the virtual cursor by a relative pixel offset in screenshot space "+
				"(origin = top-left, x increases right, y increases down) and return the new position. "+
				"Use this to nudge an already-close cursor; prefer move_cursor_to when you can read the "+
				"target off a screenshot.",
			map[string]any{
				"dx": num("Horizontal offset in screenshot pixels. Positive = right, negative = left."),
				"dy": num("Vertical offset in screenshot pixels. Positive = down, negative = up."),
			}, []string{"dx", "dy"}),
		tool("move_cursor_to",
			"Move the virtual cursor to an absolute position in screenshot pixels and return the new "+
				"position. This is the reliable way to aim: read the target's coordinates off a "+
				"screenshot and pass them directly.",
			xy(), []string{"x", "y"}),
		tool("click",
			"Left-click at the current virtual cursor position.",
			nil, nil),
		tool("right_click",
			"Right-click at the current virtual cursor position.",
			nil, nil),
		tool("double_click",
			"Double-click at the current virtual cursor position.",
			nil, nil),
		tool("move_and_click",
			"Move the virtual cursor to an absolute screenshot-pixel position and left-click there, in "+
				"one step. Preferred over move_cursor_to + click for clicking something you located in a "+
				"screenshot.",
			xy(), []string{"x", "y"}),
		tool("move_and_right_click",
			"Move the virtual cursor to an absolute screenshot-pixel position and right-click there, in "+
				"one step — e.g. to open a context menu.",
			xy(), []string{"x", "y"}),
		tool("move_and_double_click",
			"Move the virtual cursor to an absolute screenshot-pixel position and double-click there, in "+
				"one step — e.g. to open an item or select a word.",
			xy(), []string{"x", "y"}),
		tool("drag",
			"Drag from the current virtual cursor position by (dx, dy) screenshot pixels. "+
				"The cursor ends at the new position.",
			map[string]any{
				"dx": num("Horizontal drag offset in screenshot pixels. Positive = right."),
				"dy": num("Vertical drag offset in screenshot pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		tool("drag_to",
			"Drag from the current virtual cursor position to an absolute screenshot-pixel position. "+
				"Place the cursor on the drag source first (move_cursor_to), then call this with the "+
				"drop target.",
			xy(), []string{"x", "y"}),
		tool("scroll",
			"Scroll at the current virtual cursor position. dy > 0 = scroll down, dy < 0 = scroll up, "+
				"dx > 0 = scroll right. The scroll lands on whatever is under the cursor, so aim at the "+
				"pane you mean to scroll.",
			map[string]any{
				"dx": num("Horizontal scroll amount in pixels."),
				"dy": num("Vertical scroll amount in pixels. Positive = down."),
			}, []string{"dx", "dy"}),
		tool("move_and_scroll",
			"Move the virtual cursor to an absolute screenshot-pixel position and scroll there, in one "+
				"step. Use this to scroll a specific pane without disturbing the rest of the window.",
			map[string]any{
				"x":  absX,
				"y":  absY,
				"dx": num("Horizontal scroll amount in pixels."),
				"dy": num("Vertical scroll amount in pixels. Positive = down."),
			}, []string{"x", "y", "dx", "dy"}),
		tool("type_text",
			"Type text at the current keyboard focus. Click the target field first — this types wherever "+
				"focus already is.",
			map[string]any{
				"text": str("The text to type."),
			}, []string{"text"}),
		tool("key_press",
			"Press a special key or shortcut. A key may be preceded by \"+\"-joined modifiers: "+
				"cmd (Command on macOS, Ctrl elsewhere — use this for ordinary shortcuts), ctrl, alt, "+
				"shift, gui (the key beside the space bar: Command / Windows / Super). Keys: a–z, 0–9, "+
				"return, tab, space, backspace, forwarddelete, escape, insert, left, right, up, down, "+
				"home, end, pageup, pagedown, capslock, printscreen, scrolllock, pause, menu, f1–f24, "+
				"minus, equals, leftbracket, rightbracket, backslash, semicolon, quote, grave, comma, "+
				"period, slash.",
			map[string]any{
				"key": str("Key name, e.g. \"return\", \"escape\", \"cmd+v\", \"ctrl+shift+t\"."),
			}, []string{"key"}),
		tool("wait",
			"Pause before the next action, to let the UI settle after a click or a page load. "+
				"Capped at 10000 ms.",
			map[string]any{
				"milliseconds": num("How long to wait, in milliseconds. Capped at 10000."),
			}, []string{"milliseconds"}),
	}
}

// maxWaitMillis bounds the `wait` tool so a bad argument cannot stall the
// caller's request for minutes.
const maxWaitMillis = 10000

// numArg reads a JSON number argument. Models routinely send numbers as
// strings, so those are accepted too; ok is false when the argument is missing
// or is neither.
func numArg(arguments map[string]any, key string) (float64, bool) {
	switch v := arguments[key].(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func (s *Server) callTool(id any, name string, arguments map[string]any) map[string]any {
	// Optional numeric argument: absent reads as 0, matching the old behaviour
	// for the relative-offset tools.
	opt := func(key string) float64 {
		v, _ := numArg(arguments, key)
		return v
	}
	position := func(pos bridge.Point, err error, action string) map[string]any {
		if err != nil {
			return rpcError(id, -32603, action+" failed: "+err.Error())
		}
		return textResult(id, fmt.Sprintf("%s. Cursor at (%d, %d).", action, pos.X, pos.Y))
	}
	// target reads the required absolute (x, y) pair. Defaulting a missing
	// coordinate to 0 would silently aim at the top-left corner, so this
	// reports the bad call instead.
	target := func() (float64, float64, map[string]any) {
		x, okX := numArg(arguments, "x")
		y, okY := numArg(arguments, "y")
		if !okX || !okY {
			return 0, 0, rpcError(id, -32602, "Tool "+name+" requires numeric x and y in screenshot pixels")
		}
		return x, y, nil
	}

	switch name {
	case "take_screenshot":
		return s.takeScreenshot(id, arguments)

	case "get_cursor_position":
		pos, err := s.br.CursorPosition()
		if err != nil {
			return rpcError(id, -32603, "Cursor query failed: "+err.Error())
		}
		return textResult(id, fmt.Sprintf("Cursor at (%d, %d).", pos.X, pos.Y))

	case "reset_cursor":
		pos, err := s.br.ResetCursor()
		if err == nil {
			s.record("resetCursor()")
		}
		return position(pos, err, "Cursor reset")

	case "move_cursor":
		pos, err := s.br.MoveCursor(opt("dx"), opt("dy"))
		if err == nil {
			s.record("move(%d, %d)", int(opt("dx")), int(opt("dy")))
		}
		return position(pos, err, "Cursor moved")

	case "move_cursor_to":
		x, y, bad := target()
		if bad != nil {
			return bad
		}
		from, tracked := s.originForMove()
		pos, err := s.br.MoveCursorTo(x, y)
		if err == nil && tracked {
			s.record("move(%d, %d)", pos.X-from.X, pos.Y-from.Y)
		}
		return position(pos, err, "Cursor moved")

	case "click":
		pos, err := s.br.Click()
		if err == nil {
			s.record("click()")
		}
		return position(pos, err, "Clicked")

	case "right_click":
		pos, err := s.br.RightClick()
		if err == nil {
			s.record("rightClick()")
		}
		return position(pos, err, "Right-clicked")

	case "double_click":
		pos, err := s.br.DoubleClick()
		if err == nil {
			s.record("doubleClick()")
		}
		return position(pos, err, "Double-clicked")

	case "move_and_click", "move_and_right_click", "move_and_double_click":
		x, y, bad := target()
		if bad != nil {
			return bad
		}
		action, do, macro := "Moved and clicked", s.br.Click, "click()"
		switch name {
		case "move_and_right_click":
			action, do, macro = "Moved and right-clicked", s.br.RightClick, "rightClick()"
		case "move_and_double_click":
			action, do, macro = "Moved and double-clicked", s.br.DoubleClick, "doubleClick()"
		}
		from, tracked := s.originForMove()
		moved, err := s.br.MoveCursorTo(x, y)
		if err != nil {
			return rpcError(id, -32603, action+" failed: "+err.Error())
		}
		if tracked {
			s.record("move(%d, %d)", moved.X-from.X, moved.Y-from.Y)
		}
		pos, err := do()
		if err == nil {
			s.record("%s", macro)
		}
		return position(pos, err, action)

	case "drag":
		pos, err := s.br.Drag(opt("dx"), opt("dy"))
		if err == nil {
			s.record("drag(%d, %d)", int(opt("dx")), int(opt("dy")))
		}
		return position(pos, err, "Dragged")

	case "drag_to":
		x, y, bad := target()
		if bad != nil {
			return bad
		}
		from, tracked := s.originForMove()
		pos, err := s.br.DragTo(x, y)
		if err == nil && tracked {
			s.record("drag(%d, %d)", pos.X-from.X, pos.Y-from.Y)
		}
		return position(pos, err, "Dragged")

	case "scroll":
		pos, err := s.br.Scroll(int(opt("dx")), int(opt("dy")))
		if err == nil {
			s.record("scroll(%d, %d)", int(opt("dx")), int(opt("dy")))
		}
		return position(pos, err, "Scrolled")

	case "move_and_scroll":
		x, y, bad := target()
		if bad != nil {
			return bad
		}
		from, tracked := s.originForMove()
		moved, err := s.br.MoveCursorTo(x, y)
		if err != nil {
			return rpcError(id, -32603, "Moved and scrolled failed: "+err.Error())
		}
		if tracked {
			s.record("move(%d, %d)", moved.X-from.X, moved.Y-from.Y)
		}
		pos, err := s.br.Scroll(int(opt("dx")), int(opt("dy")))
		if err == nil {
			s.record("scroll(%d, %d)", int(opt("dx")), int(opt("dy")))
		}
		return position(pos, err, "Moved and scrolled")

	case "type_text":
		text, _ := arguments["text"].(string)
		if err := s.br.TypeText(text); err != nil {
			return rpcError(id, -32603, "Type failed: "+err.Error())
		}
		s.record("typeText(%q)", text)
		return textResult(id, fmt.Sprintf("Typed %d characters.", len([]rune(text))))

	case "key_press":
		key, _ := arguments["key"].(string)
		if err := s.br.KeyPress(key); err != nil {
			return rpcError(id, -32603, "Key press failed: "+err.Error())
		}
		s.record("keyPress(%q)", key)
		return textResult(id, "Pressed "+key+".")

	case "wait":
		ms, ok := numArg(arguments, "milliseconds")
		if !ok || ms < 0 {
			return rpcError(id, -32602, "wait requires a non-negative milliseconds value")
		}
		if ms > maxWaitMillis {
			ms = maxWaitMillis
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		s.record("sleep(%d)", int(ms))
		return textResult(id, fmt.Sprintf("Waited %d ms.", int(ms)))

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
	shot, err := s.br.CaptureScreenshot(withCursor, crop)
	if err != nil {
		return rpcError(id, -32603, "Screenshot capture failed")
	}
	if crop != nil {
		s.record("take_screenshot(%d, %d, %d, %d)", int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
	} else {
		s.record("take_screenshot()")
	}

	content := []any{map[string]any{
		"type":     "image",
		"data":     base64.StdEncoding.EncodeToString(shot),
		"mimeType": "image/png",
	}}
	// The absolute-position tools address this image's pixel grid, so state its
	// size — and, for a crop, the offset those coordinates need.
	if cfg, err := png.DecodeConfig(bytes.NewReader(shot)); err == nil {
		note := fmt.Sprintf("Screenshot is %d×%d pixels; coordinates for move_cursor_to / move_and_click "+
			"are measured from its top-left corner.", cfg.Width, cfg.Height)
		if crop != nil {
			note += fmt.Sprintf(" This is a crop taken at (%d, %d), so add that offset to any coordinate "+
				"read off this image before clicking.", int(crop.X), int(crop.Y))
		}
		content = append(content, map[string]any{"type": "text", "text": note})
	}

	return rpcResult(id, map[string]any{"content": content})
}

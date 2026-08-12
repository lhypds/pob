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
	"math"
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

	// The picture last handed to a client, and what its pixels are worth —
	// see viewMaxEdge and scale().
	viewMu    sync.Mutex
	viewScale float64
	viewSrcW  int
	viewSrcH  int
}

// viewMaxEdge caps the longest edge of the picture take_screenshot hands back.
//
// Coordinates are only useful if the grid they are read off is the grid they are
// sent back in, and a client cannot keep that promise for a picture this big: an
// LLM host shrinks an image to fit its model's limit before the model ever sees
// it — around 1568 pixels for Anthropic's API — so a window captured at 3288
// across arrives at less than half size. Nothing says so in the picture, and a
// coordinate read straight off it lands at less than half the distance from the
// corner. Whether it hits anything is luck.
//
// So the shrinking is done here, to a size no host has reason to touch, and the
// picture that arrives is the picture that was sent. What a client reads off it
// is what it sends back, and scale() puts it back into the window's own pixels.
// The cap is on the longest edge rather than the width because a host measures
// the same way — a tall narrow window capped by its width would still be shrunk
// on the way in.
const viewMaxEdge = 1500

// scale is how many window pixels one pixel of the last picture is worth. 1
// until a screenshot has established otherwise, which is also what an older
// shell — one that reports no sizes and does no shrinking — leaves it at, so
// coordinates there mean what they always did.
func (s *Server) scale() float64 {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	if s.viewScale <= 0 {
		return 1
	}
	return s.viewScale
}

// setView records the mapping a fresh uncropped capture establishes. Only an
// uncropped one may: a crop reports its own size as the source, which says
// nothing about the window it came from.
func (s *Server) setView(scale float64, srcW, srcH int) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	s.viewScale = scale
	s.viewSrcW = srcW
	s.viewSrcH = srcH
}

// windowSize is the last known size of the window in its own pixels, or zeroes
// before the first capture.
func (s *Server) windowSize() (int, int) {
	s.viewMu.Lock()
	defer s.viewMu.Unlock()
	return s.viewSrcW, s.viewSrcH
}

// toWindow takes a coordinate the client read off the picture into the window's
// pixel space, which is what the shell and a recorded macro speak.
func (s *Server) toWindow(v float64) float64 { return v * s.scale() }

// toView is the way back, for the position every result quotes: a client that
// is handed a number it could not have read off its own picture cannot use it
// for the next call.
func (s *Server) toView(v int) int { return int(math.Round(float64(v) / s.scale())) }

// fitWidth is the width to ask the shell to shrink to so that neither edge of
// the result passes viewMaxEdge. Zero — leave it alone — when it already fits,
// or when the size is not known yet.
func fitWidth(srcW, srcH int) int {
	long := max(srcW, srcH)
	if srcW <= 0 || srcH <= 0 || long <= viewMaxEdge {
		return 0
	}
	return max(1, int(math.Round(float64(srcW)*float64(viewMaxEdge)/float64(long))))
}

// MacroRecorder is the main macro's sink. An action driven over MCP is an action
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
		applog.Errorf("MCPServer: listen failed: %v", err)
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
			applog.Errorf("MCPServer: listener failed: %v", err)
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

	// Absolute coordinates are pixels of the picture take_screenshot handed back,
	// which is also the space the position line of every result is quoted in.
	absX := num("Target x, measured from the left edge of the last screenshot as you see it.")
	absY := num("Target y, measured from the top edge of the last screenshot as you see it.")
	xy := func() map[string]any { return map[string]any{"x": absX, "y": absY} }
	// The same pair where a tool takes it or does without it. Both or neither:
	// one coordinate alone would aim at a position half of which was guessed,
	// so it is refused rather than filled in.
	optionalXY := func() map[string]any {
		return map[string]any{
			"x": num("Optional target x, measured from the left edge of the last screenshot. Give both x and y, or neither."),
			"y": num("Optional target y, measured from the top edge of the last screenshot. Give both x and y, or neither."),
		}
	}

	return []any{
		tool("take_screenshot",
			"Capture a screenshot of the Pob window and return it as a PNG image, plus a text line "+
				"giving the image size. Start here: this image's own pixel grid is the coordinate "+
				"space every other tool uses, so a position read straight off it is a position you "+
				"can click — read it off the image as you see it and pass those numbers back. All "+
				"crop parameters are optional. When all four are provided, only that region is "+
				"captured; a crop comes back at the same scale as the whole view, so a coordinate "+
				"read off it plus the crop's own offset is a coordinate you can click. Origin is "+
				"top-left. Set with_cursor to true to draw the virtual cursor into the image.",
			map[string]any{
				"crop_x":      map[string]any{"type": "integer", "description": "Left edge, in the pixels of the last screenshot."},
				"crop_y":      map[string]any{"type": "integer", "description": "Top edge, in the pixels of the last screenshot."},
				"crop_width":  map[string]any{"type": "integer", "description": "Width, in the pixels of the last screenshot."},
				"crop_height": map[string]any{"type": "integer", "description": "Height, in the pixels of the last screenshot."},
				"with_cursor": map[string]any{"type": "boolean", "description": "Draw the virtual cursor into the image. Default false."},
			}, nil),
		tool("get_cursor_position",
			"Return the current virtual cursor position, in the pixels of the last screenshot, "+
				"without moving or clicking anything.",
			nil, nil),
		tool("reset_cursor",
			"Reset the virtual cursor to its home position and return the new position. "+
				"Use this to get to a known state before a sequence of relative moves.",
			nil, nil),
		tool("move",
			"Move the virtual cursor by a relative offset, measured across the last screenshot "+
				"(x increases right, y increases down), and return the new position. "+
				"Use this to nudge an already-close cursor; prefer move_to when you can read the "+
				"target off a screenshot.",
			map[string]any{
				"dx": num("Horizontal offset, measured across the last screenshot. Positive = right, negative = left."),
				"dy": num("Vertical offset, measured across the last screenshot. Positive = down, negative = up."),
			}, []string{"dx", "dy"}),
		tool("move_to",
			"Move the virtual cursor to an absolute position in the last screenshot's pixels and "+
				"return the new position. This is the reliable way to aim: read the target's "+
				"coordinates off the screenshot and pass them directly.",
			xy(), []string{"x", "y"}),
		tool("click",
			"Left-click. With x and y, move the virtual cursor to that position in the last "+
				"screenshot and click there, in one step — the preferred way to click something you "+
				"located in a screenshot. With neither, click where the cursor already is.",
			optionalXY(), nil),
		tool("right_click",
			"Right-click, at x, y when given and at the current virtual cursor position otherwise — "+
				"e.g. to open a context menu on something you located in a screenshot.",
			optionalXY(), nil),
		tool("double_click",
			"Double-click, at x, y when given and at the current virtual cursor position otherwise — "+
				"e.g. to open an item or select a word.",
			optionalXY(), nil),
		tool("drag",
			"Drag from the current virtual cursor position by (dx, dy), measured across the last "+
				"screenshot. The cursor ends at the new position.",
			map[string]any{
				"dx": num("Horizontal drag offset, measured across the last screenshot. Positive = right."),
				"dy": num("Vertical drag offset, measured across the last screenshot. Positive = down."),
			}, []string{"dx", "dy"}),
		tool("drag_to",
			"Drag from the current virtual cursor position to an absolute position in the last "+
				"screenshot's pixels. Place the cursor on the drag source first (move_to), then call "+
				"this with the drop target.",
			xy(), []string{"x", "y"}),
		tool("scroll",
			"Scroll at the current virtual cursor position. dy > 0 = scroll down, dy < 0 = scroll up, "+
				"dx > 0 = scroll right. The scroll lands on whatever is under the cursor, so put the "+
				"cursor on the pane you mean to scroll first (move_to).",
			map[string]any{
				"dx": num("Horizontal scroll amount, measured across the last screenshot."),
				"dy": num("Vertical scroll amount, measured across the last screenshot. Positive = down."),
			}, []string{"dx", "dy"}),
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
				"return (alias enter), tab, space, backspace (alias delete), forwarddelete, "+
				"escape (alias esc), insert, left, right, up, down, "+
				"home, end, pageup, pagedown, capslock, printscreen, scrolllock, pause, menu, f1–f24, "+
				"minus, equals, leftbracket, rightbracket, backslash, semicolon, quote, grave, comma, "+
				"period, slash.",
			map[string]any{
				"key": str("Key name, e.g. \"return\", \"escape\", \"cmd+v\", \"ctrl+shift+t\"."),
			}, []string{"key"}),
		tool("sleep",
			"Pause before the next action, to let the UI settle after a click or a page load. "+
				"Capped at 10 seconds.",
			map[string]any{
				"seconds": num("How long to sleep, in seconds. Fractions are fine — 0.25 is a quarter of a second. Capped at 10."),
			}, []string{"seconds"}),
	}
}

// maxSleepSeconds bounds the `sleep` tool so a bad argument cannot stall the
// caller's request for minutes.
const maxSleepSeconds = 10

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
	// Relative offsets are read off the picture the same as absolute ones — the
	// distance between two things in it — so they are scaled the same way.
	optWindow := func(key string) float64 { return s.toWindow(opt(key)) }
	position := func(pos bridge.Point, err error, action string) map[string]any {
		if err != nil {
			return rpcError(id, -32603, action+" failed: "+err.Error())
		}
		// Quoted in the picture's pixels: a position the client could not have
		// read off its own screenshot is not one it can use in the next call.
		return textResult(id, fmt.Sprintf("%s. Cursor at (%d, %d).", action, s.toView(pos.X), s.toView(pos.Y)))
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
		// Into the window's own pixels, which is what the shell aims with.
		return s.toWindow(x), s.toWindow(y), nil
	}
	// clickTarget reads the (x, y) a click may be aimed at. It is offered rather
	// than required — a click with neither goes where the cursor already is — but
	// it is both coordinates or neither: one alone would aim at a position half
	// of which was guessed, and target says so rather than defaulting the other
	// to the top-left corner.
	clickTarget := func() (x, y float64, aimed bool, bad map[string]any) {
		_, okX := numArg(arguments, "x")
		_, okY := numArg(arguments, "y")
		if !okX && !okY {
			return 0, 0, false, nil
		}
		x, y, bad = target()
		return x, y, bad == nil, bad
	}

	switch name {
	case "take_screenshot":
		return s.takeScreenshot(id, arguments)

	case "get_cursor_position":
		pos, err := s.br.CursorPosition()
		if err != nil {
			return rpcError(id, -32603, "Cursor query failed: "+err.Error())
		}
		return textResult(id, fmt.Sprintf("Cursor at (%d, %d).", s.toView(pos.X), s.toView(pos.Y)))

	case "reset_cursor":
		pos, err := s.br.ResetCursor()
		if err == nil {
			s.record("resetCursor()")
		}
		return position(pos, err, "Cursor reset")

	case "move":
		dx, dy := optWindow("dx"), optWindow("dy")
		pos, err := s.br.MoveCursor(dx, dy)
		if err == nil {
			// The window's pixels, not the picture's: replay moves the cursor
			// itself, and knows nothing about how big a picture some client was
			// once shown.
			s.record("move(%d, %d)", int(dx), int(dy))
		}
		return position(pos, err, "Cursor moved")

	case "move_to":
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

	// One click either way, told apart by whether it was handed a target: with
	// one the cursor goes there and the button goes down where it landed,
	// without one it goes down where the cursor already is. There is no separate
	// move-and-click tool, because that is this one with x and y.
	case "click", "right_click", "double_click":
		do, macro := s.br.Click, "click()"
		here, there := "Clicked", "Moved and clicked"
		switch name {
		case "right_click":
			do, macro = s.br.RightClick, "rightClick()"
			here, there = "Right-clicked", "Moved and right-clicked"
		case "double_click":
			do, macro = s.br.DoubleClick, "doubleClick()"
			here, there = "Double-clicked", "Moved and double-clicked"
		}
		x, y, aimed, bad := clickTarget()
		if bad != nil {
			return bad
		}
		action := here
		if aimed {
			action = there
			from, tracked := s.originForMove()
			moved, err := s.br.MoveCursorTo(x, y)
			if err != nil {
				return rpcError(id, -32603, action+" failed: "+err.Error())
			}
			if tracked {
				s.record("move(%d, %d)", moved.X-from.X, moved.Y-from.Y)
			}
		}
		pos, err := do()
		if err == nil {
			s.record("%s", macro)
		}
		return position(pos, err, action)

	case "drag":
		dx, dy := optWindow("dx"), optWindow("dy")
		pos, err := s.br.Drag(dx, dy)
		if err == nil {
			s.record("drag(%d, %d)", int(dx), int(dy))
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
		// A scroll is a distance across the picture too — "past this much of
		// what I can see" — so it travels the same way as a move.
		dx, dy := int(optWindow("dx")), int(optWindow("dy"))
		pos, err := s.br.Scroll(dx, dy)
		if err == nil {
			s.record("scroll(%d, %d)", dx, dy)
		}
		return position(pos, err, "Scrolled")

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

	case "sleep":
		sec, ok := numArg(arguments, "seconds")
		if !ok || sec < 0 {
			return rpcError(id, -32602, "sleep requires a non-negative seconds value")
		}
		if sec > maxSleepSeconds {
			sec = maxSleepSeconds
		}
		time.Sleep(time.Duration(sec * float64(time.Second)))
		// The macro statement takes a time, and the unit this was asked in is the
		// unit it is written down in: sleep(0.25s) replays the pause that
		// happened rather than a rounding of it. Formatted without an exponent,
		// since 1e-07s is not a time PSL can read back.
		written := strconv.FormatFloat(sec, 'f', -1, 64) + "s"
		s.record("sleep(%s)", written)
		return textResult(id, fmt.Sprintf("Slept %s.", written))

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
	scale := s.scale()
	var crop *bridge.CropRect
	x, okX := arguments["crop_x"].(float64)
	y, okY := arguments["crop_y"].(float64)
	cw, okW := arguments["crop_width"].(float64)
	ch, okH := arguments["crop_height"].(float64)
	cropped := okX && okY && okW && okH
	if cropped {
		// The crop is asked for in the picture's pixels, like every other
		// coordinate, and cut in the window's.
		crop = &bridge.CropRect{X: s.toWindow(x), Y: s.toWindow(y), W: s.toWindow(cw), H: s.toWindow(ch)}
	}

	withCursor, _ := arguments["with_cursor"].(bool)
	opts := bridge.ShotOptions{WithCursor: withCursor, Crop: crop}
	if cropped {
		// A crop comes back at the same scale as the whole view, so that a pixel
		// means the same thing in both and a coordinate read off the crop is the
		// crop's own offset plus what was read. Shrinking it to the cap instead
		// would make every crop its own private grid.
		opts.MaxWidth = int(math.Round(crop.W / scale))
	} else if srcW, srcH := s.windowSize(); srcW > 0 {
		opts.MaxWidth = fitWidth(srcW, srcH)
	} else {
		// Nothing captured yet, so the window's shape is unknown and only its
		// width can be capped. A window taller than it is wide comes back long
		// on this first call and right on every one after it.
		opts.MaxWidth = viewMaxEdge
	}

	shot, err := s.br.CaptureShot(opts)
	if err != nil {
		return rpcError(id, -32603, "Screenshot capture failed")
	}
	// The macro is replayed against the window, not against a picture, so it
	// records the region in window pixels.
	if crop != nil {
		s.record("takeScreenshot(%d, %d, %d, %d)", int(crop.X), int(crop.Y), int(crop.W), int(crop.H))
	} else {
		s.record("takeScreenshot()")
	}

	content := []any{map[string]any{
		"type":     "image",
		"data":     base64.StdEncoding.EncodeToString(shot.Bytes),
		"mimeType": "image/png",
	}}
	// The picture's own pixel grid is the coordinate space every other tool
	// takes, so state its size — and, for a crop, the offset those coordinates
	// need. Measured off the bytes rather than taken from the shell's reply,
	// which an older shell does not send.
	if cfg, err := png.DecodeConfig(bytes.NewReader(shot.Bytes)); err == nil {
		if !cropped && shot.SourceWidth > 0 && cfg.Width > 0 {
			s.setView(float64(shot.SourceWidth)/float64(cfg.Width), shot.SourceWidth, shot.SourceHeight)
		}
		note := fmt.Sprintf("Screenshot is %d×%d pixels; coordinates for move_to / click "+
			"are measured from its top-left corner.", cfg.Width, cfg.Height)
		if cropped {
			note += fmt.Sprintf(" This is a crop taken at (%d, %d), so add that offset to any coordinate "+
				"read off this image before clicking.", int(x), int(y))
		}
		content = append(content, map[string]any{"type": "text", "text": note})
	}

	return rpcResult(id, map[string]any{"content": content})
}

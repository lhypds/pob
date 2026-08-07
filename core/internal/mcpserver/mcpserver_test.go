package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	"pob/core/internal/bridge"
	"pob/core/internal/ipc"
)

// fakeShell stands in for the native app on the other end of the IPC pipe. It
// keeps the virtual cursor the way every platform does — cursor.move adds a
// delta and answers with the result — so tests exercise the real protocol.
type fakeShell struct {
	mu       sync.Mutex
	pos      bridge.Point
	called   []string
	notified []string // "method=param" for each notification received
}

// note records a notification (no id, no response) such as mcp.state.
func (f *fakeShell) note(method string, params map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notified = append(f.notified, fmt.Sprintf("%s active=%v", method, params["active"]))
}

func (f *fakeShell) notifications() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.notified...)
}

func (f *fakeShell) log(method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = append(f.called, method)
}

func (f *fakeShell) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.called...)
}

func (f *fakeShell) handle(method string, params map[string]any) map[string]any {
	f.log(method)
	num := func(key string) int {
		v, _ := params[key].(float64)
		return int(v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "cursor.reset":
		f.pos = bridge.Point{X: 20, Y: 20}
	case "cursor.move", "mouse.drag":
		f.pos.X += num("dx")
		f.pos.Y += num("dy")
	case "screenshot.capture":
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 600))); err != nil {
			return map[string]any{}
		}
		return map[string]any{"image": encodeBase64(buf.Bytes())}
	case "keyboard.type", "keyboard.keyPress":
		return map[string]any{}
	}
	return map[string]any{"x": float64(f.pos.X), "y": float64(f.pos.Y)}
}

// newTestServer wires a Server to a fakeShell over real os.Pipe stdio, since
// ipc.NewStdio is the only constructor and it reads os.Stdin/os.Stdout.
func newTestServer(t *testing.T) (*Server, *fakeShell) {
	t.Helper()

	shellReads, coreWrites, err := os.Pipe() // core stdout -> shell
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	coreReads, shellWrites, err := os.Pipe() // shell -> core stdin
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = coreReads, coreWrites
	client := ipc.NewStdio()
	os.Stdin, os.Stdout = origIn, origOut

	go client.Run()

	shell := &fakeShell{}
	go func() {
		scanner := bufio.NewScanner(shellReads)
		for scanner.Scan() {
			var msg map[string]any
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				continue
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			id, hasID := msg["id"]
			if !hasID {
				shell.note(method, params)
				continue // notification: no response expected
			}
			reply, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": id, "result": shell.handle(method, params),
			})
			_, _ = shellWrites.Write(append(reply, '\n'))
		}
	}()

	t.Cleanup(func() {
		_ = coreWrites.Close()
		_ = shellWrites.Close()
	})
	return &Server{br: bridge.New(client), sessions: map[string]chan []byte{}}, shell
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func (f *fakeShell) posNow() bridge.Point {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pos
}

// resultText returns the text payload of a tool result, or fails on an error
// response.
func resultText(t *testing.T, resp map[string]any) string {
	t.Helper()
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("unexpected error response: %v", errObj)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", resp)
	}
	content, _ := result["content"].([]any)
	var text string
	for _, c := range content {
		if m, ok := c.(map[string]any); ok && m["type"] == "text" {
			text += m["text"].(string)
		}
	}
	return text
}

// Every advertised tool must be dispatched — a tool listed in tools/list but
// missing from callTool would answer "Unknown tool" at runtime.
func TestEveryAdvertisedToolIsDispatched(t *testing.T) {
	srv, _ := newTestServer(t)

	args := map[string]map[string]any{
		"move_cursor":           {"dx": 10.0, "dy": 10.0},
		"move_cursor_to":        {"x": 100.0, "y": 120.0},
		"move_and_click":        {"x": 100.0, "y": 120.0},
		"move_and_right_click":  {"x": 100.0, "y": 120.0},
		"move_and_double_click": {"x": 100.0, "y": 120.0},
		"drag":                  {"dx": 5.0, "dy": 5.0},
		"drag_to":               {"x": 50.0, "y": 60.0},
		"scroll":                {"dx": 0.0, "dy": 120.0},
		"move_and_scroll":       {"x": 100.0, "y": 120.0, "dx": 0.0, "dy": 120.0},
		"type_text":             {"text": "hello"},
		"key_press":             {"key": "return"},
		"wait":                  {"milliseconds": 1.0},
	}

	for _, name := range ToolNames() {
		resp := srv.callTool(1, name, args[name])
		if errObj, ok := resp["error"].(map[string]any); ok {
			t.Errorf("tool %s returned error: %v", name, errObj["message"])
		}
	}
}

// The UI shows the virtual cursor for as long as the MCP server is up, so the
// shell has to be told when it starts and stops.
func TestServerAnnouncesMCPStateOnStartAndStop(t *testing.T) {
	srv, shell := newTestServer(t)

	if err := srv.Start(0); err != nil { // port 0: let the OS pick a free one
		t.Fatalf("start: %v", err)
	}
	srv.Stop()

	// The notifications are written asynchronously; give the pipe a moment.
	var got []string
	for i := 0; i < 50; i++ {
		got = shell.notifications()
		if len(got) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	want := []string{"mcp.state active=true", "mcp.state active=false"}
	if len(got) != len(want) {
		t.Fatalf("got notifications %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("notification %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// tools/list is what every client reads before it can call anything, so the
// advertised schemas must be complete and serialisable.
func TestToolDefinitionsAreWellFormed(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := srv.processRPC("tools/list", 1, nil)
	if _, err := json.Marshal(resp); err != nil {
		t.Fatalf("tools/list does not marshal: %v", err)
	}

	seen := map[string]bool{}
	for _, entry := range resp["result"].(map[string]any)["tools"].([]any) {
		tool := entry.(map[string]any)
		name, _ := tool["name"].(string)
		if name == "" {
			t.Fatalf("tool with no name: %v", tool)
		}
		if seen[name] {
			t.Errorf("duplicate tool name %q", name)
		}
		seen[name] = true

		if desc, _ := tool["description"].(string); desc == "" {
			t.Errorf("tool %q has no description", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("tool %q has no inputSchema", name)
		}
		props, _ := schema["properties"].(map[string]any)
		// Every required parameter must actually be declared.
		required, _ := schema["required"].([]string)
		for _, req := range required {
			if _, ok := props[req]; !ok {
				t.Errorf("tool %q requires %q but does not declare it", name, req)
			}
		}
		for prop, def := range props {
			d, _ := def.(map[string]any)
			if d["type"] == nil || d["description"] == nil {
				t.Errorf("tool %q parameter %q is missing type or description", name, prop)
			}
		}
	}
}

// The absolute tools translate to the relative move the shell expects, from
// whatever position the cursor happens to be at.
func TestAbsoluteMoveLandsOnTarget(t *testing.T) {
	srv, shell := newTestServer(t)

	if _, err := srv.br.MoveCursor(37, 91); err != nil { // arbitrary starting point
		t.Fatalf("seed move: %v", err)
	}

	got := resultText(t, srv.callTool(1, "move_cursor_to", map[string]any{"x": 400.0, "y": 300.0}))
	if want := "Cursor moved. Cursor at (400, 300)."; got != want {
		t.Errorf("move_cursor_to: got %q, want %q", got, want)
	}

	got = resultText(t, srv.callTool(1, "move_and_double_click", map[string]any{"x": 640.0, "y": 480.0}))
	if want := "Moved and double-clicked. Cursor at (640, 480)."; got != want {
		t.Errorf("move_and_double_click: got %q, want %q", got, want)
	}

	if pos := shell.posNow(); pos.X != 640 || pos.Y != 480 {
		t.Errorf("shell cursor at (%d, %d), want (640, 480)", pos.X, pos.Y)
	}

	// The composite must actually click, not just move.
	var sawDoubleClick bool
	for _, m := range shell.methods() {
		if m == "mouse.doubleClick" {
			sawDoubleClick = true
		}
	}
	if !sawDoubleClick {
		t.Error("move_and_double_click never issued mouse.doubleClick")
	}
}

// Missing coordinates must be reported, not silently treated as (0, 0) — that
// would click the top-left corner of the window.
func TestAbsoluteToolsRejectMissingCoordinates(t *testing.T) {
	srv, shell := newTestServer(t)

	for _, name := range []string{"move_cursor_to", "move_and_click", "drag_to", "move_and_scroll"} {
		resp := srv.callTool(1, name, map[string]any{"dx": 1.0})
		if _, ok := resp["error"].(map[string]any); !ok {
			t.Errorf("%s accepted a call with no x/y: %v", name, resp)
		}
	}
	for _, m := range shell.methods() {
		if m != "cursor.move" {
			continue
		}
		t.Errorf("a rejected call still moved the cursor (%v)", shell.methods())
		break
	}
}

// Models frequently send numbers as JSON strings.
func TestNumericArgumentsAcceptStrings(t *testing.T) {
	srv, _ := newTestServer(t)

	got := resultText(t, srv.callTool(1, "move_and_click", map[string]any{"x": "250", "y": "175"}))
	if want := "Moved and clicked. Cursor at (250, 175)."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// take_screenshot returns the image plus the pixel grid the absolute tools address.
func TestScreenshotReportsItsDimensions(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := srv.callTool(1, "take_screenshot", map[string]any{})
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected image + text content, got %d blocks", len(content))
	}
	if got := content[0].(map[string]any)["type"]; got != "image" {
		t.Errorf("first content block is %v, want image", got)
	}
	if text := resultText(t, resp); !bytes.Contains([]byte(text), []byte("800×600")) {
		t.Errorf("size note missing dimensions: %q", text)
	}
}

package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
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

// The UI shows the virtual cursor while a client is connected, so the shell is
// told when one arrives and when the last one leaves — and not merely when the
// listener comes up, which it does with the instance and says nothing about
// whether anything is driving.
func TestServerAnnouncesMCPStateOnClientConnectAndDisconnect(t *testing.T) {
	srv, shell := newTestServer(t)

	if err := srv.Start(DefaultHost, 0); err != nil { // port 0: let the OS pick a free one
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	settle()
	if got := shell.notifications(); len(got) != 0 {
		t.Fatalf("listening alone announced %v, want nothing until a client connects", got)
	}

	closeFirst := connectSSE(t, srv.Port())
	if got := waitForNotifications(t, shell, 1); len(got) != 1 || got[0] != "mcp.state active=true" {
		t.Fatalf("first client: got %v, want [mcp.state active=true]", got)
	}

	// A second client arriving and leaving is neither announced again nor
	// allowed to take the cursor from the one still connected.
	closeSecond := connectSSE(t, srv.Port())
	closeSecond()
	settle()
	if got := shell.notifications(); len(got) != 1 {
		t.Errorf("second client came and went: got %v, want the cursor left with the first", got)
	}

	closeFirst()
	got := waitForNotifications(t, shell, 2)
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

// Stopping the server drops the clients with it, so the cursor is released
// without anyone disconnecting first.
func TestStoppingTheServerReleasesTheCursor(t *testing.T) {
	srv, shell := newTestServer(t)

	if err := srv.Start(DefaultHost, 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer connectSSE(t, srv.Port())()
	waitForNotifications(t, shell, 1)

	srv.Stop()

	got := waitForNotifications(t, shell, 2)
	if len(got) != 2 || got[1] != "mcp.state active=false" {
		t.Errorf("got notifications %v, want the cursor released on stop", got)
	}
}

// A machine driven from another one binds "0.0.0.0", and the client already
// pointed at localhost has to keep working — a wildcard bind holds 127.0.0.1
// alongside every other address, so opening the server to the network is not a
// move away from the local client.
func TestWildcardBindAnswersLoopbackAndTheNetwork(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start("0.0.0.0", 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()
	port := srv.Port()

	// The local client's address, unchanged by the move to a wildcard bind.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/sse", port))
	if err != nil {
		t.Fatalf("loopback did not answer a wildcard-bound server: %v", err)
	}
	resp.Body.Close()

	// And a routable address of this machine — the one another machine dials.
	// Skipped rather than failed when there is none: a sandbox off every
	// network has nothing to answer on, which says nothing about the bind.
	ip := routableIPv4()
	if ip == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}
	resp, err = http.Get(fmt.Sprintf("http://%s/sse", net.JoinHostPort(ip, strconv.Itoa(port))))
	if err != nil {
		t.Fatalf("%s did not answer a wildcard-bound server: %v", ip, err)
	}
	resp.Body.Close()
}

// The default bind answers the network, so a client on another machine reaches
// it with nothing configured first — the same posture as the Pob server, which
// can type on this machine too and has been open since it existed.
func TestDefaultBindAnswersTheNetwork(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start(DefaultHost, 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	ip := routableIPv4()
	if ip == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(srv.Port()))
	resp, err := http.Get(fmt.Sprintf("http://%s/sse", addr))
	if err != nil {
		t.Fatalf("%s did not answer a default-bound server: %v", addr, err)
	}
	resp.Body.Close()
}

// And the way back: a machine on a network its owner would rather not trust
// sets loopback explicitly, and then nothing on that network can reach it.
func TestAnExplicitLoopbackBindIsLoopbackOnly(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	ip := routableIPv4()
	if ip == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(srv.Port()))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Errorf("%s answered a loopback-bound server, want nothing listening there", addr)
	}
}

// What a wildcard-bound server reports is what someone who has just opened
// `mcp_host` reads to check that it took, and what a client on another machine
// is given: loopback alone would say the opposite of what is true.
func TestWildcardBindReportsTheNetworkAddresses(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start("0.0.0.0", 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	urls := srv.URLs()
	loopback := fmt.Sprintf("http://127.0.0.1:%d/sse", srv.Port())
	if len(urls) == 0 || urls[0] != loopback {
		t.Fatalf("URLs() = %v, want %s first — the local client's address", urls, loopback)
	}
	if srv.Host() != "0.0.0.0" {
		t.Errorf("Host() = %q, want the wildcard it was bound to", srv.Host())
	}

	ip := routableIPv4()
	if ip == "" {
		t.Skip("no non-loopback IPv4 address on this machine")
	}
	want := fmt.Sprintf("http://%s/sse", net.JoinHostPort(ip, strconv.Itoa(srv.Port())))
	for _, u := range urls {
		if u == want {
			return
		}
	}
	t.Errorf("URLs() = %v, want %s among them — the address another machine dials", urls, want)
}

// A loopback-bound server reports loopback and nothing else. Reporting an
// address it does not answer on is worse than saying too little: it sends
// someone off to debug the network for a server that never opened.
func TestLoopbackBindReportsOnlyLoopback(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start("127.0.0.1", 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Stop()

	want := []string{fmt.Sprintf("http://127.0.0.1:%d/sse", srv.Port())}
	if urls := srv.URLs(); len(urls) != 1 || urls[0] != want[0] {
		t.Errorf("URLs() = %v, want %v", urls, want)
	}
}

// Nothing is running, so there is no address to hand anyone.
func TestAStoppedServerReportsNoURL(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start(DefaultHost, 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	srv.Stop()
	if urls := srv.URLs(); urls != nil {
		t.Errorf("URLs() = %v on a stopped server, want none", urls)
	}
}

// routableIPv4 is an address of this machine another machine could dial, or ""
// when it is on no network. Link-local is left out: nothing routes to it.
func routableIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifi := range interfaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
				return ip4.String()
			}
		}
	}
	return ""
}

// The port a client is handed has to be the port it can reach, so a server
// asked for another one moves rather than quietly staying where it was —
// `pob mcp start <port>` always arrives at a server that is already running.
func TestStartMovesARunningServerToANewPort(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Start(DefaultHost, 0); err != nil {
		t.Fatalf("start: %v", err)
	}
	firstPort := srv.Port()
	if firstPort == 0 {
		t.Fatal("Port() = 0 after start, want the port the OS picked")
	}

	// Starting on the port it is already on changes nothing.
	if err := srv.Start(DefaultHost, firstPort); err != nil {
		t.Fatalf("restart on the same port: %v", err)
	}
	if srv.Port() != firstPort {
		t.Errorf("Port() = %d after a same-port start, want %d", srv.Port(), firstPort)
	}

	if err := srv.Start(DefaultHost, 0); err != nil { // another OS-picked port
		t.Fatalf("start on a new port: %v", err)
	}
	defer srv.Stop()
	if srv.Port() == firstPort {
		t.Errorf("Port() = %d, want a port other than the first", srv.Port())
	}
	if _, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/sse", firstPort)); err == nil {
		t.Error("the old port still answers; the listener was not closed")
	}

	// A move to a port that will not bind keeps the server where it is, rather
	// than leaving a registered client with nothing to talk to. Taken on the
	// same interface the server binds — a port held on loopback alone does not
	// stop a wildcard bind on every platform, and the conflict is the point of
	// the test, not the addressing.
	taken, err := net.Listen("tcp", net.JoinHostPort(DefaultHost, "0"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer taken.Close()
	movedTo := srv.Port()
	if err := srv.Start(DefaultHost, taken.Addr().(*net.TCPAddr).Port); err == nil {
		t.Error("Start() on a taken port returned nil, want the bind error")
	}
	if srv.Port() != movedTo || !srv.Running() {
		t.Errorf("after a failed move: running=%v port=%d, want the server still on %d",
			srv.Running(), srv.Port(), movedTo)
	}
}

// connectSSE opens a real /sse stream and waits for the endpoint event, so the
// session is registered by the time it returns. The returned func closes the
// stream; closing twice is safe, since a test may close one early and still
// have it cleaned up at the end.
func connectSSE(t *testing.T, port int) func() {
	t.Helper()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/sse", port))
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}

	var once sync.Once
	closeFn := func() { once.Do(func() { _ = resp.Body.Close() }) }
	t.Cleanup(closeFn)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: /messages?sessionId=") {
			return closeFn
		}
	}
	t.Fatal("no endpoint event on the SSE stream")
	return closeFn
}

// waitForNotifications waits for at least n notifications to arrive down the
// pipe, which they do asynchronously, and returns whatever did.
func waitForNotifications(t *testing.T, shell *fakeShell, n int) []string {
	t.Helper()
	for i := 0; i < 100; i++ {
		if len(shell.notifications()) >= n {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return shell.notifications()
}

// settle gives a notification that should not be sent time to turn up anyway,
// so the tests that assert on silence are testing something.
func settle() { time.Sleep(50 * time.Millisecond) }

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

// fakeRecorder stands in for macro.txt and the shell's record toggle.
type fakeRecorder struct {
	mu    sync.Mutex
	on    bool
	lines []string
}

func (f *fakeRecorder) Recording() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.on
}

func (f *fakeRecorder) AppendToMacro(line string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, line)
}

func (f *fakeRecorder) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

// An MCP client drives the same machine the agent loop does, so a running
// recording has to capture what it did — in the grammar replay reads back, with
// absolute moves written down as the relative offsets replay chains.
func TestToolCallsAreRecordedAsMacroLines(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := &fakeRecorder{on: true}
	srv.SetRecorder(rec)

	srv.callTool(1, "reset_cursor", map[string]any{}) // cursor is now at (20, 20)
	srv.callTool(1, "move_and_click", map[string]any{"x": 100.0, "y": 120.0})
	srv.callTool(1, "scroll", map[string]any{"dx": 0.0, "dy": 120.0})
	srv.callTool(1, "type_text", map[string]any{"text": `say "hi"`})
	srv.callTool(1, "key_press", map[string]any{"key": "cmd+v"})
	srv.callTool(1, "wait", map[string]any{"milliseconds": 250.0})
	srv.callTool(1, "take_screenshot", map[string]any{})

	want := []string{
		"resetCursor()",
		"move(80, 100)",
		"click()",
		"scroll(0, 120)",
		`typeText("say \"hi\"")`,
		`keyPress("cmd+v")`,
		"sleep(250)",
		"take_screenshot()",
	}
	got := rec.recorded()
	if len(got) != len(want) {
		t.Fatalf("recorded %d lines, want %d:\n got: %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// Recorded lines must replay: every action name written here has to be one the
// macro runner dispatches, or a recording turns into a file of skipped lines.
func TestRecordedActionNamesAreReplayable(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := &fakeRecorder{on: true}
	srv.SetRecorder(rec)

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
		srv.callTool(1, name, args[name])
	}

	// Mirrors the switch in agent.runMacroAction; get_cursor_position is the one
	// tool that reads without acting, so it records nothing.
	replayable := map[string]bool{
		"move": true, "click": true, "rightClick": true, "doubleClick": true,
		"drag": true, "scroll": true, "typeText": true, "keyPress": true,
		"sleep": true, "take_screenshot": true, "resetCursor": true,
	}
	for _, line := range rec.recorded() {
		name, _, ok := strings.Cut(line, "(")
		if !ok || !replayable[name] {
			t.Errorf("recorded line %q is not a replayable macro action", line)
		}
	}
}

// The toggle is the shell's: with recording off, nothing reaches macro.txt.
func TestNothingIsRecordedWhileRecordingIsOff(t *testing.T) {
	srv, shell := newTestServer(t)
	rec := &fakeRecorder{on: false}
	srv.SetRecorder(rec)

	srv.callTool(1, "move_and_click", map[string]any{"x": 100.0, "y": 120.0})
	srv.callTool(1, "type_text", map[string]any{"text": "hello"})

	if lines := rec.recorded(); len(lines) != 0 {
		t.Errorf("recorded %q while the toggle was off", lines)
	}
	// The position read that recording needs must not be issued either.
	for _, m := range shell.methods() {
		if m == "cursor.position" {
			t.Error("queried the cursor for a recording that was not running")
			break
		}
	}
}

// A server with no recorder attached (the CLI path) must not panic.
func TestToolCallsSurviveWithoutARecorder(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp := srv.callTool(1, "move_and_click", map[string]any{"x": 10.0, "y": 10.0}); resp["error"] != nil {
		t.Errorf("call failed without a recorder: %v", resp["error"])
	}
}

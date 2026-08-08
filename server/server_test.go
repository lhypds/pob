package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestPobKey(t *testing.T) {
	cases := []struct {
		chord string
		want  string
		ok    bool
	}{
		{"c", "c", true},
		{"7", "7", true},
		{"ENTER", "return", true},
		{"BACKSPACE", "backspace", true},
		{"DELETE", "forwarddelete", true},
		{"FORWARD_SLASH", "slash", true},
		{"F13", "f13", true},
		{"CTRL+c", "ctrl+c", true},
		{"GUI+SHIFT+4", "shift+gui+4", true},
		{"CTRL+ALT+SHIFT+GUI+a", "ctrl+alt+shift+gui+a", true},
		{"SHIFT+EQUALS", "shift+equals", true},
		// The keypad's asterisk is shift-8 on a US layout, which is how the
		// board types it too.
		{"*", "shift+8", true},
		// An upper-case letter is the shifted key, not a key of its own.
		{"A", "shift+a", true},
		// Nothing here can press a bare modifier, or a name that isn't a key.
		{"SHIFT", "", false},
		{"WAT", "", false},
		{"CTRL+WAT", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := pobKey(c.chord)
		if ok != c.ok || got != c.want {
			t.Errorf("pobKey(%q) = %q, %v; want %q, %v", c.chord, got, ok, c.want, c.ok)
		}
	}
}

func TestParseMouse(t *testing.T) {
	action, x, y, ok := parseMouse("MOVE(-3,12)")
	if !ok || action != "MOVE" || x != -3 || y != 12 {
		t.Errorf(`parseMouse("MOVE(-3,12)") = %q, %d, %d, %v`, action, x, y, ok)
	}
	for _, bad := range []string{"MOVE", "MOVE(1)", "MOVE(1,2", "MOVE(a,b)", ""} {
		if _, _, _, ok := parseMouse(bad); ok {
			t.Errorf("parseMouse(%q) should not parse", bad)
		}
	}
}

// fakeTarget records what reached the machine, in order.
type fakeTarget struct {
	calls  []string
	cursor [2]int
}

func (f *fakeTarget) CursorPosition() (int, int, error) { return f.cursor[0], f.cursor[1], nil }

func (f *fakeTarget) MoveCursor(dx, dy float64) error {
	f.cursor[0] += int(dx)
	f.cursor[1] += int(dy)
	f.record("move %v %v", dx, dy)
	return nil
}

func (f *fakeTarget) MoveCursorTo(x, y float64) error {
	f.cursor = [2]int{int(x), int(y)}
	f.record("moveTo %v %v", x, y)
	return nil
}

func (f *fakeTarget) Click() error                { f.record("click"); return nil }
func (f *fakeTarget) RightClick() error           { f.record("rightClick"); return nil }
func (f *fakeTarget) DoubleClick() error          { f.record("doubleClick"); return nil }
func (f *fakeTarget) Drag(dx, dy float64) error   { f.record("drag %v %v", dx, dy); return nil }
func (f *fakeTarget) Scroll(dx, dy int) error     { f.record("scroll %d %d", dx, dy); return nil }
func (f *fakeTarget) TypeText(text string) error  { f.record("type %s", text); return nil }
func (f *fakeTarget) KeyPress(key string) error   { f.record("key %s", key); return nil }
func (f *fakeTarget) SetRemoteActive(active bool) { f.record("active %v", active) }

func (f *fakeTarget) CaptureView() ([]byte, error) {
	f.record("capture")
	return []byte("\x89PNG\r\n\x1a\n"), nil
}

func (f *fakeTarget) record(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func run(t *testing.T, bodies ...string) *fakeTarget {
	t.Helper()
	target := &fakeTarget{}
	ctl := newController(target, func(string, ...any) {})
	for _, body := range bodies {
		ctl.run(body)
	}
	return target
}

func TestTyping(t *testing.T) {
	// A trailing space is a real keystroke and must survive.
	got := run(t, "typing=hi ").calls
	if want := []string{"type hi "}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestKeycodeSequence(t *testing.T) {
	got := run(t, "keycode=CTRL+c,CTRL+v").calls
	want := []string{"key ctrl+c", "key ctrl+v"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSeqDedup(t *testing.T) {
	// The same token twice is a retry of a command that already ran; a new
	// token is a new command, even with an identical body.
	got := run(t,
		"seq=a-1&keycode=ENTER",
		"seq=a-1&keycode=ENTER",
		"seq=a-2&keycode=ENTER",
	).calls
	want := []string{"key return", "key return"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrollDirection(t *testing.T) {
	// One notch up for the client is that many pixels *up* for Pob, whose
	// scroll counts downwards.
	got := run(t, "mouse=SCROLL(0,2)").calls
	if want := []string{"scroll 0 -80"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDragPlaysOutOnRelease(t *testing.T) {
	got := run(t,
		"mouse=PRESS(0,0)",
		"mouse=MOVE(30,10)",
		"mouse=MOVE(10,5)",
		"mouse=RELEASE(0,0)",
	).calls
	// The cursor follows the finger while the button is notionally down, then
	// goes back to where it went down so the drag can be played out whole.
	want := []string{"move 30 10", "move 10 5", "moveTo 0 0", "drag 40 15"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPressAndReleaseInPlaceIsAClick(t *testing.T) {
	got := run(t, "mouse=PRESS(0,0)", "mouse=RELEASE(0,0)").calls
	if want := []string{"click"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDoubleClickAfterPress(t *testing.T) {
	// The page presses on the second tap of a double-tap, then sends a
	// double-click rather than a release when the finger lifts in place.
	got := run(t, "mouse=PRESS(0,0)", "mouse=DOUBLE_CLICK(0,0)", "mouse=RELEASE(0,0)").calls
	if want := []string{"doubleClick"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplitHead(t *testing.T) {
	cases := []struct{ path, head, rest string }{
		{"/", "", ""},
		{"/pb-a703", "pb-a703", ""},
		{"/pb-a703/", "pb-a703", "/"},
		{"/pb-a703/view", "pb-a703", "/view"},
		{"/pb-a703/control/x", "pb-a703", "/control/x"},
	}
	for _, c := range cases {
		head, rest := splitHead(c.path)
		if head != c.head || rest != c.rest {
			t.Errorf("splitHead(%q) = %q, %q; want %q, %q", c.path, head, rest, c.head, c.rest)
		}
	}
}

// freePort takes a port the OS has just confirmed is free. Racy in principle,
// but nothing else on a test machine is racing for it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// serve starts a server on a free port and returns the base address.
func serve(t *testing.T, target Target) string {
	t.Helper()
	port := freePort(t)
	server := New("pb-aaaa", target, nil)
	if err := server.Start(port); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Stop)
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// Each address answers with the one thing it names, and a page can be reached
// with or without the instance in the path — an address written down before
// and one typed from memory both arrive.
func TestEachAddressServesItsOwnThing(t *testing.T) {
	base := serve(t, &fakeTarget{})
	cases := []struct{ path, want string }{
		// The address that names the instance is the machine itself, so what
		// it answers with is a picture of it, not a page.
		{"/pb-aaaa", "\x89PNG"},
		{"/pb-aaaa/", "\x89PNG"},
		{"/control", `id="trackpad"`},
		{"/pb-aaaa/control", `id="trackpad"`},
		{"/pb-aaaa/control/", `id="trackpad"`},
		{"/view", `id="frame-a"`},
		{"/pb-aaaa/view", `id="frame-a"`},
		{"/status", `"instance":"pb-aaaa"`},
		{"/pb-aaaa/status", `"instance":"pb-aaaa"`},
	}
	for _, c := range cases {
		if body := get(t, base+c.path); !bytes.Contains(body, []byte(c.want)) {
			t.Errorf("GET %s did not serve %s: %.80s", c.path, c.want, body)
		}
	}
}

// The bare root is the index, not the machine: the shortest address on the
// network must not answer with a picture of someone's screen.
func TestBareRootIsTheIndexNotTheMachine(t *testing.T) {
	target := &fakeTarget{}
	base := serve(t, target)
	if body := get(t, base+"/"); !bytes.Contains(body, []byte(`id="addresses"`)) {
		t.Errorf("GET / did not serve the index page: %.80s", body)
	}
	if len(target.calls) != 0 {
		t.Errorf("GET / captured the machine anyway: %q", target.calls)
	}
}

// A frame must be typed as one — an <img> is all that ever asks for it.
func TestFrameIsAnImage(t *testing.T) {
	base := serve(t, &fakeTarget{})
	resp, err := http.Get(base + "/pb-aaaa")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	// A cached frame is a moment that has already passed.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// The index page reads the instance's state from here, so what the server was
// told must come back out — with the server's own address alongside it.
func TestStatusReportsTheInstanceAndTheServer(t *testing.T) {
	port := freePort(t)
	server := New("pb-aaaa", &fakeTarget{}, nil)
	server.SetStatus(func() Status {
		return Status{Root: "/tmp/pob", Model: "a-model", Executing: true, Session: "s-1"}
	})
	if err := server.Start(port); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	var got Status
	body := get(t, fmt.Sprintf("http://127.0.0.1:%d/status", port))
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Instance != "pb-aaaa" || got.Root != "/tmp/pob" || got.Session != "s-1" || !got.Executing {
		t.Errorf("status = %+v", got)
	}
	if got.Port != port {
		t.Errorf("status port = %d, want %d", got.Port, port)
	}
}

// Watching is not driving: a tab left open on the view page would otherwise
// keep the virtual cursor pinned on screen for as long as it stayed open.
func TestWatchingDoesNotMarkTheServerActive(t *testing.T) {
	target := &fakeTarget{}
	base := serve(t, target)
	get(t, base+"/pb-aaaa")
	if want := []string{"capture"}; !reflect.DeepEqual(target.calls, want) {
		t.Errorf("got %q, want %q", target.calls, want)
	}
}

// Nothing else is here, so a path naming another instance is a mistake worth
// saying out loud rather than quietly serving this one — and so is a path
// under this instance that names nothing.
func TestUnknownPathsAreNotFound(t *testing.T) {
	base := serve(t, &fakeTarget{})
	for _, path := range []string{"/pb-bbbb/", "/favicon.ico", "/pb-aaaa/nope", "/pb-aaaa/view/x"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

// Only the root takes commands. The pages are pages, and a command posted to
// one is a client that has the address wrong — worth saying so.
func TestCommandOnAPageIsNotAllowed(t *testing.T) {
	target := &fakeTarget{}
	base := serve(t, target)
	resp, err := http.Post(base+"/pb-aaaa/control", "text/plain", strings.NewReader("typing=hello"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /pb-aaaa/control = %d, want 405", resp.StatusCode)
	}
	if len(target.calls) != 0 {
		t.Errorf("the machine was driven anyway: %q", target.calls)
	}
}

// A command posted to the bare root reaches the machine — served where it
// lands, never redirected, since most clients would turn a redirected POST
// into a GET and the keystroke would be lost on the way.
func TestCommandAtRootReachesTheMachine(t *testing.T) {
	target := &fakeTarget{}
	base := serve(t, target)

	resp, err := http.Post(base+"/", "text/plain", strings.NewReader("typing=hello"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if want := []string{"active true", "type hello"}; !reflect.DeepEqual(target.calls, want) {
		t.Errorf("got %q, want %q", target.calls, want)
	}
}

// One instance runs, so a taken port means something else has it. Reporting
// that beats starting a server nobody can reach.
func TestPortAlreadyTakenIsAnError(t *testing.T) {
	port := freePort(t)
	// The same address the server binds. Holding only 127.0.0.1 would not
	// collide: SO_REUSEADDR lets a 0.0.0.0 bind through beside it.
	held, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	server := New("pb-aaaa", &fakeTarget{}, nil)
	if err := server.Start(port); err == nil {
		server.Stop()
		t.Fatal("Start on a port already in use reported success")
	}
	if server.Running() {
		t.Error("Start failed but the server reports it is running")
	}
}

func get(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

package server

import (
	"bytes"
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

func TestSplitInstance(t *testing.T) {
	cases := []struct{ path, id, rest string }{
		{"/", "", ""},
		{"/pb-a703", "pb-a703", ""},
		{"/pb-a703/", "pb-a703", "/"},
		{"/pb-a703/anything", "pb-a703", "/anything"},
	}
	for _, c := range cases {
		id, rest := splitInstance(c.path)
		if id != c.id || rest != c.rest {
			t.Errorf("splitInstance(%q) = %q, %q; want %q, %q", c.path, id, rest, c.id, c.rest)
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

// The bare root and the path naming the instance are the same page, so an
// address written down before and one typed from memory both arrive.
func TestRootAndInstancePathBothServeThePage(t *testing.T) {
	port := freePort(t)
	server := New("pb-aaaa", &fakeTarget{}, nil)
	if err := server.Start(port); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, path := range []string{"/", "/pb-aaaa", "/pb-aaaa/"} {
		if body := get(t, base+path); !bytes.Contains(body, []byte(`id="trackpad"`)) {
			t.Errorf("GET %s did not serve the web UI page: %.80s", path, body)
		}
	}
}

// Nothing else is here, so a path naming another instance is a mistake worth
// saying out loud rather than quietly serving this one.
func TestOtherInstanceIDIsNotFound(t *testing.T) {
	port := freePort(t)
	server := New("pb-aaaa", &fakeTarget{}, nil)
	if err := server.Start(port); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/pb-bbbb/", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /pb-bbbb/ = %d, want 404", resp.StatusCode)
	}
}

// A command posted to the bare root reaches the machine — served where it
// lands, never redirected, since most clients would turn a redirected POST
// into a GET and the keystroke would be lost on the way.
func TestCommandAtRootReachesTheMachine(t *testing.T) {
	port := freePort(t)
	target := &fakeTarget{}
	server := New("pb-aaaa", target, nil)
	if err := server.Start(port); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/", port),
		"text/plain", strings.NewReader("typing=hello"))
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

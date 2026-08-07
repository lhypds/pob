package webui

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
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

// Two instances share one port: whichever binds it must serve both, handing
// the other's requests to the process that owns it.
func TestSharedPortServesEveryInstance(t *testing.T) {
	logs := t.TempDir()
	port := freePort(t)

	first, second := &fakeTarget{}, &fakeTarget{}
	a := New("pb-aaaa", logs, first, nil)
	b := New("pb-bbbb", logs, second, nil)
	if err := a.Start(port); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	if err := b.Start(port); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()

	if !a.HoldsPort() || b.HoldsPort() {
		t.Fatalf("expected the first instance to hold the port (a=%v b=%v)", a.HoldsPort(), b.HoldsPort())
	}

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, id := range []string{"pb-aaaa", "pb-bbbb"} {
		resp, err := http.Get(base + "/" + id + "/")
		if err != nil {
			t.Fatalf("GET /%s/: %v", id, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !bytes.Contains(body, []byte("id=\"trackpad\"")) {
			t.Errorf("GET /%s/ = %d, %d bytes — not the web UI page", id, resp.StatusCode, len(body))
		}
	}

	// A command for the second instance must reach the second instance, not
	// the one that happens to be holding the port.
	resp, err := http.Post(base+"/pb-bbbb/", "text/plain", strings.NewReader("typing=hello"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(first.calls) != 0 {
		t.Errorf("the front-door instance ran it instead: %q", first.calls)
	}
	if want := []string{"active true", "type hello"}; !reflect.DeepEqual(second.calls, want) {
		t.Errorf("second instance got %q, want %q", second.calls, want)
	}
}

// Closing the instance that holds the port must not take the machine off the
// air: another one takes over.
func TestPortIsHandedOn(t *testing.T) {
	logs := t.TempDir()
	port := freePort(t)

	a := New("pb-aaaa", logs, &fakeTarget{}, nil)
	b := New("pb-bbbb", logs, &fakeTarget{}, nil)
	if err := a.Start(port); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(port); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()

	a.Stop()
	deadline := time.Now().Add(claimInterval * 3)
	for !b.HoldsPort() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !b.HoldsPort() {
		t.Fatal("the remaining instance never took the port")
	}

	// And with only one instance left, the bare root leads to it.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/pb-bbbb/" {
		t.Errorf("/ landed on %q, want /pb-bbbb/", resp.Request.URL.Path)
	}
}

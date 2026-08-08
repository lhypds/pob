package server

import (
	"strconv"
	"strings"
	"sync"
)

// Target is the machine the server drives: Pob's virtual cursor and the
// keyboard and mouse events the native shell posts for it. Coordinates are
// screenshot pixels, the same space the MCP tools work in.
type Target interface {
	CursorPosition() (x, y int, err error)
	MoveCursor(dx, dy float64) error
	MoveCursorTo(x, y float64) error
	Click() error
	RightClick() error
	DoubleClick() error
	Drag(dx, dy float64) error
	Scroll(dx, dy int) error
	TypeText(text string) error
	KeyPress(key string) error
	// CaptureView is what the machine looks like right now, as PNG bytes —
	// the same capture the agent and the MCP server work from, with the
	// virtual cursor drawn in, since a browser watching from across the room
	// has no other way to see where the pointer is.
	CaptureView() ([]byte, error)
	// SetRemoteActive tells the shell a browser is driving this instance, so
	// the virtual cursor stays on screen while it is — otherwise the moves
	// would be invisible and look like nothing happened.
	SetRemoteActive(active bool)
}

// scrollPixelsPerNotch converts the wheel notches the clients send into the
// pixel amounts Pob scrolls by. It matches what the shells already assume a
// notch is worth when they translate the other way.
const scrollPixelsPerNotch = 40

// controller runs the commands one at a time. The clients already send one
// request at a time, but two browsers can be open at once, and a half-applied
// drag from interleaved commands would be worse than a queued one.
type controller struct {
	target Target
	logf   func(string, ...any)

	mu      sync.Mutex
	lastSeq string
	// holding is a button the client thinks is down, from PRESS to RELEASE,
	// and holdX/holdY is where it went down. Pob cannot hold a button between
	// calls — the shells post a whole click or a whole drag and hold nothing
	// in between — so the press is only remembered. The cursor still follows
	// the finger, so the drop target is visible, and the drag is played out in
	// one go on release, from where the button went down to where it came up.
	holding      bool
	holdX, holdY int
}

func newController(target Target, logf func(string, ...any)) *controller {
	return &controller{target: target, logf: logf}
}

// run executes one command body, in the form the pico-hid HTTP API takes:
// an optional "seq=<token>&" prefix followed by one of typing=, keycode=,
// consumer= or mouse=.
func (c *controller) run(body string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Delivery is at-least-once: a client retries when a response goes
	// missing, which may be after the command already ran. The seq token is
	// stamped once and reused across retries, so a repeat of the last token
	// is that retry and not a second keystroke.
	if rest, ok := strings.CutPrefix(body, "seq="); ok {
		token, remainder, _ := strings.Cut(rest, "&")
		body = remainder
		if token == c.lastSeq {
			return
		}
		c.lastSeq = token
	}

	verb, argument, ok := strings.Cut(body, "=")
	if !ok {
		return
	}
	switch verb {
	case "typing":
		// No trimming: a lone or trailing space is a real keystroke — the
		// page's mirror mode sends each space as its own typing command.
		c.fail("type", c.target.TypeText(argument))

	case "keycode":
		// "," separates keys pressed one after another, "+" joins keys held
		// together as a chord: "CTRL+c" or "CTRL+c,CTRL+v".
		for _, chord := range strings.Split(strings.TrimSpace(argument), ",") {
			key, ok := pobKey(chord)
			if !ok {
				c.logf("Server: no key for %q", chord)
				continue
			}
			c.fail("key press", c.target.KeyPress(key))
		}

	case "mouse":
		c.mouse(strings.TrimSpace(argument))

	case "consumer":
		// Media and brightness keys. The Pob Keyboard offers them on a Mac
		// target's function row, but the shells post plain key events and have
		// nowhere to put a consumer-control usage, so they go no further.
		c.logf("Server: consumer control %q is not supported", strings.TrimSpace(argument))

	case "automove":
		// The board's idle-jiggler. Pob has no such mode and needs none.

	default:
		c.logf("Server: unknown command %q", verb)
	}
}

func (c *controller) mouse(argument string) {
	action, x, y, ok := parseMouse(argument)
	if !ok {
		c.logf("Server: bad mouse command %q", argument)
		return
	}

	// Every action but MOVE carries an offset to apply first. The clients
	// always send (0,0) — they move the cursor with MOVE and then act where it
	// landed — but honouring it keeps the command shape the API's.
	moveFirst := func() {
		if x != 0 || y != 0 {
			c.fail("move", c.target.MoveCursor(float64(x), float64(y)))
		}
	}

	switch action {
	case "MOVE":
		c.fail("move", c.target.MoveCursor(float64(x), float64(y)))

	case "CLICK":
		moveFirst()
		c.fail("click", c.target.Click())

	case "RIGHT_CLICK":
		moveFirst()
		c.fail("right click", c.target.RightClick())

	case "DOUBLE_CLICK":
		// A double-click can arrive with the button notionally held: the page
		// presses on the second tap of a double-tap so a drag can start there,
		// then sends this instead of a release when the finger lifts in place.
		// Nothing was actually pressed, so there is nothing to undo.
		c.holding = false
		moveFirst()
		c.fail("double click", c.target.DoubleClick())

	case "PRESS":
		moveFirst()
		cx, cy, err := c.target.CursorPosition()
		if err != nil {
			c.fail("press", err)
			return
		}
		c.holding, c.holdX, c.holdY = true, cx, cy

	case "RELEASE":
		moveFirst()
		if !c.holding {
			return // a release with nothing held: the press was already spent
		}
		c.holding = false
		endX, endY, err := c.target.CursorPosition()
		if err != nil {
			c.fail("release", err)
			return
		}
		dx, dy := endX-c.holdX, endY-c.holdY
		if dx == 0 && dy == 0 {
			// Pressed and released without going anywhere — a press-and-hold,
			// which on the target is just a click.
			c.fail("click", c.target.Click())
			return
		}
		// Back to where the button went down, then drag to where the finger
		// left off. The cursor ends where it already was, so the jump back is
		// only ever a frame long.
		if err := c.target.MoveCursorTo(float64(c.holdX), float64(c.holdY)); err != nil {
			c.fail("drag", err)
			return
		}
		c.fail("drag", c.target.Drag(float64(dx), float64(dy)))

	case "SCROLL":
		// Wheel notches in y, positive scrolling up — the opposite sign to
		// Pob's own scroll, which counts pixels downwards.
		c.fail("scroll", c.target.Scroll(x*scrollPixelsPerNotch, -y*scrollPixelsPerNotch))

	default:
		c.logf("Server: unknown mouse action %q", action)
	}
}

// parseMouse reads "ACTION(x,y)".
func parseMouse(s string) (action string, x, y int, ok bool) {
	name, rest, found := strings.Cut(s, "(")
	if !found || !strings.HasSuffix(rest, ")") {
		return "", 0, 0, false
	}
	first, second, found := strings.Cut(strings.TrimSuffix(rest, ")"), ",")
	if !found {
		return "", 0, 0, false
	}
	x, errX := strconv.Atoi(strings.TrimSpace(first))
	y, errY := strconv.Atoi(strings.TrimSpace(second))
	if errX != nil || errY != nil {
		return "", 0, 0, false
	}
	return strings.ToUpper(strings.TrimSpace(name)), x, y, true
}

// fail logs a command that didn't land. Nothing is reported back to the
// client: it has no room to show a status, and a keystroke that went missing
// is something you see on the target machine anyway.
func (c *controller) fail(what string, err error) {
	if err != nil {
		c.logf("Server: %s failed: %v", what, err)
	}
}

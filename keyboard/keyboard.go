// Command keyboard is Pob Keyboard: a desktop front end for a running Pob
// instance — a full-size on-screen keyboard, and a trackpad beside it, driving
// the machine Pob runs on through the web UI's HTTP API.
//
//	./keyboard.sh
//
// It is the pico-hid board's keyboard client pointed at Pob instead: the same
// board, the same trackpad, the same wire protocol. Only the address differs —
// Pob is reached at http://<machine>:<port>/<instance>, the address `pob
// status` prints.
//
// The address is set under Settings… in the menu bar and remembered between
// runs. It can also come from -url or POB_SERVER_URL (or .env).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// Sending
//
// Everything funnels through a single worker that sends one command at a time,
// exactly like the queue in the web UI's own page: Pob answers a command only
// once it has run, so one request in flight is what keeps the commands in the
// order they were made. The two clients speak the same protocol, including the
// seq de-duplication token.
// ---------------------------------------------------------------------------

const (
	sendTimeout = 6 * time.Second // a stalled request must not wedge the queue
	maxTries    = 3
)

// Settings remembered between runs.
const (
	appName          = "Pob Keyboard"
	appID            = "dev.pob.keyboard"
	prefInstanceHost = "instance-host"
	prefInstanceID   = "instance-id"
	prefPort         = "instance-port"
	prefSpeed        = "pointer-speed"
	prefTarget       = "target-os"
)

// Pob is addressed by the machine it runs on and the id of the instance
// there:
//
//	http://192.168.1.40:8033/pb-a703
//
// A machine runs one instance and it keeps its id, so this address is worth
// saving — it stays right across restarts. The id is the four hex digits the
// app shows in its toolbar; `pob status` prints the whole address.
const (
	instanceIDPrefix = "pb-"
	instanceIDHint   = "xxxx"
	hostHint         = "192.168.1.40"
	// An id is always this long: Pob builds it from two bytes of a fresh UID
	// as lowercase hex.
	instanceIDLength = 4
	// The port Pob is reached through, unless someone has changed webui_port
	// — which is why it is editable at all.
	defaultPort = 8033
)

// instanceURL is the address of an instance, or "" until enough of it is
// known to be worth sending anything to.
func instanceURL(host string, port int, id string) string {
	host = strings.TrimSpace(host)
	if id = normalizeInstanceID(id); host == "" || id == "" {
		return ""
	}
	return fmt.Sprintf("http://%s/%s%s/", net.JoinHostPort(host, strconv.Itoa(port)), instanceIDPrefix, id)
}

// splitAddress takes an address back apart into the three things the settings
// ask for. What it can't read comes back empty, or as the default port — which
// is what a half-typed field leaves behind.
func splitAddress(address string) (host string, port int, id string) {
	port = defaultPort
	u, err := url.Parse(normalizeURL(address))
	if err != nil {
		return "", port, ""
	}
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 && p < 65536 {
		port = p
	}
	return u.Hostname(), port, normalizeInstanceID(u.Path)
}

// normalizeInstanceID reduces what was typed to an id an instance could
// actually have: hex digits, lower case, and never more than four of them. It
// tolerates a whole address being pasted in, since that is the obvious thing
// to try.
func normalizeInstanceID(s string) string {
	var id strings.Builder
	for _, r := range strings.ToLower(cleanInstanceID(s)) {
		if id.Len() == instanceIDLength {
			break
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			id.WriteRune(r)
		}
	}
	return id.String()
}

// cleanInstanceID reduces whatever was typed or pasted to the id itself, so a
// whole address pasted into the field still lands on the right instance. The
// id is the last thing in an address, which is what makes this a matter of
// taking the final path element.
func cleanInstanceID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimPrefix(s, instanceIDPrefix)
}

// limitToInstanceID keeps an entry's text down to something an instance id
// could be. Fyne's Entry has no length limit of its own, so over-long or
// non-hex input is put back trimmed — which re-enters this handler once and
// then settles.
func limitToInstanceID(entry *widget.Entry, apply func(id string)) func(string) {
	return func(s string) {
		if id := normalizeInstanceID(s); id != s {
			entry.SetText(id)
			return
		}
		apply(s)
	}
}

// limitToPort does the same for the port field: digits only, and never more
// than a port number can be.
func limitToPort(entry *widget.Entry, apply func(port int)) func(string) {
	return func(s string) {
		var digits strings.Builder
		for _, r := range s {
			if r >= '0' && r <= '9' && digits.Len() < 5 {
				digits.WriteRune(r)
			}
		}
		if digits.String() != s {
			entry.SetText(digits.String())
			return
		}
		port, err := strconv.Atoi(s)
		if err != nil || port < 1 || port > 65535 {
			return // half-typed; the last valid one stays in force
		}
		apply(port)
	}
}

// limitToHost keeps the host field to a host. Pasting a whole address into it
// is the obvious thing to do with the line `pob status` prints, so that is
// taken apart and spread across all three fields rather than rejected.
func limitToHost(entry *widget.Entry, apply func(host string), spread func(host string, port int, id string)) func(string) {
	return func(s string) {
		if !strings.ContainsAny(s, "/ \t") {
			apply(strings.TrimSpace(s))
			return
		}
		host, port, id := splitAddress(s)
		entry.SetText(host)
		spread(host, port, id)
	}
}

// Which machine Pob is running on. It changes no key that gets sent — the keys
// are always physical ones — only how the modifiers are labelled and which
// side of the space bar each one sits on.
const (
	targetWindows = "Windows"
	targetMac     = "macOS"
)

func defaultTarget() string {
	if runtime.GOOS == "darwin" {
		return targetMac
	}
	return targetWindows
}

type command struct {
	body  string
	tries int
	retry bool // motion is not worth retrying; a keystroke is
}

// addrCache remembers what the machine's name resolved to. An address typed as
// an IP costs nothing to look up, but a name can — seconds, on a network whose
// resolver is slow — and that lands on every connection the pool has to open.
// Resolve once, reuse, and look it up again only if connecting to the
// remembered address fails.
type addrCache struct {
	dialer net.Dialer

	mu    sync.Mutex
	known map[string]string // "host:port" -> the "ip:port" it resolved to
}

func (c *addrCache) dial(ctx context.Context, network, address string) (net.Conn, error) {
	c.mu.Lock()
	known := c.known[address]
	c.mu.Unlock()

	if known != "" {
		if conn, err := c.dialer.DialContext(ctx, network, known); err == nil {
			return conn, nil
		}
		// The machine may have been given a different address; forget and re-look.
		c.mu.Lock()
		delete(c.known, address)
		c.mu.Unlock()
	}

	conn, err := c.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.known[address] = conn.RemoteAddr().String()
	c.mu.Unlock()
	return conn, nil
}

func (c *addrCache) forget() {
	c.mu.Lock()
	clear(c.known)
	c.mu.Unlock()
}

type sender struct {
	client *http.Client
	addrs  *addrCache
	wake   chan struct{}

	mu        sync.Mutex
	url       string
	queue     []*command
	dx, dy    float32 // pointer motion, coalesced rather than queued
	scroll    float32 // wheel notches, coalesced too; positive scrolls up
	seqPrefix string
	seqNum    int
}

func newSender(address string) *sender {
	addrs := &addrCache{
		dialer: net.Dialer{Timeout: sendTimeout},
		known:  map[string]string{},
	}
	return &sender{
		// Commands go out back to back, so the connection is worth keeping:
		// one kept alive saves a handshake — and a name lookup — on every
		// keystroke and every pointer move.
		client: &http.Client{Transport: &http.Transport{
			DialContext:     addrs.dial,
			MaxConnsPerHost: 1,
			IdleConnTimeout: 90 * time.Second,
		}},
		addrs:     addrs,
		wake:      make(chan struct{}, 1),
		url:       address,
		seqPrefix: strconv.FormatUint(uint64(rand.Uint32()), 36),
	}
}

// resolve looks the instance up now, so the wait doesn't land on the first
// keystroke. Nothing is sent: it opens a connection and drops it.
func (s *sender) resolve() {
	s.mu.Lock()
	target := s.url
	s.mu.Unlock()
	if target == "" {
		return
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(host, "80")
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if conn, err := s.addrs.dial(ctx, "tcp", host); err == nil {
		conn.Close()
	}
}

func (s *sender) setURL(u string) {
	s.mu.Lock()
	changed := s.url != u
	s.url = u
	s.mu.Unlock()
	if changed {
		s.addrs.forget() // a different instance, so a different address
	}
}

// signal nudges the worker without blocking; a pending nudge is enough.
func (s *sender) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// stamp prefixes a command with its seq token. Delivery is at-least-once: a
// retry can re-send a command Pob already ran (the response was lost, not the
// request). The token is stamped once and reused on retry, and Pob skips a
// token it just handled, so a keystroke can't come out doubled.
func (s *sender) stamp(body string) string {
	s.seqNum++
	return "seq=" + s.seqPrefix + "-" + strconv.Itoa(s.seqNum) + "&" + body
}

func (s *sender) enqueue(body string) {
	s.mu.Lock()
	s.queue = append(s.queue, &command{body: s.stamp(body), retry: true})
	s.mu.Unlock()
	s.signal()
}

// moveBy accumulates pointer motion instead of queueing it: a stale delta is
// worth less than a fresh one, so unlike a keystroke it is fine to merge or
// drop. Same for scrollBy.
func (s *sender) moveBy(dx, dy float32) {
	s.mu.Lock()
	s.dx += dx
	s.dy += dy
	s.mu.Unlock()
	s.signal()
}

func (s *sender) scrollBy(notches float32) {
	s.mu.Lock()
	s.scroll += notches
	s.mu.Unlock()
	s.signal()
}

// mouseCmd queues a discrete mouse command, flushing coalesced motion ahead of
// it: the queue is drained before pending motion, so without this a click or a
// button release would overtake the moves that were meant to position it.
func (s *sender) mouseCmd(body string) {
	s.mu.Lock()
	if dx, dy, ok := s.takeMotionLocked(); ok {
		s.queue = append(s.queue, &command{body: s.stamp(fmt.Sprintf("mouse=MOVE(%d,%d)", dx, dy))})
	}
	s.queue = append(s.queue, &command{body: s.stamp(body), retry: true})
	s.mu.Unlock()
	s.signal()
}

// takeMotionLocked converts accumulated motion into whole pixels, keeping the
// fraction behind so slow movement still adds up to a step eventually.
func (s *sender) takeMotionLocked() (int, int, bool) {
	dx := int(math.Round(float64(s.dx)))
	dy := int(math.Round(float64(s.dy)))
	if dx == 0 && dy == 0 {
		return 0, 0, false
	}
	s.dx -= float32(dx)
	s.dy -= float32(dy)
	return dx, dy, true
}

// next picks the command to send: queued ones first, then coalesced motion,
// then coalesced scrolling. Returns nil when there is nothing to do.
func (s *sender) next() *command {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.queue) > 0 {
		c := s.queue[0]
		s.queue = s.queue[1:]
		return c
	}
	if dx, dy, ok := s.takeMotionLocked(); ok {
		return &command{body: s.stamp(fmt.Sprintf("mouse=MOVE(%d,%d)", dx, dy))}
	}
	if n := int(math.Round(float64(s.scroll))); n != 0 {
		s.scroll -= float32(n)
		return &command{body: s.stamp(fmt.Sprintf("mouse=SCROLL(0,%d)", n))}
	}
	return nil
}

func (s *sender) requeue(c *command) {
	s.mu.Lock()
	s.queue = append([]*command{c}, s.queue...)
	s.mu.Unlock()
	s.signal()
}

func (s *sender) run() {
	for {
		c := s.next()
		if c == nil {
			<-s.wake
			continue
		}

		// A failed command is retried and then let go. Nothing is reported: the
		// window has no room set aside for status, and a keystroke that didn't
		// land is something you see on the target machine anyway.
		err := s.post(c.body)
		c.tries++
		if err != nil && c.retry && c.tries < maxTries {
			s.requeue(c)
		}
	}
}

func (s *sender) post(body string) error {
	s.mu.Lock()
	target := s.url
	s.mu.Unlock()
	if target == "" {
		return errors.New("no Pob instance address set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := s.client.Do(req)
	if err != nil {
		// A *url.Error repeats the whole request line; the cause alone reads
		// better in a one-line message.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			return uerr.Err
		}
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Pob returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// normalizeURL turns what someone is likely to have to hand —
// "192.168.1.40:8033/pb-3f9a", "http://desktop:8033/pb-3f9a/" — into an
// address the instance answers on. Commands are POSTed to the instance's own
// path, which is the same endpoint the web UI's own page posts to.
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

// ---------------------------------------------------------------------------
// Key layout
//
// Widths are in units, 1u being one alphanumeric key: a standard full-size ANSI
// board of 104 keys — function row, main block, navigation cluster and numeric
// keypad.
//
// Keys send the *physical* key name rather than the character it would produce,
// so the target machine applies its own layout — the same choice the web UI
// makes.
// ---------------------------------------------------------------------------

type keyKind uint8

const (
	kindNormal  keyKind = iota
	kindMod             // latches a modifier instead of sending on its own
	kindNumLock         // toggles what the keypad sends, locally
	kindFn              // latches fn on a Mac target; a plain key elsewhere
	kindGap             // blank filler, not a key at all
)

type keyDef struct {
	w, h      float32
	kind      keyKind
	face      string // label with nothing latched
	shiftFace string // label while SHIFT is latched; empty means unchanged
	send      string // API key name
	mod       string // the modifier a kindMod key latches
	numFace   string // keypad label with Num Lock off
	numSend   string // API key name with Num Lock off
	macFace   string // label when the target is a Mac
	macMod    string // the modifier a kindMod key latches on a Mac target
	macSend   string // API key name when the target is a Mac; empty means unchanged
	macOnly   bool   // a key only a Mac board has; drawn as empty space otherwise
	fnFace    string // label while fn is latched on a Mac target
	fnSend    string // consumer-control name sent while fn is latched
}

func gp(w float32) keyDef         { return keyDef{w: w, h: 1, kind: kindGap} }
func k1(face, send string) keyDef { return keyDef{w: 1, h: 1, face: face, send: send} }
func ltr(c string) keyDef         { return k1(c, c) }

func kw(w float32, face, send string) keyDef {
	return keyDef{w: w, h: 1, face: face, send: send}
}

func sym(face, shifted, send string) keyDef {
	return keyDef{w: 1, h: 1, face: face, shiftFace: shifted, send: send}
}

func symW(w float32, face, shifted, send string) keyDef {
	return keyDef{w: w, h: 1, face: face, shiftFace: shifted, send: send}
}

func md(w float32, face, mod string) keyDef {
	return keyDef{w: w, h: 1, kind: kindMod, face: face, mod: mod}
}

// pad builds a keypad key with both of its faces. The API has no keypad key
// names, so each one sends either a digit or the navigation key printed on it —
// which is what Num Lock picks between on a real board too.
func pad(w float32, face, send, numFace, numSend string) keyDef {
	return keyDef{w: w, h: 1, face: face, send: send, numFace: numFace, numSend: numSend}
}

func tall(d keyDef) keyDef { d.h = 2; return d }

// fk builds a function-row key. On a Mac target with fn latched it takes on
// the feature Apple prints on that keycap, sent as a consumer-control code —
// a Mac ignores a plain F1 keycode's brightness ambitions, but honours the
// media usage a real keyboard's brightness key sends.
func fk(n, fnFace, fnSend string) keyDef {
	return keyDef{w: 1, h: 1, face: n, send: n, fnFace: fnFace, fnSend: fnSend}
}

// mk builds a key a Mac both labels and sends differently: the three keys
// right of the function row are prtsc/scrlk/pause on a PC, but a Mac has no
// such keys — Apple puts F13-F15 there instead.
func mk(face, send, macName string) keyDef {
	return keyDef{w: 1, h: 1, face: face, send: send, macFace: macName, macSend: macName}
}

// macF builds one of the F16-F19 keys an Apple full-size board carries above
// the keypad. A PC board has nothing there, so on other targets the key
// disappears into the gap it fills.
func macF(n string) keyDef {
	return keyDef{w: 1, h: 1, face: n, send: n, macOnly: true}
}

// mdSwap builds one of the modifier keys flanking the space bar. A PC and a Mac
// label these differently and — more to the point — put them in a different
// order, so the same keycap takes on a different identity per target: reading
// ctrl-win-alt on a PC and ctrl-⌥-⌘ on a Mac, each in the order it really has.
func mdSwap(w float32, face, mod, macFace, macMod string) keyDef {
	return keyDef{w: w, h: 1, kind: kindMod, face: face, mod: mod, macFace: macFace, macMod: macMod}
}

var keyRows = [][]keyDef{
	{
		// The fn faces follow Apple's current function row. F6 stays F6: its
		// Focus feature has no consumer-control usage a third-party keyboard
		// can send (Apple puts it on the Generic Desktop page).
		k1("esc", "ESCAPE"), gp(1),
		fk("F1", "bri-", "BRIGHTNESS_DOWN"), fk("F2", "bri+", "BRIGHTNESS_UP"),
		fk("F3", "mctl", "MISSION_CONTROL"), fk("F4", "spot", "SPOTLIGHT"), gp(0.5),
		fk("F5", "dict", "DICTATION"), k1("F6", "F6"),
		fk("F7", "prev", "PREV_TRACK"), fk("F8", "play", "PLAY_PAUSE"), gp(0.5),
		fk("F9", "next", "NEXT_TRACK"), fk("F10", "mute", "MUTE"),
		fk("F11", "vol-", "VOLUME_DOWN"), fk("F12", "vol+", "VOLUME_UP"), gp(0.5),
		mk("prtsc", "PRINT_SCREEN", "F13"), mk("scrlk", "SCROLL_LOCK", "F14"), mk("pause", "PAUSE", "F15"),
		gp(0.5),
		macF("F16"), macF("F17"), macF("F18"), macF("F19"),
	},
	{}, // an empty row is vertical breathing room
	{
		sym("`", "~", "GRAVE_ACCENT"),
		sym("1", "!", "1"), sym("2", "@", "2"), sym("3", "#", "3"), sym("4", "$", "4"),
		sym("5", "%", "5"), sym("6", "^", "6"), sym("7", "&", "7"), sym("8", "*", "8"),
		sym("9", "(", "9"), sym("0", ")", "0"),
		sym("-", "_", "MINUS"), sym("=", "+", "EQUALS"),
		kw(2, "⌫", "BACKSPACE"),
		gp(0.5),
		k1("ins", "INSERT"), k1("home", "HOME"), k1("pgup", "PAGE_UP"),
		gp(0.5),
		{w: 1, h: 1, kind: kindNumLock, face: "num"},
		k1("/", "FORWARD_SLASH"),
		k1("*", "*"), // the API maps "*" to shift-8, which is what a keypad * types
		k1("-", "MINUS"),
	},
	{
		kw(1.5, "tab", "TAB"),
		ltr("q"), ltr("w"), ltr("e"), ltr("r"), ltr("t"), ltr("y"), ltr("u"), ltr("i"), ltr("o"), ltr("p"),
		sym("[", "{", "LEFT_BRACKET"), sym("]", "}", "RIGHT_BRACKET"),
		symW(1.5, "\\", "|", "BACKSLASH"),
		gp(0.5),
		k1("del", "DELETE"), k1("end", "END"), k1("pgdn", "PAGE_DOWN"),
		gp(0.5),
		pad(1, "7", "7", "home", "HOME"), pad(1, "8", "8", "↑", "UP"), pad(1, "9", "9", "pgup", "PAGE_UP"),
		tall(k1("+", "SHIFT+EQUALS")),
	},
	{
		kw(1.75, "caps", "CAPS_LOCK"),
		ltr("a"), ltr("s"), ltr("d"), ltr("f"), ltr("g"), ltr("h"), ltr("j"), ltr("k"), ltr("l"),
		sym(";", ":", "SEMICOLON"), sym("'", "\"", "QUOTE"),
		kw(2.25, "enter", "ENTER"),
		gp(0.5), gp(3), gp(0.5), // the navigation cluster has no middle row
		pad(1, "4", "4", "←", "LEFT"), k1("5", "5"), pad(1, "6", "6", "→", "RIGHT"),
		gp(1), // the keypad + reaches down from the row above
	},
	{
		md(2.25, "shift", "SHIFT"),
		ltr("z"), ltr("x"), ltr("c"), ltr("v"), ltr("b"), ltr("n"), ltr("m"),
		sym(",", "<", "COMMA"), sym(".", ">", "PERIOD"), sym("/", "?", "FORWARD_SLASH"),
		md(2.75, "shift", "SHIFT"),
		gp(0.5),
		gp(1), k1("↑", "UP"), gp(1),
		gp(0.5),
		pad(1, "1", "1", "end", "END"), pad(1, "2", "2", "↓", "DOWN"), pad(1, "3", "3", "pgdn", "PAGE_DOWN"),
		tall(k1("enter", "ENTER")),
	},
	{
		md(1.25, "ctrl", "CTRL"),
		mdSwap(1.25, "win", "GUI", "⌥", "ALT"), mdSwap(1.25, "alt", "ALT", "⌘", "GUI"),
		kw(6.25, "space", "SPACE"),
		mdSwap(1.25, "alt", "ALT", "⌘", "GUI"), mdSwap(1.25, "win", "GUI", "⌥", "ALT"),
		// The context-menu key, spelled out rather than drawn as ▤, which most
		// fonts don't carry and renders as a blank box. A Mac has fn in this
		// spot instead, which latches locally rather than sending anything.
		{w: 1.25, h: 1, kind: kindFn, face: "menu", send: "APPLICATION", macFace: "fn"},
		md(1.25, "ctrl", "CTRL"),
		gp(0.5),
		k1("←", "LEFT"), k1("↓", "DOWN"), k1("→", "RIGHT"),
		gp(0.5),
		pad(2, "0", "0", "ins", "INSERT"), pad(1, ".", "PERIOD", "del", "DELETE"),
		gp(1), // the keypad Enter reaches down from the row above
	},
}

// rowGapUnits is how tall an empty row in keyRows is.
const rowGapUnits = 0.4

// minKeyUnit keeps the smallest the board can shrink to still legible.
const minKeyUnit = 27

// capInset is how far a keycap is drawn inside its cell, which is what makes the
// gap between neighbouring keys. The trackpad uses it too, so the board and the
// pad sit the same distance inside the window.
const capInset = 1.5

// marginUnits is the space left around the board, measured in the same units as
// the keys. Keeping it in units rather than pixels means it scales with them —
// and, more to the point, that the window has one shape at every size, which is
// what lets the window server hold a resize drag to it.
const marginUnits = 0.2

// ---------------------------------------------------------------------------
// Keyboard state
// ---------------------------------------------------------------------------

// modOrder is the order modifiers go into a chord — the same order the web UI
// uses, so both clients produce identical command strings.
var modOrder = []string{"CTRL", "ALT", "SHIFT", "GUI"}

// Latch levels for a modifier key.
const (
	latchOff    = 0
	latchSticky = 1 // applies to the next key only
	latchLocked = 2 // stays down until clicked off
)

type keyboardUI struct {
	snd     *sender
	keys    []*keyCap
	mods    map[string]int  // API modifier name -> latch level
	held    map[string]bool // API key names held down on the real keyboard
	numLock bool
	fn      int  // fn latch level; only a Mac target has an fn key to latch
	mac     bool // the target machine is a Mac, which relabels the modifiers
	// settingsOpen holds the real keyboard back while the settings dialog is up,
	// so what gets typed there fills the settings fields instead of being
	// forwarded.
	settingsOpen bool
}

// modLevel reports how a modifier is currently held. One held on the real
// keyboard counts as locked, so a chord typed there lands whole.
func (ui *keyboardUI) modLevel(name string) int {
	if ui.held[name] {
		return latchLocked
	}
	return ui.mods[name]
}

// modOf is the modifier a key latches, which for the keys flanking the space
// bar depends on which target the board is plugged into.
func (ui *keyboardUI) modOf(d keyDef) string {
	if ui.mac && d.macMod != "" {
		return d.macMod
	}
	return d.mod
}

// fnActive reports whether the fn latch is on. It can only be on a Mac
// target: switching targets clears it along with the key itself.
func (ui *keyboardUI) fnActive() bool {
	return ui.mac && ui.fn != latchOff
}

// hides reports whether this key isn't on the target's board at all —
// F16-F19 fill what is empty space on a PC layout.
func (ui *keyboardUI) hides(d keyDef) bool {
	return d.macOnly && !ui.mac
}

func (ui *keyboardUI) refreshKeys() {
	for _, k := range ui.keys {
		k.Refresh()
	}
}

func (ui *keyboardUI) press(d keyDef) {
	if ui.hides(d) {
		return
	}
	switch d.kind {
	case kindMod:
		// Off -> sticky (clears after the next key) -> locked -> off.
		m := ui.modOf(d)
		ui.mods[m] = (ui.mods[m] + 1) % 3
		ui.refreshKeys()
		return
	case kindNumLock:
		ui.numLock = !ui.numLock
		ui.refreshKeys()
		return
	case kindFn:
		if !ui.mac {
			ui.sendChord(d.send) // the menu key it is on a PC
			return
		}
		// The same off -> sticky -> locked round the modifiers make, though
		// fn never goes out on the wire: it only picks what the F row sends.
		ui.fn = (ui.fn + 1) % 3
		ui.refreshKeys()
		return
	}

	// An fn-latched function key sends its consumer-control feature instead
	// of a keycode. Modifiers don't apply — the consumer report has no room
	// for them, and shift-volume means nothing to the target — but a sticky
	// one is still spent, this being the key it was waiting for.
	if ui.fnActive() && d.fnSend != "" {
		ui.snd.enqueue("consumer=" + d.fnSend)
		ui.clearSticky()
		return
	}

	send := d.send
	if ui.mac && d.macSend != "" {
		send = d.macSend
	}
	if !ui.numLock && d.numSend != "" {
		send = d.numSend
	}
	ui.sendChord(send)
}

// sendChord sends one key together with every modifier currently latched, as a
// single "+"-joined chord — held at once is what makes a shortcut register as a
// shortcut rather than as separate keystrokes.
func (ui *keyboardUI) sendChord(key string) {
	if key == "" {
		return
	}
	parts := make([]string, 0, len(modOrder)+1)
	for _, m := range modOrder {
		if ui.modLevel(m) != latchOff {
			parts = append(parts, m)
		}
	}
	parts = append(parts, key)
	ui.snd.enqueue("keycode=" + strings.Join(parts, "+"))
	ui.clearSticky()
}

// clearSticky spends the sticky latches: a sticky modifier — and a sticky fn —
// applies to one key only, while a locked one stays down.
func (ui *keyboardUI) clearSticky() {
	cleared := false
	for m, level := range ui.mods {
		if level == latchSticky {
			ui.mods[m] = latchOff
			cleared = true
		}
	}
	if ui.fn == latchSticky {
		ui.fn = latchOff
		cleared = true
	}
	if cleared {
		ui.refreshKeys()
	}
}

// ---------------------------------------------------------------------------
// The real keyboard
//
// While the window has focus, keys pressed on the actual keyboard go to the
// target as well, and the matching keycap draws itself held so you can see what
// went. Fyne only routes canvas key events while nothing is focused, which is
// what makes the settings dialog's own text field behave normally. Key repeat
// never reaches these handlers either, so a held key can't flood the target.
// ---------------------------------------------------------------------------

var physModName = map[fyne.KeyName]string{
	desktop.KeyShiftLeft:    "SHIFT",
	desktop.KeyShiftRight:   "SHIFT",
	desktop.KeyControlLeft:  "CTRL",
	desktop.KeyControlRight: "CTRL",
	desktop.KeyAltLeft:      "ALT",
	desktop.KeyAltRight:     "ALT",
	desktop.KeySuperLeft:    "GUI",
	desktop.KeySuperRight:   "GUI",
}

var physKeyName = map[fyne.KeyName]string{
	fyne.KeyA: "a", fyne.KeyB: "b", fyne.KeyC: "c", fyne.KeyD: "d", fyne.KeyE: "e",
	fyne.KeyF: "f", fyne.KeyG: "g", fyne.KeyH: "h", fyne.KeyI: "i", fyne.KeyJ: "j",
	fyne.KeyK: "k", fyne.KeyL: "l", fyne.KeyM: "m", fyne.KeyN: "n", fyne.KeyO: "o",
	fyne.KeyP: "p", fyne.KeyQ: "q", fyne.KeyR: "r", fyne.KeyS: "s", fyne.KeyT: "t",
	fyne.KeyU: "u", fyne.KeyV: "v", fyne.KeyW: "w", fyne.KeyX: "x", fyne.KeyY: "y",
	fyne.KeyZ: "z",

	fyne.Key0: "0", fyne.Key1: "1", fyne.Key2: "2", fyne.Key3: "3", fyne.Key4: "4",
	fyne.Key5: "5", fyne.Key6: "6", fyne.Key7: "7", fyne.Key8: "8", fyne.Key9: "9",

	fyne.KeyF1: "F1", fyne.KeyF2: "F2", fyne.KeyF3: "F3", fyne.KeyF4: "F4",
	fyne.KeyF5: "F5", fyne.KeyF6: "F6", fyne.KeyF7: "F7", fyne.KeyF8: "F8",
	fyne.KeyF9: "F9", fyne.KeyF10: "F10", fyne.KeyF11: "F11", fyne.KeyF12: "F12",

	fyne.KeyReturn: "ENTER", fyne.KeyEnter: "ENTER", fyne.KeyTab: "TAB",
	fyne.KeySpace: "SPACE", fyne.KeyBackspace: "BACKSPACE", fyne.KeyDelete: "DELETE",
	fyne.KeyEscape: "ESCAPE", fyne.KeyInsert: "INSERT", fyne.KeyHome: "HOME",
	fyne.KeyEnd: "END", fyne.KeyPageUp: "PAGE_UP", fyne.KeyPageDown: "PAGE_DOWN",
	fyne.KeyUp: "UP", fyne.KeyDown: "DOWN", fyne.KeyLeft: "LEFT", fyne.KeyRight: "RIGHT",

	fyne.KeyMinus: "MINUS", fyne.KeyEqual: "EQUALS",
	fyne.KeyLeftBracket: "LEFT_BRACKET", fyne.KeyRightBracket: "RIGHT_BRACKET",
	fyne.KeyBackslash: "BACKSLASH", fyne.KeySemicolon: "SEMICOLON",
	fyne.KeyApostrophe: "QUOTE", fyne.KeyBackTick: "GRAVE_ACCENT",
	fyne.KeyComma: "COMMA", fyne.KeyPeriod: "PERIOD", fyne.KeySlash: "FORWARD_SLASH",
	fyne.KeyAsterisk: "*", fyne.KeyPlus: "SHIFT+EQUALS",

	desktop.KeyCapsLock:    "CAPS_LOCK",
	desktop.KeyPrintScreen: "PRINT_SCREEN",
	desktop.KeyMenu:        "APPLICATION",
}

func (ui *keyboardUI) physKeyDown(e *fyne.KeyEvent) {
	if ui.settingsOpen {
		return
	}
	// A modifier only marks itself held; it goes out as part of the next key's
	// chord, which is what makes a shortcut arrive as a shortcut.
	if m, ok := physModName[e.Name]; ok {
		ui.held[m] = true
		ui.refreshKeys()
		return
	}
	name, ok := physKeyName[e.Name]
	if !ok {
		return
	}
	ui.held[name] = true
	ui.sendChord(name)
	ui.refreshKeys()
}

func (ui *keyboardUI) physKeyUp(e *fyne.KeyEvent) {
	if ui.settingsOpen {
		return
	}
	name, ok := physModName[e.Name]
	if !ok {
		if name, ok = physKeyName[e.Name]; !ok {
			return
		}
	}
	delete(ui.held, name)
	ui.refreshKeys()
}

// releaseAllPhysical clears the held keys. Losing focus mid-keypress means the
// key-up never arrives, and the cap would stay drawn as held for good.
func (ui *keyboardUI) releaseAllPhysical() {
	if len(ui.held) == 0 {
		return
	}
	clear(ui.held)
	ui.refreshKeys()
}

// ---------------------------------------------------------------------------
// Key widget
// ---------------------------------------------------------------------------

type keyCap struct {
	widget.BaseWidget
	ui  *keyboardUI
	def keyDef

	hovered bool
	down    bool
}

func newKeyCap(ui *keyboardUI, def keyDef) *keyCap {
	c := &keyCap{ui: ui, def: def}
	c.ExtendBaseWidget(c)
	return c
}

// face is the label to draw, which depends on what is currently latched.
func (c *keyCap) face() string {
	if c.def.numFace != "" && !c.ui.numLock {
		return c.def.numFace
	}
	if c.def.fnFace != "" && c.ui.fnActive() {
		return c.def.fnFace
	}
	if c.def.shiftFace != "" && c.ui.modLevel("SHIFT") != latchOff {
		return c.def.shiftFace
	}
	if c.def.macFace != "" && c.ui.mac {
		return c.def.macFace
	}
	return c.def.face
}

// sends is the API key name this cap would send as things stand.
func (c *keyCap) sends() string {
	if c.def.kind == kindMod {
		return c.ui.modOf(c.def)
	}
	if c.def.kind == kindFn {
		if c.ui.mac {
			return "" // fn latches locally; nothing goes out
		}
		return c.def.send
	}
	if !c.ui.numLock && c.def.numSend != "" {
		return c.def.numSend
	}
	if c.ui.mac && c.def.macSend != "" {
		return c.def.macSend
	}
	return c.def.send
}

// pressed reports whether the key is being held right now, by the mouse on the
// cap or by the matching key on the real keyboard.
func (c *keyCap) pressed() bool {
	if c.down {
		return true
	}
	name := c.sends()
	return name != "" && c.ui.held[name]
}

// latch is the highlight level to draw this key at. Only modifiers and fn
// latch: Num Lock already shows in the legends on the keypad itself.
func (c *keyCap) latch() int {
	if c.def.kind == kindMod {
		return c.ui.mods[c.ui.modOf(c.def)]
	}
	if c.def.kind == kindFn && c.ui.mac {
		return c.ui.fn
	}
	return latchOff
}

func (c *keyCap) Tapped(*fyne.PointEvent)        { c.ui.press(c.def) }
func (c *keyCap) MouseDown(*desktop.MouseEvent)  { c.down = true; c.Refresh() }
func (c *keyCap) MouseUp(*desktop.MouseEvent)    { c.down = false; c.Refresh() }
func (c *keyCap) MouseIn(*desktop.MouseEvent)    { c.hovered = true; c.Refresh() }
func (c *keyCap) MouseMoved(*desktop.MouseEvent) {}

func (c *keyCap) Cursor() desktop.Cursor {
	if c.ui.hides(c.def) {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// MouseOut clears the pressed look as well as the hover: a button released over
// a different key never sends this one its MouseUp, and the keycap would sit
// there looking held down.
func (c *keyCap) MouseOut() {
	c.hovered, c.down = false, false
	c.Refresh()
}

func (c *keyCap) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 4
	bg.StrokeWidth = 1
	label := canvas.NewText("", color.Black)
	label.Alignment = fyne.TextAlignCenter
	r := &keyCapRenderer{cap: c, bg: bg, label: label}
	r.Refresh()
	return r
}

type keyCapRenderer struct {
	cap   *keyCap
	bg    *canvas.Rectangle
	label *canvas.Text
}

func (r *keyCapRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(capInset, capInset))
	r.bg.Resize(fyne.NewSize(size.Width-2*capInset, size.Height-2*capInset))

	// Scale the legend with the key so the board stays readable at any window
	// size, but keep it inside the range the font actually looks right at.
	textSize := clampF(size.Height*0.3, 8, 13)
	// Then shrink it if the word would overrun a narrow key — the keypad's
	// "enter" is a whole word on a one-unit cap.
	avail := size.Width - 2*capInset - 2
	for textSize > 5 && fyne.MeasureText(r.label.Text, textSize, r.label.TextStyle).Width > avail {
		textSize -= 0.5
	}
	r.label.TextSize = textSize
	h := r.label.MinSize().Height
	r.label.Resize(fyne.NewSize(size.Width, h))
	r.label.Move(fyne.NewPos(0, (size.Height-h)/2))
}

func (r *keyCapRenderer) MinSize() fyne.Size {
	return fyne.NewSize(minKeyUnit*r.cap.def.w, minKeyUnit*r.cap.def.h)
}

func (r *keyCapRenderer) Refresh() {
	c := r.cap
	// A key the target's board doesn't have draws as the empty space it
	// occupies on that layout — still here, just invisible and inert.
	if c.ui.hides(c.def) {
		r.bg.FillColor, r.bg.StrokeWidth = color.Transparent, 0
		r.label.Text = ""
		r.bg.Refresh()
		r.label.Refresh()
		return
	}
	base := theme.Color(theme.ColorNameButton)
	ink := theme.Color(theme.ColorNameForeground)
	// The states are told apart by depth of grey rather than by hue, so the
	// whole board stays monochrome. Contrasting text comes from the page
	// colour, which inverts with the theme just as the ink does.
	fill, fg := base, ink
	stroke, strokeWidth := theme.Color(theme.ColorNameInputBorder), float32(1)

	switch {
	case c.pressed():
		fill = blend(base, ink, 0.3)
	case c.latch() == latchLocked:
		// Locked reads as the darkest, and outlined, so sticky is tellable.
		fill, fg = blend(base, ink, 0.62), theme.Color(theme.ColorNameBackground)
		stroke, strokeWidth = ink, 2
	case c.latch() == latchSticky:
		fill, fg = blend(base, ink, 0.45), theme.Color(theme.ColorNameBackground)
	case c.hovered:
		fill = blend(base, ink, 0.1)
	}

	r.bg.FillColor, r.bg.StrokeColor, r.bg.StrokeWidth = fill, stroke, strokeWidth
	r.label.Text, r.label.Color = c.face(), fg
	// A latch can swap the legend for a longer one, so re-fit it to the cap
	// before drawing rather than only when the window resizes.
	if sz := c.Size(); sz.Width > 0 {
		r.Layout(sz)
	}
	r.bg.Refresh()
	r.label.Refresh()
}

func (r *keyCapRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.label} }
func (r *keyCapRenderer) Destroy()                     {}

// ---------------------------------------------------------------------------
// Desk panel
//
// The keys are laid out by hand in unit space rather than with a grid: a
// full-size board has half-unit offsets, keys 1.25u to 6.25u wide and two keys
// that are 2u tall, none of which a row-and-column layout expresses. Placing
// them from one unit size also means the whole board scales with the window.
//
// The trackpad rides in that same grid, off the right-hand end. Sizing it in
// units is what keeps it exactly square and exactly as tall as the board at
// every window size, with no divider to drag out of true.
// ---------------------------------------------------------------------------

// padGapUnits is the space between the right edge of the board and the pad.
const padGapUnits = 0.6

type placedKey struct {
	cap        *keyCap
	x, y, w, h float32 // in units
}

type keyboardPanel struct {
	widget.BaseWidget
	placed     []placedKey
	pad        *trackpad
	cols, rows float32
	padSide    float32 // in units; the pad is square, so this is both dimensions

	// fit asks for the window to be reshaped, last is the size this panel was
	// given before now, and asked is the reshape already in flight. Together
	// they keep the window the same shape as the board, so dragging one edge
	// moves the other with it. All three are only touched from the layout,
	// which runs on the UI thread.
	fit   func(fyne.Size)
	last  fyne.Size
	asked fyne.Size
}

// outer is the whole panel in units: the board plus the margin around it.
func (p *keyboardPanel) outer() (cols, rows float32) {
	return p.cols + 2*marginUnits, p.rows + 2*marginUnits
}

func newKeyboardPanel(ui *keyboardUI, pad *trackpad) *keyboardPanel {
	p := &keyboardPanel{pad: pad}
	y := float32(0)
	for _, row := range keyRows {
		if len(row) == 0 {
			y += rowGapUnits
			continue
		}
		x := float32(0)
		for _, d := range row {
			if d.kind == kindGap {
				x += d.w
				continue
			}
			c := newKeyCap(ui, d)
			ui.keys = append(ui.keys, c)
			p.placed = append(p.placed, placedKey{cap: c, x: x, y: y, w: d.w, h: d.h})
			x += d.w
		}
		if x > p.cols {
			p.cols = x
		}
		y++
	}
	p.rows = y

	// A square pad as tall as the board, sitting off its right-hand end.
	p.padSide = p.rows
	p.cols += padGapUnits + p.padSide

	p.ExtendBaseWidget(p)
	return p
}

func (p *keyboardPanel) CreateRenderer() fyne.WidgetRenderer {
	objects := make([]fyne.CanvasObject, 0, len(p.placed)+1)
	for _, pl := range p.placed {
		objects = append(objects, pl.cap)
	}
	objects = append(objects, p.pad)
	return &keyboardPanelRenderer{panel: p, objects: objects}
}

type keyboardPanelRenderer struct {
	panel   *keyboardPanel
	objects []fyne.CanvasObject
}

func (r *keyboardPanelRenderer) Layout(size fyne.Size) {
	p := r.panel
	p.keepShape(size)
	cols, rows := p.outer()
	unit := size.Width / cols
	if byHeight := size.Height / rows; byHeight < unit {
		unit = byHeight
	}
	// Centre whatever is left over, then step in by the margin.
	offX := (size.Width-cols*unit)/2 + marginUnits*unit
	offY := (size.Height-rows*unit)/2 + marginUnits*unit
	if offY < marginUnits*unit {
		offY = marginUnits * unit
	}
	for _, pl := range p.placed {
		pl.cap.Move(fyne.NewPos(offX+pl.x*unit, offY+pl.y*unit))
		pl.cap.Resize(fyne.NewSize(pl.w*unit, pl.h*unit))
	}

	side := p.padSide * unit
	p.pad.Move(fyne.NewPos(offX+(p.cols-p.padSide)*unit, offY))
	p.pad.Resize(fyne.NewSize(side, side))
}

// keepShape asks the window to match the board's proportions, so a resize in one
// direction brings the other with it instead of leaving a band of empty space.
// The edge that moved furthest is the one taken as intended; the other follows.
func (p *keyboardPanel) keepShape(size fyne.Size) {
	if p.fit == nil || size.Width <= 0 || size.Height <= 0 {
		return
	}
	cols, rows := p.outer()
	previous := p.last
	p.last = size

	// Already the right shape, within a pixel: nothing to do, and this is what
	// stops the reshape below from asking again in an endless round. The request
	// in flight is cleared here too, so a later drag back to a size already asked
	// for once is not mistaken for that same request arriving.
	wantHeight := size.Width / cols * rows
	if math.Abs(float64(size.Height-wantHeight)) <= 1 {
		p.asked = fyne.Size{}
		return
	}

	want := fyne.NewSize(size.Width, wantHeight)
	if math.Abs(float64(size.Height-previous.Height)) > math.Abs(float64(size.Width-previous.Width)) {
		// The height was the edge being dragged, so keep it and follow with the
		// width instead.
		want = fyne.NewSize(size.Height/rows*cols, size.Height)
	}
	if want == p.asked {
		return // already asked for this and the answer is on its way
	}
	p.asked = want

	// Asked for after this layout pass, not during it: resizing the window from
	// inside one leaves Fyne to finish the pass with the size it began with,
	// which puts the board back where it was and undoes the fit.
	fyne.Do(func() { p.fit(want) })
}

func (r *keyboardPanelRenderer) MinSize() fyne.Size {
	cols, rows := r.panel.outer()
	return fyne.NewSize(cols*minKeyUnit, rows*minKeyUnit)
}

func (r *keyboardPanelRenderer) Refresh() {
	for _, o := range r.objects {
		o.Refresh()
	}
}

func (r *keyboardPanelRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *keyboardPanelRenderer) Destroy()                     {}

// ---------------------------------------------------------------------------
// Trackpad
//
// The gestures are the web UI's, from public/index.html: dragging moves the
// pointer, click, right-click and double-click do the obvious thing, and the
// wheel scrolls the target machine. Click and then press again straight away to
// drag on the target itself — the second press grabs and holds the button, so
// motion after it drags whatever is under the pointer.
//
// The clicks are told apart here rather than by Fyne, which cannot say "a press
// arrived inside the double-click window" — and that moment is exactly when the
// button has to go down for a drag to start at the double-click point.
// ---------------------------------------------------------------------------

// Fyne scales raw wheel offsets by a per-platform constant before handing them
// on; divide it back out so one notch here is one notch on the target.
var scrollUnitsPerNotch = func() float32 {
	if runtime.GOOS == "darwin" {
		return 10
	}
	return 25
}()

const (
	minSpeed = 0.25
	maxSpeed = 4
	// Just the four hex digits an id is, and no room for more.
	instanceIDWidth = 46
	// Room for a five-digit port and no more.
	portWidth = 56
	// Wide enough for an IPv4 address, which is what usually goes here.
	hostWidth = 130
	// Wide enough for a full-size board at a comfortable key size; the height
	// that goes with it is worked out from the layout.
	windowWidth = 1240
	// How long a click waits to see whether a second one follows, and how far a
	// gesture may wander and still count as being in one place. Both match the
	// web UI, so the two clients feel the same.
	doubleClickWindow = 300 * time.Millisecond
	clickMoveSlop     = 6
)

type trackpad struct {
	widget.BaseWidget
	snd *sender

	sens float32 // pointer sensitivity multiplier

	// pending is a click waiting out the double-click window, in case a second
	// press follows and turns the gesture into a double-click or a drag.
	pending   *time.Timer
	held      bool      // the board is holding the left button down for us
	pressedAt time.Time // when that press went down, to tell a tap from a hold
	dx, dy    float32   // how far this gesture has travelled, for the tap test
	active    bool
	hovered   bool
}

func newTrackpad(snd *sender, sens float32) *trackpad {
	t := &trackpad{snd: snd, sens: sens}
	t.ExtendBaseWidget(t)
	return t
}

func (t *trackpad) MouseDown(e *desktop.MouseEvent) {
	t.active = true
	t.dx, t.dy = 0, 0
	if e.Button == desktop.MouseButtonPrimary {
		t.claimPendingClick()
	}
	t.Refresh()
}

// claimPendingClick turns a click still waiting out the double-click window into
// a held button. Pressing the moment the second press lands is what makes the
// grab happen at the double-click point; waiting for movement instead would lose
// the start of a quick drag to the tap test below.
func (t *trackpad) claimPendingClick() {
	if t.pending == nil {
		return
	}
	claimed := t.pending.Stop()
	t.pending = nil // whether or not it fired, it is spent either way
	if !claimed {
		return // the window had passed and it went out as a plain click
	}
	t.held = true
	t.pressedAt = time.Now()
	t.snd.mouseCmd("mouse=PRESS(0,0)")
}

func (t *trackpad) MouseUp(e *desktop.MouseEvent) {
	t.active = false
	defer t.Refresh()

	if t.held {
		t.held = false
		// A quick lift in place says it was a double-click after all. The button
		// is still down, so DOUBLE_CLICK's two press/release pairs reach the
		// target as up, down, up — pressing a held button is a no-op — which
		// completes the double-click well inside the time it allows.
		if time.Since(t.pressedAt) < doubleClickWindow && t.inOnePlace() {
			t.snd.mouseCmd("mouse=DOUBLE_CLICK(0,0)")
		} else {
			t.snd.mouseCmd("mouse=RELEASE(0,0)")
		}
		return
	}

	if !t.inOnePlace() {
		return // the gesture moved the pointer, so it was never a click
	}
	switch e.Button {
	case desktop.MouseButtonSecondary:
		t.snd.mouseCmd("mouse=RIGHT_CLICK(0,0)")
	case desktop.MouseButtonPrimary:
		// Held back: a press inside the window makes this a double-click, or the
		// beginning of a drag. The timer talks straight to the sender, which any
		// goroutine may do — handing the work to the UI thread instead would
		// leave the click sitting there until the next event happened to arrive,
		// and an idle window has none.
		t.pending = time.AfterFunc(doubleClickWindow, func() {
			t.snd.mouseCmd("mouse=CLICK(0,0)")
		})
	}
}

// inOnePlace reports whether the gesture has stayed put, within the slop a hand
// on a mouse cannot help but add.
func (t *trackpad) inOnePlace() bool {
	return math.Hypot(float64(t.dx), float64(t.dy)) <= clickMoveSlop
}

func (t *trackpad) MouseIn(*desktop.MouseEvent)    { t.hovered = true; t.Refresh() }
func (t *trackpad) MouseMoved(*desktop.MouseEvent) {}
func (t *trackpad) MouseOut()                      { t.hovered = false; t.Refresh() }
func (t *trackpad) Cursor() desktop.Cursor         { return desktop.CrosshairCursor }

func (t *trackpad) Dragged(e *fyne.DragEvent) {
	t.dx += e.Dragged.DX
	t.dy += e.Dragged.DY
	if t.held && t.inOnePlace() {
		// Wobble under a press that hasn't really gone anywhere is held back:
		// this may yet end as a double-click, which the target voids if the
		// pointer strays between the two clicks. Once the slop is passed the
		// event flows through whole, so a quick drag loses none of its motion.
		return
	}
	t.snd.moveBy(e.Dragged.DX*t.sens, e.Dragged.DY*t.sens)
}

// DragEnd only tidies up the look. The button, if one is held, is let go in
// MouseUp — which Fyne delivers first, and delivers whether a drag happened.
func (t *trackpad) DragEnd() {
	t.active = false
	t.Refresh()
}

// releaseHeld lets a held button go. Losing focus mid-drag means no mouse-up
// ever arrives, and the target would be left holding its button down.
func (t *trackpad) releaseHeld() {
	if !t.held {
		return
	}
	t.held = false
	t.snd.mouseCmd("mouse=RELEASE(0,0)")
}

func (t *trackpad) Scrolled(e *fyne.ScrollEvent) {
	t.snd.scrollBy(e.Scrolled.DY / scrollUnitsPerNotch)
}

func (t *trackpad) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 6
	bg.StrokeWidth = 1
	r := &trackpadRenderer{pad: t, bg: bg}
	r.Refresh()
	return r
}

type trackpadRenderer struct {
	pad *trackpad
	bg  *canvas.Rectangle
}

func (r *trackpadRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(capInset, capInset))
	r.bg.Resize(fyne.NewSize(size.Width-2*capInset, size.Height-2*capInset))
}

func (r *trackpadRenderer) MinSize() fyne.Size { return fyne.NewSize(200, 160) }

func (r *trackpadRenderer) Refresh() {
	fill := theme.Color(theme.ColorNameInputBackground)
	switch {
	case r.pad.held:
		// The target's button is down: make that impossible to miss, since
		// everything the pointer touches is being dragged.
		fill = blend(fill, theme.Color(theme.ColorNamePrimary), 0.35)
	case r.pad.active:
		fill = blend(fill, theme.Color(theme.ColorNameForeground), 0.1)
	case r.pad.hovered:
		fill = blend(fill, theme.Color(theme.ColorNameForeground), 0.05)
	}
	r.bg.FillColor = fill
	r.bg.StrokeColor = theme.Color(theme.ColorNameInputBorder)
	r.bg.Refresh()
}

func (r *trackpadRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg} }
func (r *trackpadRenderer) Destroy()                     {}

// ---------------------------------------------------------------------------
// Theme
// ---------------------------------------------------------------------------

// monoTheme takes the colour out of the standard theme. The keycaps say what
// they mean with depth of grey, so a blue radio button or slider in the settings
// dialog would be the only colour in the app — and even the standard greys carry
// a faint blue cast.
type monoTheme struct {
	fyne.Theme
}

func (t monoTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameError, theme.ColorNameWarning, theme.ColorNameSuccess,
		theme.ColorNameForegroundOnError, theme.ColorNameForegroundOnWarning,
		theme.ColorNameForegroundOnSuccess:
		// The status colours stay: a failed command has to read as a failure.
		return t.Theme.Color(name, v)
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameSelection,
		theme.ColorNameHyperlink, theme.ColorNamePressed:
		// Ink instead of accent, so a selected control still reads as selected.
		return desaturate(t.Theme.Color(theme.ColorNameForeground, v))
	case theme.ColorNameForegroundOnPrimary:
		return desaturate(t.Theme.Color(theme.ColorNameBackground, v))
	}
	return desaturate(t.Theme.Color(name, v))
}

// slimTheme draws its contents smaller. It exists to thin down the pointer-speed
// slider, whose thumb comes from a size the radio buttons share — turning that
// down for the whole app would shrink them too.
type slimTheme struct {
	fyne.Theme
}

func (t slimTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInlineIcon:
		return t.Theme.Size(name) * 0.85 // the thumb
	case theme.SizeNameInnerPadding:
		// This padding is what sets the track in from the widget's edge, and a
		// slider also insets its ends by half a thumb so the thumb can sit
		// centred at either extreme. Tuned so the track starts where the values
		// on the rows above it do.
		return t.Theme.Size(name) * 0.55
	}
	return t.Theme.Size(name)
}

// desaturate drops the hue from a colour while keeping how light and how
// transparent it is.
func desaturate(c color.Color) color.Color {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	// Rec. 601 luma, which tracks how light the eye reads each channel.
	y := uint8((299*int(n.R) + 587*int(n.G) + 114*int(n.B)) / 1000)
	return color.NRGBA{R: y, G: y, B: y, A: n.A}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// blend mixes b into a by t (0..1), so key faces can be tinted without
// hard-coding colours that would clash with whichever theme is active.
func blend(a, b color.Color, t float32) color.Color {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	mix := func(x, y uint32) uint8 {
		return uint8((float32(x)*(1-t) + float32(y)*t) / 257)
	}
	return color.NRGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: mix(aa, ba)}
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Assembly
// ---------------------------------------------------------------------------

func main() {
	urlFlag := flag.String("url", "", "instance address, e.g. http://192.168.1.40:8033/pb-3f9a")
	flag.Parse()

	// A .env beside the app is read the same way Pob's own clients read theirs.
	// Missing is fine: the address can come from the flag, the environment, or
	// the settings dialog, and the dialog remembers it.
	_ = godotenv.Load()

	// Every widget update from the sender goroutine goes through fyne.Do, so
	// declare that rather than run under Fyne's compatibility shim.
	app.SetMetadata(fyne.AppMetadata{
		ID:         appID,
		Name:       appName,
		Migrations: map[string]bool{"fyneDo": true},
	})

	a := app.NewWithID(appID)
	a.Settings().SetTheme(monoTheme{Theme: theme.DefaultTheme()})
	w := a.NewWindow(appName)
	prefs := a.Preferences()

	// A flag or the environment gives the whole address at once; the settings
	// keep its three parts separately, which is what makes each of them
	// editable on its own.
	address := firstNonEmpty(*urlFlag, os.Getenv("POB_SERVER_URL"),
		instanceURL(prefs.String(prefInstanceHost),
			prefs.IntWithFallback(prefPort, defaultPort),
			prefs.String(prefInstanceID)))
	configured := address != ""
	host, port, instance := splitAddress(address)
	snd := newSender(normalizeURL(address))

	// Which machine Pob is running on is anyone's guess, so start from the one
	// running this window and let the setting correct it.
	ui := &keyboardUI{
		snd:     snd,
		mods:    map[string]int{},
		held:    map[string]bool{},
		numLock: true,
		mac:     prefs.StringWithFallback(prefTarget, defaultTarget()) == targetMac,
	}
	pad := newTrackpad(snd, float32(prefs.FloatWithFallback(prefSpeed, 1)))
	keyboard := newKeyboardPanel(ui, pad)

	settings := func() {
		// The address is laid out as its three parts, spelled out in the order
		// they appear in it, so the field being edited is the one that reads
		// where it sits in what gets typed into a browser.
		//
		// An Entry scrolls its text, and the scroller draws a shadow at the edge
		// once it thinks the content overruns the box — which a narrow box
		// always does. None of these can overflow by much, so the scrolling is
		// no loss and the shadow goes with it.
		narrow := func(text, hint string) *widget.Entry {
			entry := widget.NewEntry()
			entry.SetPlaceHolder(hint)
			entry.Wrapping = fyne.TextWrapOff
			entry.Scroll = fyne.ScrollNone
			entry.SetText(text)
			return entry
		}

		// All three fields feed the one address, so each rebuilds it from the set.
		retarget := func() {
			address = instanceURL(host, port, instance)
			snd.setURL(normalizeURL(address))
		}

		hostEntry := narrow(host, hostHint)
		entry := narrow(instance, instanceIDHint)
		portEntry := narrow(strconv.Itoa(port), strconv.Itoa(defaultPort))

		hostEntry.OnChanged = limitToHost(hostEntry, func(h string) {
			host = h
			prefs.SetString(prefInstanceHost, h)
			retarget()
		}, func(h string, p int, id string) {
			// A whole address was pasted in: fill the other two from it rather
			// than leave them contradicting what was just typed.
			host, port, instance = h, p, id
			prefs.SetString(prefInstanceHost, h)
			prefs.SetInt(prefPort, p)
			prefs.SetString(prefInstanceID, id)
			portEntry.SetText(strconv.Itoa(p))
			entry.SetText(id)
			retarget()
		})

		entry.OnChanged = limitToInstanceID(entry, func(id string) {
			instance = id
			prefs.SetString(prefInstanceID, id)
			retarget()
		})

		portEntry.OnChanged = limitToPort(portEntry, func(p int) {
			port = p
			prefs.SetInt(prefPort, p)
			retarget()
		})

		slim := slimTheme{Theme: a.Settings().Theme()}
		shown := widget.NewLabel(fmt.Sprintf("%.2f×", pad.sens))
		speed := widget.NewSlider(minSpeed, maxSpeed)
		speed.Step = 0.25
		speed.Value = float64(pad.sens)
		speed.OnChanged = func(v float64) {
			pad.sens = float32(v)
			prefs.SetFloat(prefSpeed, v)
			shown.SetText(fmt.Sprintf("%.2f×", v))
		}

		// Cosmetic, but not merely so: the keys sent are physical either way, and
		// this is what tells you which keycap next to the space bar is which.
		target := widget.NewRadioGroup([]string{targetWindows, targetMac}, nil)
		target.Horizontal = true
		target.Selected = targetWindows
		if ui.mac {
			target.Selected = targetMac
		}
		target.OnChanged = func(s string) {
			if mac := s == targetMac; mac != ui.mac {
				ui.mac = mac
				ui.fn = latchOff // the key that held it just left the board
				prefs.SetString(prefTarget, s)
				ui.refreshKeys()
			}
		}

		d := dialog.NewCustom("Settings", "Close", widget.NewForm(
			widget.NewFormItem("Instance", container.NewHBox(
				widget.NewLabel("http://"),
				container.NewGridWrap(fyne.NewSize(hostWidth, hostEntry.MinSize().Height), hostEntry),
				widget.NewLabel(":"),
				container.NewGridWrap(fyne.NewSize(portWidth, portEntry.MinSize().Height), portEntry),
				widget.NewLabel("/"+instanceIDPrefix),
				container.NewGridWrap(fyne.NewSize(instanceIDWidth, entry.MinSize().Height), entry),
			)),
			widget.NewFormItem("Target", target),
			// The slider takes the whole row, starting where the fields above it
			// do; only the reading beside it is held back. It is drawn slim
			// rather than short, so length costs the dialog nothing.
			widget.NewFormItem("Pointer speed", container.NewBorder(nil, nil, nil, shown,
				container.NewThemeOverride(speed, slim),
			)),
		), w)
		d.Resize(fyne.NewSize(560, 230))
		ui.settingsOpen = true
		ui.releaseAllPhysical() // nothing stays drawn held while the dialog is up
		d.SetOnClosed(func() {
			ui.settingsOpen = false
			w.Canvas().Unfocus()
			go snd.resolve() // the address is settled now; get the lookup over with
		})
		d.Show()
		// Fyne doesn't focus a dialog's first field, and without focus the
		// address would have nowhere to land.
		w.Canvas().Focus(hostEntry)
	}

	// Settings live in the menu bar, which keeps the window itself to nothing but
	// the keyboard and the pad. The item has to be called exactly "Settings…":
	// macOS then lifts it into the application menu where it belongs and drops
	// the now-empty File menu, while other platforms show File > Settings….
	w.SetMainMenu(fyne.NewMainMenu(
		fyne.NewMenu("File", fyne.NewMenuItem("Settings…", settings)),
	))

	// Keys pressed on the real keyboard go to the target too, and light up the
	// cap they belong to.
	if c, ok := w.Canvas().(desktop.Canvas); ok {
		c.SetOnKeyDown(ui.physKeyDown)
		c.SetOnKeyUp(ui.physKeyUp)
	}
	// A key or a button held as the window loses focus never reports its
	// release: drop the held keys rather than leave a cap stuck looking pressed,
	// and let go of the mouse button rather than leave the target holding it.
	a.Lifecycle().SetOnExitedForeground(func() {
		ui.releaseAllPhysical()
		pad.releaseHeld()
	})

	// No padding container and no window padding: the margin is part of the
	// board's own unit grid, which is what keeps the window to one shape at
	// every size.
	w.SetPadded(false)
	w.SetContent(keyboard)
	// The board keeps its proportions, so its height follows from its width, and
	// the window has to be exactly that tall. Any spare height would otherwise
	// go to fitting the board by height instead, which shrinks it and leaves the
	// slack at the sides.
	// The board has one shape, so ask the window server to hold resizing to it —
	// then a drag scales the whole thing under the pointer. Where it won't, the
	// panel reshapes the window itself after each resize instead, which follows
	// the drag rather than leading it but gets to the same place.
	outerCols, outerRows := keyboard.outer()
	if !holdWindowAspect(outerCols, outerRows) {
		keyboard.fit = func(want fyne.Size) { w.Resize(want) }
	}
	w.Resize(fyne.NewSize(windowWidth, windowWidth/outerCols*outerRows))
	w.CenterOnScreen()

	go snd.run()
	go snd.resolve() // so the first keystroke isn't the one that waits for mDNS

	// The stand-in address resolves nowhere, so on a first run open the settings
	// straight away rather than letting the first keypress fail for a reason
	// that isn't on screen.
	if !configured {
		settings()
	}

	w.ShowAndRun()
}

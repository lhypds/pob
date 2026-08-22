// Package bridge exposes the Swift shell's perception/operation primitives
// (screenshot capture, mouse, keyboard, UI dialogs) as typed Go calls over
// the IPC channel. All coordinates are screenshot pixels; the Swift side owns
// the virtual cursor state and the pixel→screen coordinate conversion.
package bridge

import (
	"encoding/base64"
	"fmt"
	"sync"

	"pob/core/internal/ipc"
)

type Bridge struct {
	ipc *ipc.Client

	mu sync.Mutex
	// driving holds the remote-control sources currently active, so the
	// virtual cursor is hidden only once the last of them has finished.
	driving map[string]bool
}

type Point struct {
	X int
	Y int
}

// CropRect is a crop region in screenshot pixels (top-left origin).
type CropRect struct {
	X, Y, W, H float64
}

func New(client *ipc.Client) *Bridge {
	return &Bridge{ipc: client, driving: map[string]bool{}}
}

func pointFrom(result map[string]any) Point {
	x, _ := result["x"].(float64)
	y, _ := result["y"].(float64)
	return Point{X: int(x), Y: int(y)}
}

// ShotOptions is what to capture and how to encode it.
//
// The defaults are the agent's: a full-size PNG, nothing thrown away, because
// what it does with a frame is read the text in it. A browser watching from
// across the room wants the opposite — the smallest picture that still looks
// like the screen, as fast as the machine can make them — which is what the
// other three fields are for.
type ShotOptions struct {
	WithCursor bool
	Crop       *CropRect
	// Format is "png" or "jpeg"; empty means PNG.
	//
	// PNG on a desktop is the slow choice twice over: it is the heaviest
	// encode of the two by an order of magnitude, and it is lossless about
	// gradients and shadows nobody is reading. JPEG is what makes a watchable
	// frame rate possible at all.
	Format string
	// MaxWidth shrinks the picture to at most this many pixels across before
	// encoding, keeping its shape. 0 leaves it alone. Every step after this
	// one costs by the pixel, so it is the cheapest thing that can be done.
	MaxWidth int
	// Quality is JPEG quality, 1-100. 0 takes the shell's default.
	Quality int
	// KeepWindow lets the shell leave the Pob window on the screen for the
	// grab, at the price of it appearing in the picture.
	//
	// It matters on one platform. macOS captures below the window and Windows
	// marks it excluded, so on both it stays on screen either way and this
	// changes nothing. X11 has neither: the window has to actually go for the
	// duration of the grab, and a stream of frames keeps it gone the whole
	// time the watcher is watching — the window vanishing off its own desktop
	// as the price of a picture without it in.
	//
	// So the caller says which it would rather have. An agent's screenshot
	// must not show Pob's own chrome — it is reading the screen and would read
	// the toolbar as part of it — and pays the disappearance. A frame for
	// someone watching does not care: the window in the corner of the picture
	// is at worst noise, and at best tells them Pob is there.
	KeepWindow bool
}

// Shot is a captured frame and the sizes needed to make sense of it.
type Shot struct {
	Bytes  []byte
	Format string
	// Width and Height are the picture's own size, after any shrinking.
	Width, Height int
	// SourceWidth and SourceHeight are its size in screenshot pixels — the
	// space Pob's coordinates live in. They are the same as Width and Height
	// unless MaxWidth shrank the picture, and a client that turns a click on
	// the picture back into a position on the machine needs the difference:
	// without it every click on a shrunk frame lands short.
	SourceWidth, SourceHeight int
}

// CaptureScreenshot captures the Pob window content area. withCursor draws
// the virtual cursor into the image. Returns raw PNG bytes.
func (b *Bridge) CaptureScreenshot(withCursor bool, crop *CropRect) ([]byte, error) {
	shot, err := b.CaptureShot(ShotOptions{WithCursor: withCursor, Crop: crop})
	if err != nil {
		return nil, err
	}
	return shot.Bytes, nil
}

// CaptureShot captures the Pob window content area to order.
func (b *Bridge) CaptureShot(opts ShotOptions) (Shot, error) {
	format := opts.Format
	if format == "" {
		format = "png"
	}
	params := map[string]any{"withCursor": opts.WithCursor, "format": format}
	if opts.Crop != nil {
		params["crop"] = map[string]any{"x": opts.Crop.X, "y": opts.Crop.Y, "width": opts.Crop.W, "height": opts.Crop.H}
	}
	if opts.MaxWidth > 0 {
		params["maxWidth"] = opts.MaxWidth
	}
	if opts.Quality > 0 {
		params["quality"] = opts.Quality
	}
	// Sent only when asked for, so the agent's capture goes over the wire
	// exactly as it always has and an older shell sees nothing new.
	if opts.KeepWindow {
		params["keepWindow"] = true
	}

	data, result, err := b.ipc.CallFrame("screenshot.capture", params)
	if err != nil {
		return Shot{}, err
	}
	// No bytes means this shell has no frame channel open and answered on the
	// JSON-RPC line, where the picture is base64 in the result.
	if data == nil {
		b64, _ := result["image"].(string)
		if b64 == "" {
			return Shot{}, fmt.Errorf("screenshot capture returned no image")
		}
		if data, err = base64.StdEncoding.DecodeString(b64); err != nil {
			return Shot{}, err
		}
	}

	shot := Shot{Bytes: data, Format: format}
	shot.Width = intFrom(result, "width")
	shot.Height = intFrom(result, "height")
	shot.SourceWidth = intFrom(result, "sourceWidth")
	shot.SourceHeight = intFrom(result, "sourceHeight")
	// An older shell reports no sizes at all. It also does no shrinking, so
	// the picture is its own source and reading the sizes back out of it is
	// the caller's job — leaving them zero says "unknown", which is the
	// honest answer and the one a client can test for.
	return shot, nil
}

func intFrom(result map[string]any, key string) int {
	v, _ := result[key].(float64)
	return int(v)
}

func (b *Bridge) ResetCursor() (Point, error) {
	result, err := b.ipc.Call("cursor.reset", nil)
	return pointFrom(result), err
}

func (b *Bridge) MoveCursor(dx, dy float64) (Point, error) {
	result, err := b.ipc.Call("cursor.move", map[string]any{"dx": dx, "dy": dy})
	return pointFrom(result), err
}

// CursorPosition returns the current virtual cursor position. The shell has no
// separate query for it, so this is a zero-delta move — every platform treats
// cursor.move as position += delta and answers with the result.
func (b *Bridge) CursorPosition() (Point, error) {
	return b.MoveCursor(0, 0)
}

// MoveCursorTo moves the virtual cursor to an absolute screenshot-pixel
// position, translating it into the relative move the shell expects.
func (b *Bridge) MoveCursorTo(x, y float64) (Point, error) {
	cur, err := b.CursorPosition()
	if err != nil {
		return Point{}, err
	}
	return b.MoveCursor(x-float64(cur.X), y-float64(cur.Y))
}

func (b *Bridge) Click() (Point, error) {
	result, err := b.ipc.Call("mouse.click", nil)
	return pointFrom(result), err
}

func (b *Bridge) RightClick() (Point, error) {
	result, err := b.ipc.Call("mouse.rightClick", nil)
	return pointFrom(result), err
}

func (b *Bridge) DoubleClick() (Point, error) {
	result, err := b.ipc.Call("mouse.doubleClick", nil)
	return pointFrom(result), err
}

// Drag drags from the current cursor position by (dx, dy); returns the end position.
func (b *Bridge) Drag(dx, dy float64) (Point, error) {
	result, err := b.ipc.Call("mouse.drag", map[string]any{"dx": dx, "dy": dy})
	return pointFrom(result), err
}

// DragTo drags from the current cursor position to an absolute position;
// returns the end position.
func (b *Bridge) DragTo(x, y float64) (Point, error) {
	cur, err := b.CursorPosition()
	if err != nil {
		return Point{}, err
	}
	return b.Drag(x-float64(cur.X), y-float64(cur.Y))
}

func (b *Bridge) Scroll(dx, dy int) (Point, error) {
	result, err := b.ipc.Call("mouse.scroll", map[string]any{"dx": dx, "dy": dy})
	return pointFrom(result), err
}

func (b *Bridge) TypeText(text string) error {
	_, err := b.ipc.Call("keyboard.type", map[string]any{"text": text})
	return err
}

func (b *Bridge) KeyPress(key string) error {
	_, err := b.ipc.Call("keyboard.keyPress", map[string]any{"key": key})
	return err
}

// Launched is what came of a launch: the application the shell opened, and
// whether its window ended up in the frame.
type Launched struct {
	// App is what the shell made of the name it was given — the application as
	// this machine knows it, which is a path on one desktop and a command on
	// another, and is not always what was asked for.
	App string
	PID int
	// Fitted is whether the window is now the content area. False means the
	// application opened and the frame is still over whatever it was over: a
	// window that never appeared, or one the desktop will not let anybody
	// place.
	Fitted bool
	// Note is what the shell has to say about the fit beyond whether it
	// happened — the reason there was no window to place, or the way a window
	// that was placed did not take the whole of what it was asked for. Empty
	// when the fit was exact.
	Note string
}

// LaunchApp opens an application on this machine and fits its window to the
// content area — the box the screenshots are of and the clicks are aimed
// through.
//
// Both halves are the shell's, because the second one needs the first: what is
// placed is the window of the process that was just started, and only the side
// that started it knows which process that is. It is also the side that owns
// the frame's geometry and already knows how to move somebody else's window —
// that is what the lock does on every drag, in each shell's CarryService.
//
// What a name means is the shell's question too. An application is an app
// bundle on macOS, an executable on Windows and a command on Linux, so the name
// goes over the wire as it was written and is resolved on the other side.
//
// gap is how much of the content area to leave around the window, on every
// side. It is settings.json's and so it is read here rather than there — the
// shell is told the margin along with the name, the way it is told everything
// else about a request. See config.DefaultLaunchGap for what it is for.
func (b *Bridge) LaunchApp(target string, gap int) (Launched, error) {
	result, err := b.ipc.Call("app.launch", map[string]any{"target": target, "gap": gap})
	if err != nil {
		return Launched{}, err
	}
	app, _ := result["app"].(string)
	note, _ := result["note"].(string)
	fitted, _ := result["fitted"].(bool)
	return Launched{App: app, PID: intFrom(result, "pid"), Fitted: fitted, Note: note}, nil
}

// FlashScreenshot triggers the white flash animation in the UI.
func (b *Bridge) FlashScreenshot() {
	_, _ = b.ipc.Call("ui.flash", nil)
}

// SetWindowLock locks or unlocks the window — the toolbar's lock button,
// pressed from somewhere else. What the shell does with it is the whole of
// what the button does: the frame is held to its size, what it frames travels
// with it, and the state is written to instance.json so the next launch comes
// back the way this one was left.
//
// Requests rather than notifications, these three: they are typed at a
// terminal that is waiting for an answer, and a shell too old to know the
// method says so — "Unknown method: ui.lock" — where a notification would
// leave the command reporting success at a window that never moved.
func (b *Bridge) SetWindowLock(locked bool) error {
	_, err := b.ipc.Call("ui.lock", map[string]any{"locked": locked})
	return err
}

// SetClickThrough turns click-through on or off — whether the overlay passes
// what is clicked on it down to the window underneath.
func (b *Bridge) SetClickThrough(enabled bool) error {
	_, err := b.ipc.Call("ui.clickThrough", map[string]any{"enabled": enabled})
	return err
}

// SetRecording starts or stops recording the user's own actions into the
// instance's macro — the toolbar's record button.
//
// The shell owns whether it is recording, so this asks rather than sets: it
// answers by doing what the button does, and one of the things the button does
// is tell the core, on the recording.changed line it always has. That is what
// keeps /status honest about a recording nobody in this process started.
func (b *Bridge) SetRecording(recording bool) error {
	_, err := b.ipc.Call("ui.record", map[string]any{"recording": recording})
	return err
}

// ShowAlert puts a message in front of the user and returns without waiting for
// it to be dismissed — what the core reaches for when something it was asked to
// run cannot run, and the log alone would leave the button looking dead.
func (b *Bridge) ShowAlert(title, message string) {
	b.ipc.Notify("ui.alert", map[string]any{"title": title, "message": message})
}

// NotifyExecutionState tells the UI whether an execution session is running.
func (b *Bridge) NotifyExecutionState(executing bool) {
	b.ipc.Notify("session.state", map[string]any{"executing": executing})
}

// NotifyServerState tells the UI where this instance answers on the network —
// http://<machine>:<port>/<instance>, or "" while the server is off. The
// address is the one thing about an instance worth handing to another device,
// so the shell puts it behind the instance badge.
//
// A machine can be on more than one network, and then it has an address per
// network; the first is sent, which is the one `pob status` prints.
func (b *Bridge) NotifyServerState(running bool, url string) {
	b.ipc.Notify("server.state", map[string]any{"running": running, "url": url})
}

// NotifyRemoteControl tells the UI that something outside the agent loop — an
// MCP client, a browser on the Pob server — is driving this instance, so the
// virtual cursor stays visible while it does. Kept separate from
// session.state: remote control should not lock the window or pause the macro
// recorder the way an agent run does.
//
// Sources are counted rather than toggled, since more than one can be driving
// at a time and the cursor must not vanish while the other still has it.
func (b *Bridge) NotifyRemoteControl(source string, active bool) {
	b.mu.Lock()
	if active == b.driving[source] {
		b.mu.Unlock()
		return
	}
	if active {
		b.driving[source] = true
	} else {
		delete(b.driving, source)
	}
	driving := len(b.driving) > 0
	b.mu.Unlock()
	b.ipc.Notify("mcp.state", map[string]any{"active": driving})
}

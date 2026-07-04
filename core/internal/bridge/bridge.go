// Package bridge exposes the Swift shell's perception/operation primitives
// (screenshot capture, mouse, keyboard, UI dialogs) as typed Go calls over
// the IPC channel. All coordinates are screenshot pixels; the Swift side owns
// the virtual cursor state and the pixel→screen coordinate conversion.
package bridge

import (
	"encoding/base64"
	"fmt"

	"pob/core/internal/ipc"
)

type Bridge struct {
	ipc *ipc.Client
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
	return &Bridge{ipc: client}
}

func pointFrom(result map[string]any) Point {
	x, _ := result["x"].(float64)
	y, _ := result["y"].(float64)
	return Point{X: int(x), Y: int(y)}
}

// CaptureScreenshot captures the Pob window content area. withCursor draws
// the virtual cursor into the image. Returns raw PNG bytes.
func (b *Bridge) CaptureScreenshot(withCursor bool, crop *CropRect) ([]byte, error) {
	params := map[string]any{"withCursor": withCursor}
	if crop != nil {
		params["crop"] = map[string]any{"x": crop.X, "y": crop.Y, "width": crop.W, "height": crop.H}
	}
	result, err := b.ipc.Call("screenshot.capture", params)
	if err != nil {
		return nil, err
	}
	b64, _ := result["image"].(string)
	if b64 == "" {
		return nil, fmt.Errorf("screenshot capture returned no image")
	}
	return base64.StdEncoding.DecodeString(b64)
}

func (b *Bridge) ResetCursor() (Point, error) {
	result, err := b.ipc.Call("cursor.reset", nil)
	return pointFrom(result), err
}

func (b *Bridge) MoveCursor(dx, dy float64) (Point, error) {
	result, err := b.ipc.Call("cursor.move", map[string]any{"dx": dx, "dy": dy})
	return pointFrom(result), err
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

// FlashScreenshot triggers the white flash animation in the UI.
func (b *Bridge) FlashScreenshot() {
	_, _ = b.ipc.Call("ui.flash", nil)
}

// ConfirmMaxStep shows the "Max step exceed" alert and blocks until the user
// picks Continue (true) or Stop (false).
func (b *Bridge) ConfirmMaxStep() bool {
	result, err := b.ipc.Call("ui.confirmMaxStep", nil)
	if err != nil {
		return false
	}
	cont, _ := result["continue"].(bool)
	return cont
}

// NotifyExecutionState tells the UI whether an execution session is running.
func (b *Bridge) NotifyExecutionState(executing bool) {
	b.ipc.Notify("session.state", map[string]any{"executing": executing})
}

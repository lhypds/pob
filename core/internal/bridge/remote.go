package bridge

// Remote presents the bridge as the plain-typed calls the Pob server takes
// (pob/server.Target). It exists so that package stays free of any Pob type:
// it is the server's own protocol on one side and nothing but ints and floats
// on the other, which is what keeps it independently testable.
type Remote struct {
	br *Bridge
}

// Remote returns this bridge behind the server's Target interface.
func (b *Bridge) Remote() *Remote { return &Remote{br: b} }

func (r *Remote) CursorPosition() (int, int, error) {
	pos, err := r.br.CursorPosition()
	return pos.X, pos.Y, err
}

func (r *Remote) MoveCursor(dx, dy float64) error {
	_, err := r.br.MoveCursor(dx, dy)
	return err
}

func (r *Remote) MoveCursorTo(x, y float64) error {
	_, err := r.br.MoveCursorTo(x, y)
	return err
}

func (r *Remote) Click() error       { _, err := r.br.Click(); return err }
func (r *Remote) RightClick() error  { _, err := r.br.RightClick(); return err }
func (r *Remote) DoubleClick() error { _, err := r.br.DoubleClick(); return err }

func (r *Remote) Drag(dx, dy float64) error { _, err := r.br.Drag(dx, dy); return err }
func (r *Remote) Scroll(dx, dy int) error   { _, err := r.br.Scroll(dx, dy); return err }

func (r *Remote) TypeText(text string) error { return r.br.TypeText(text) }
func (r *Remote) KeyPress(key string) error  { return r.br.KeyPress(key) }

// CaptureView takes the whole frame with the cursor in it: it is watched, not
// measured, so there is nothing to crop to and the pointer is the one thing a
// watcher most needs to see.
func (r *Remote) CaptureView() ([]byte, error) { return r.br.CaptureScreenshot(true, nil) }

func (r *Remote) SetRemoteActive(active bool) { r.br.NotifyRemoteControl("server", active) }

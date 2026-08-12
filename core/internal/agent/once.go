package agent

// The once block: the macro that waits for something to happen.
//
// An if asks its condition where the replay reaches it, and a loop asks its own
// before each of a counted number of passes. Both are questions about the screen
// as it is at a moment the macro picked. A once is the other way round — the
// screen picks the moment, and the macro is standing there when it does.
//
// What it watches with is a memory of a single picture. Every interval it takes
// a screenshot and compares it with the one it is holding, then holds the new
// one instead: a screen nobody has touched compares equal and costs a capture,
// and nothing else happens. When the two differ the condition is asked — that,
// and only that, is when a model is paid for — and the block runs if the answer
// is true.
//
// So the two halves do different work and both are needed. The comparison is
// cheap and knows nothing: it says something on this screen moved. The condition
// is expensive and knows what it was asked: it says whether what moved is the
// thing the macro is waiting for. A once written without the first would ask a
// model every second, all day, about a screen that had not changed since the
// last time it asked.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"time"

	"pob/core/internal/applog"
)

// runMacroOnce watches the screen and runs the block each time the screen has
// changed into something the condition holds of. It does not end: what ends it
// is Stop, or a stop() the block itself reached.
//
// Every ask after the first puts the block back the way it was written, for the
// reason a loop's pass does — see runMacroLoop. The screen the slots are about
// is the screen that has just changed, and a condition answered once could never
// turn true a second time.
func (r *Runner) runMacroOnce(ctx context.Context, run *macroRun, node macroNode) {
	label := macroBlockLabel(node)
	// Both settings are read again on every interval rather than settled here. A
	// watch runs for as long as somebody leaves it running, and the two numbers
	// that say how it watches are exactly the ones worth adjusting while it is —
	// what the log records is what it started with.
	interval := time.Duration(r.cfg.OnceInterval()) * time.Millisecond
	threshold := percentage(r.cfg.OnceChangePercent() / 100)
	r.store.LogInstancef(">> ONCE START", "line=%d once=%q interval=%s change=%s",
		node.line, label, interval, threshold)
	applog.Logf("[%s] Macro %s — watching the screen, %s between pictures, acting on more than %s of it changing",
		run.sessionID, label, interval, threshold)

	var memory []byte
	changes, runs, asked := 0, 0, 0
	// A watch is measured in hours, so what goes wrong every interval is said
	// once and not once a second: a bridge that has stopped answering would
	// otherwise write a line a second for as long as nobody is looking.
	blind := false

	for ctx.Err() == nil && !run.halted() {
		interval = time.Duration(r.cfg.OnceInterval()) * time.Millisecond
		shot, err := r.br.CaptureScreenshot(true, nil)
		if err != nil {
			if !blind {
				blind = true
				applog.Logf("[%s] Macro %s — no screenshot: %v. Still watching, and this is said once until one comes back",
					run.sessionID, label, err)
			}
			sleepCtx(ctx, interval)
			continue
		}
		if blind {
			blind = false
			applog.Logf("[%s] Macro %s — a screenshot came back; watching on", run.sessionID, label)
		}

		// The first picture is the memory rather than a change. There is nothing
		// yet for it to be different from, and a screen that has been sitting there
		// since before the macro started has not changed into anything.
		if len(memory) == 0 {
			memory = shot
			sleepCtx(ctx, interval)
			continue
		}

		change := screenChange(memory, shot, r.cfg.OnceChangePercent()/100)
		memory = shot
		if !change.changed {
			sleepCtx(ctx, interval)
			continue
		}

		changes++
		r.store.LogInstancef("ONCE CHANGE", "line=%d once=%q change=%d detail=%q", node.line, label, changes, change.note)
		applog.Logf("[%s] Macro %s — the screen changed: %s", run.sessionID, label, change.note)

		if asked > 0 {
			run.restore(node.line)
			run.restoreBlock(node.body)
		}
		asked++

		// The condition takes a screenshot of its own, a moment after the one the
		// change was seen in. That is the right way round: what the model is asked
		// about is the screen as it is when the question is put, and a picture from
		// a moment ago is one the run has already moved past.
		holds, _ := r.evalMacroCondition(ctx, run, node)
		if !holds {
			// Nothing in the block runs, so nothing in it is asked about — and a slot
			// left live in there is the one psl would fill at the next change, in
			// place of the condition that is waiting on it.
			run.spendBlock(node.body)
			sleepCtx(ctx, interval)
			continue
		}

		// What it says about the block running is evalMacroCondition's `-> TRUE`
		// row, the same one an if and a loop are announced by. This is the count
		// that goes with it, which is a thing about the watch rather than about the
		// condition.
		runs++
		r.store.LogInstancef("ONCE RUN", "line=%d once=%q run=%d", node.line, label, runs)
		r.runMacroNodes(ctx, run, node.body)
		if ctx.Err() != nil || run.halted() {
			break
		}

		// What the block just did to the screen is not a change to notice. The
		// memory is the screen the block left behind, so the next change is the next
		// thing that happens rather than the last thing this macro did — a click of
		// its own read as an event would run the block again on its own wake.
		if shot, err := r.br.CaptureScreenshot(true, nil); err == nil {
			memory = shot
		} else {
			// Nothing to compare against, rather than a picture of the screen from
			// before the block touched it: the next capture becomes the memory, and
			// the change after that one is somebody else's doing.
			memory = nil
		}
		sleepCtx(ctx, interval)
	}

	// However it ended, the block is done being asked about: a restored pass that
	// never ran holds its slots again, and left there they are what psl would fill
	// next. Nothing is written under a once for that to matter to — the check says
	// so — but a stop() unwinds into the file that called this one, and that file
	// has statements of its own.
	run.spendBlock(node.body)
	run.spend(node.line)
	r.store.LogInstancef("ONCE STOP", "line=%d once=%q changes=%d runs=%d status=%q",
		node.line, label, changes, runs, macroStepStatus(ctx, run))
	applog.Logf("[%s] Macro %s — done watching: %d change(s) seen, block run %d time(s)",
		run.sessionID, label, changes, runs)
}

// onceChannelTolerance is how far one channel of a pixel may drift before that
// pixel counts as different — 8 steps in 255. Subpixel antialiasing and the soft
// edge of a shadow move a pixel a little either way while nothing on the screen
// has happened, and what a once is looking for moves further than that.
const onceChannelTolerance = 8

// onceChange is what comparing the picture just taken with the one remembered
// says: whether it counts as a change, and the sentence the log puts it in.
type onceChange struct {
	changed bool
	note    string
}

// screenChange compares the remembered picture with the one just taken. ratio is
// the fraction of the picture that has to differ for it to count as a change —
// once_change_percent, as a fraction. See config.DefaultOnceChangePercent.
func screenChange(before, after []byte, ratio float64) onceChange {
	// A screenshot is a PNG, which is lossless, so one unchanged screen encodes to
	// the same bytes twice. That is the common answer on a machine nobody is
	// touching, and it costs a byte comparison rather than two decodes.
	if bytes.Equal(before, after) {
		return onceChange{note: "the picture is byte for byte the one before it"}
	}

	old, errOld := png.Decode(bytes.NewReader(before))
	shot, errShot := png.Decode(bytes.NewReader(after))
	if errOld != nil || errShot != nil {
		// Nothing can be measured, and what cannot be measured is taken as a change.
		// The cost of asking the condition about a still screen is one model call;
		// the cost the other way round is a once that never notices anything.
		return onceChange{changed: true, note: "the picture could not be read, so it is taken as a change"}
	}

	oldBounds, shotBounds := old.Bounds(), shot.Bounds()
	if oldBounds.Dx() != shotBounds.Dx() || oldBounds.Dy() != shotBounds.Dy() {
		return onceChange{changed: true, note: fmt.Sprintf("the picture is %d×%d, and the one before it was %d×%d",
			shotBounds.Dx(), shotBounds.Dy(), oldBounds.Dx(), oldBounds.Dy())}
	}

	total := shotBounds.Dx() * shotBounds.Dy()
	if total == 0 {
		return onceChange{note: "the picture has no pixels in it"}
	}

	limit := int(float64(total) * ratio)
	differing, past := pixelsDiffering(old, shot, limit)
	if past {
		return onceChange{changed: true, note: fmt.Sprintf("more than %s of the picture is different", percentage(ratio))}
	}
	return onceChange{note: fmt.Sprintf("%s of the picture is different, and a change is more than %s",
		percentage(float64(differing)/float64(total)), percentage(ratio))}
}

// percentage writes a fraction of the picture the way the log says it.
func percentage(ratio float64) string { return fmt.Sprintf("%.3g%%", ratio*100) }

// pixelsDiffering counts the pixels that differ by more than the tolerance, and
// gives up counting as soon as it has more than limit of them: past that the
// answer is the same whatever the rest of the picture holds, and the rest of the
// picture is two million pixels every interval.
func pixelsDiffering(before, after image.Image, limit int) (int, bool) {
	if pixOld, pixShot, ok := packedPixels(before, after); ok {
		n := 0
		for i := 0; i+3 < len(pixOld); i += 4 {
			if bytesDiffer(pixOld[i], pixShot[i]) || bytesDiffer(pixOld[i+1], pixShot[i+1]) ||
				bytesDiffer(pixOld[i+2], pixShot[i+2]) || bytesDiffer(pixOld[i+3], pixShot[i+3]) {
				n++
				if n > limit {
					return n, true
				}
			}
		}
		return n, false
	}

	// A picture in some other format — a greyscale or paletted PNG — read a pixel
	// at a time through the interface every image has. It is slower by a good deal
	// and it is what makes this work whatever a shell hands back.
	oldBounds, shotBounds := before.Bounds(), after.Bounds()
	n := 0
	for y := 0; y < shotBounds.Dy(); y++ {
		for x := 0; x < shotBounds.Dx(); x++ {
			r1, g1, b1, a1 := before.At(oldBounds.Min.X+x, oldBounds.Min.Y+y).RGBA()
			r2, g2, b2, a2 := after.At(shotBounds.Min.X+x, shotBounds.Min.Y+y).RGBA()
			if bytesDiffer(uint8(r1>>8), uint8(r2>>8)) || bytesDiffer(uint8(g1>>8), uint8(g2>>8)) ||
				bytesDiffer(uint8(b1>>8), uint8(b2>>8)) || bytesDiffer(uint8(a1>>8), uint8(a2>>8)) {
				n++
				if n > limit {
					return n, true
				}
			}
		}
	}
	return n, false
}

func bytesDiffer(a, b uint8) bool {
	if a > b {
		return a-b > onceChannelTolerance
	}
	return b-a > onceChannelTolerance
}

// packedPixels hands back the two pictures' raw bytes, when both are the one
// format laid out the one way — the same concrete type, a row exactly as long as
// the picture is wide, and the same number of bytes in each. That is what a
// screenshot decodes to, and comparing the bytes is a hundred times what
// comparing through the interface costs.
//
// The same type matters as much as the same size: an *image.RGBA holds its
// colours multiplied by their alpha and an *image.NRGBA does not, so the same
// pixel is different bytes in the two of them.
func packedPixels(before, after image.Image) ([]uint8, []uint8, bool) {
	switch old := before.(type) {
	case *image.RGBA:
		shot, ok := after.(*image.RGBA)
		if ok && packedRows(old.Stride, old.Rect) && packedRows(shot.Stride, shot.Rect) && len(old.Pix) == len(shot.Pix) {
			return old.Pix, shot.Pix, true
		}
	case *image.NRGBA:
		shot, ok := after.(*image.NRGBA)
		if ok && packedRows(old.Stride, old.Rect) && packedRows(shot.Stride, shot.Rect) && len(old.Pix) == len(shot.Pix) {
			return old.Pix, shot.Pix, true
		}
	}
	return nil, nil, false
}

// packedRows reports whether a picture's rows sit one after another with no gap,
// which is what lets the whole of it be read as one run of pixels.
func packedRows(stride int, rect image.Rectangle) bool { return stride == 4*rect.Dx() }

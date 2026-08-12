package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"pob/core/internal/config"
)

// defaultChange is what a once compares against unless once_change_percent says
// otherwise, as the fraction screenChange is handed.
const defaultChange = config.DefaultOnceChangePercent / 100

// A once is a block like the other two, and reads like one: a keyword, a
// condition in parentheses, and the statements the block holds.
func TestParseMacroOnceBlock(t *testing.T) {
	checkParse(t, `move(398, 915)
once (:: there is a new message ::) {
    click()
    typeText(:: a short reply ::)
}`, []string{
		"move(398, 915)",
		"once (:: there is a new message ::)",
		"  click()",
		"  unfilled: typeText(:: a short reply ::)",
	})
}

// The keyword is lowercase and read in any case, for the reason `if` and `loop`
// are: a header Pob failed to recognise is a body that runs once, unguarded,
// where the file asks for it to run at every change.
func TestParseMacroOnceIsCaseInsensitive(t *testing.T) {
	checkParse(t, `ONCE (true) {
    click()
}`, []string{
		"once (true)",
		"  click()",
	})
}

// A condition written out asks nothing and costs nothing, the same as an if's:
// `once (true)` runs the block at every change the comparison sees, without a
// model call between the change and the block.
func TestCheckAllowsAOnceWithAWrittenOutCondition(t *testing.T) {
	wantProblems(t, `once (true) {
    takeScreenshot()
}`)
}

// A macro that watches is the last thing the file does, and nothing about it is
// the check's business once it says that much.
func TestCheckPassesAMacroThatEndsInAOnce(t *testing.T) {
	wantProblems(t, `move(398, 915)
click()
once (:: an unread message has arrived ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply to it ::)
    keyPress("return")
}`)
}

// Where it is written is half of what it means. A once never hands the run back,
// so one inside another block is a block that runs to the end of the run in
// place of the statements after it.
func TestCheckRefusesAOnceInsideAnotherBlock(t *testing.T) {
	wantProblems(t, `if (true) {
    once (:: a new message ::) {
        click()
    }
}`, "written at the top level of a file")

	wantProblems(t, `loop (3) {
    once (:: a new message ::) {
        click()
    }
}`, "written at the top level of a file")

	wantProblems(t, `once (:: a new message ::) {
    once (:: another one ::) {
        click()
    }
}`, "written at the top level of a file")
}

// The statements inside a dropped once are checked all the same — they will
// still be there once the block is moved, and a macro is worth fixing in one
// pass rather than as many as it has mistakes.
func TestCheckReadsInsideADroppedOnce(t *testing.T) {
	wantProblems(t, `if (true) {
    once (:: a new message ::) {
        move(1)
    }
}`, "written at the top level of a file", "move takes 2 arguments")
}

// A once watches until the run is stopped, so everything under it is a statement
// nothing ever reaches. Said once, at the first of them: a tail of unreachable
// statements is one mistake and not twenty.
func TestCheckRefusesStatementsUnderAOnce(t *testing.T) {
	wantProblems(t, `once (:: a new message ::) {
    click()
}
move(1, 2)
click()
stop()`, "nothing here runs")
}

// A second once is one of those statements: the first never gives the run back,
// so the second is never reached.
func TestCheckRefusesASecondOnce(t *testing.T) {
	wantProblems(t, `once (:: a new message ::) {
    click()
}
once (:: a dialog opened ::) {
    keyPress("escape")
}`, "nothing here runs")
}

// A once is a question asked of every screen that arrives, so one with nothing
// to ask is a block with no reason to run — and a header Pob cannot read takes
// its block with it rather than leaving the body to run unguarded.
func TestCheckRefusesAMalformedOnceHeader(t *testing.T) {
	wantProblems(t, `once {
    click()
}`, "once wants a condition in parentheses")

	wantProblems(t, `once () {
    click()
}`, "once wants a condition in parentheses")

	wantProblems(t, `once (the screen changed) {
    click()
}`, "once wants a condition in parentheses")

	// The `{` missing off the end is the same malformed header, and the block
	// under it is read and dropped the way an if's is — the `}` closes it.
	wantProblems(t, `once (:: a new message ::)
    click()
}`, "once wants a condition in parentheses")
}

// An else belongs to an if. A once has no other half: the condition it asks is
// asked again at the next change rather than answered once, so there is no
// moment an else would be the thing that runs instead.
func TestCheckRefusesAnElseOnAOnce(t *testing.T) {
	wantProblems(t, `once (:: a new message ::) {
    click()
} else {
    keyPress("escape")
}`, "else belongs to an if")
}

// A block that opens and never closes is reported against the line it opened
// on, the same as the other two.
func TestCheckRefusesAnUnclosedOnce(t *testing.T) {
	wantProblems(t, `once (:: a new message ::) {
    click()`, "never closed by a } of its own")
}

// The whole header comes back from psl, so the condition is read out of it again
// rather than assumed to be what replaced the slot.
func TestOnceConditionIsReadBackOutOfTheFilledHeader(t *testing.T) {
	node := macroNode{isOnce: true, condition: ":: a new message ::", line: 1}
	if got := readCondition(node, "once (true) {"); got != "true" {
		t.Errorf("readCondition = %q, want %q", got, "true")
	}
	if got := readCondition(node, "click()"); got != "" {
		t.Errorf("readCondition of a line that is no longer a header = %q, want %q", got, "")
	}
}

// The log names a block the way it is written.
func TestOnceBlockLabel(t *testing.T) {
	label := macroBlockLabel(macroNode{isOnce: true, condition: ":: a new message ::"})
	if label != "once (:: a new message ::)" {
		t.Errorf("macroBlockLabel = %q", label)
	}
}

// picture draws a test screenshot: a background, and a patch of another colour
// w×h at the top left, which is what one thing happening on a screen looks like
// to the comparison.
func picture(t *testing.T, width, height, patchW, patchH int, patch color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.NRGBA{R: 20, G: 30, B: 40, A: 255}
			if x < patchW && y < patchH {
				c = patch
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the test picture: %v", err)
	}
	return buf.Bytes()
}

var white = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

// A screen nobody has touched encodes to the same bytes twice, which is the
// answer a once gets on nearly every interval it runs.
func TestScreenChangeSaysNothingHappened(t *testing.T) {
	shot := picture(t, 400, 300, 0, 0, white)
	if change := screenChange(shot, shot, defaultChange); change.changed {
		t.Errorf("an identical picture reads as a change: %s", change.note)
	}
}

// A caret blinking in a text box moves a handful of pixels, and a once woken by
// that would put a question to a model every interval about a screen where
// nothing had happened.
func TestScreenChangeIgnoresAPatchTooSmallToMatter(t *testing.T) {
	before := picture(t, 400, 300, 0, 0, white)
	after := picture(t, 400, 300, 2, 8, white) // 16 pixels of 120,000
	change := screenChange(before, after, defaultChange)
	if change.changed {
		t.Errorf("16 pixels of 120,000 reads as a change: %s", change.note)
	}
	if change.note == "" {
		t.Error("the comparison said nothing the log could print")
	}
}

// And what these blocks are written to notice is often small — a row arriving in
// a list, a badge appearing on an icon — so the threshold has to be low enough
// to see one.
func TestScreenChangeSeesARowArrive(t *testing.T) {
	before := picture(t, 400, 300, 0, 0, white)
	after := picture(t, 400, 300, 120, 4, white) // 480 pixels of 120,000
	if change := screenChange(before, after, defaultChange); !change.changed {
		t.Errorf("480 pixels of 120,000 reads as no change: %s", change.note)
	}
}

// A colour that has barely moved is the same colour: subpixel antialiasing and
// the soft edge of a shadow drift a step or two while nothing has happened.
func TestScreenChangeIgnoresAPixelThatBarelyMoved(t *testing.T) {
	before := picture(t, 400, 300, 400, 300, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	after := picture(t, 400, 300, 400, 300, color.NRGBA{R: 104, G: 104, B: 104, A: 255})
	if change := screenChange(before, after, defaultChange); change.changed {
		t.Errorf("a drift of 4 in 255 across the picture reads as a change: %s", change.note)
	}
	further := picture(t, 400, 300, 400, 300, color.NRGBA{R: 120, G: 120, B: 120, A: 255})
	if change := screenChange(before, further, defaultChange); !change.changed {
		t.Errorf("a drift of 20 in 255 across the picture reads as no change: %s", change.note)
	}
}

// A window that has been resized is a different picture whatever its pixels say,
// and comparing the two of them pixel by pixel is not a comparison at all.
func TestScreenChangeSeesADifferentSize(t *testing.T) {
	before := picture(t, 400, 300, 0, 0, white)
	after := picture(t, 380, 300, 0, 0, white)
	if change := screenChange(before, after, defaultChange); !change.changed {
		t.Errorf("a picture of another size reads as no change: %s", change.note)
	}
}

// Nothing can be measured, so it is taken as a change: one model call is what
// that costs, and the other way round is a once that never notices anything.
func TestScreenChangeTakesAnUnreadablePictureAsAChange(t *testing.T) {
	change := screenChange(picture(t, 40, 30, 0, 0, white), []byte("not a png"), defaultChange)
	if !change.changed {
		t.Errorf("a picture that does not decode reads as no change: %s", change.note)
	}
}

// Whatever a shell hands back is compared. A greyscale PNG decodes to neither of
// the two packed formats, and is read a pixel at a time instead.
func TestScreenChangeReadsAPictureInAnotherFormat(t *testing.T) {
	grey := func(shade uint8, patch int) []byte {
		img := image.NewGray(image.Rect(0, 0, 400, 300))
		for y := 0; y < 300; y++ {
			for x := 0; x < 400; x++ {
				c := uint8(60)
				if x < patch && y < 4 {
					c = shade
				}
				img.SetGray(x, y, color.Gray{Y: c})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("encoding the test picture: %v", err)
		}
		return buf.Bytes()
	}
	if change := screenChange(grey(60, 0), grey(255, 4), defaultChange); change.changed {
		t.Errorf("16 grey pixels of 120,000 read as a change: %s", change.note)
	}
	if change := screenChange(grey(60, 0), grey(255, 120), defaultChange); !change.changed {
		t.Errorf("480 grey pixels of 120,000 read as no change: %s", change.note)
	}
}

// The same two pictures, and the setting is the whole of what the answer turns
// on: a screen being watched for a badge appearing and one redrawing a graph
// every second want opposite ends of the range.
func TestTheChangeThresholdIsWhatDecides(t *testing.T) {
	before := picture(t, 400, 300, 0, 0, white)
	after := picture(t, 400, 300, 2, 8, white) // 16 pixels of 120,000 — 0.013%
	if change := screenChange(before, after, 0.0001); !change.changed {
		t.Errorf("at 0.01%% those 16 pixels read as no change: %s", change.note)
	}
	if change := screenChange(before, after, 0.1); change.changed {
		t.Errorf("at 10%% those 16 pixels read as a change: %s", change.note)
	}
	// A threshold of none is every pixel that moved, which is what the bottom of
	// the range asks for: the count has to be more than the limit, and the limit
	// is nothing.
	if change := screenChange(before, after, 0); !change.changed {
		t.Errorf("at 0%% those 16 pixels read as no change: %s", change.note)
	}
}

// The count stops as soon as the answer cannot change, so a picture where
// everything moved costs the threshold and not the picture.
func TestPixelsDifferingStopsCountingPastTheLimit(t *testing.T) {
	before, _ := png.Decode(bytes.NewReader(picture(t, 400, 300, 0, 0, white)))
	after, _ := png.Decode(bytes.NewReader(picture(t, 400, 300, 400, 300, white)))
	n, past := pixelsDiffering(before, after, 100)
	if !past || n != 101 {
		t.Errorf("pixelsDiffering = %d, past=%v; want it to stop at 101", n, past)
	}
}

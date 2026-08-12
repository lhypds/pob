package agent

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the test picture: %v", err)
	}
	return buf.Bytes()
}

// A scale of 1 is the setting's default and has to cost nothing: the picture
// goes to the model as it was captured, not decoded and re-encoded on the way.
func TestAScaleOfOneLeavesThePictureAlone(t *testing.T) {
	src := testPNG(t, 100, 80)
	out, w, h, err := shrinkPNG(src, 1)
	if err != nil || w != 0 || h != 0 || !bytes.Equal(out, src) {
		t.Errorf("shrinkPNG(src, 1) = %d bytes, %dx%d, %v; want the bytes back untouched", len(out), w, h, err)
	}
}

func TestShrinkingHalvesBothSides(t *testing.T) {
	out, w, h, err := shrinkPNG(testPNG(t, 100, 80), 0.5)
	if err != nil {
		t.Fatalf("shrinkPNG: %v", err)
	}
	if w != 50 || h != 40 {
		t.Errorf("shrinkPNG reported %dx%d, want 50x40", w, h)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("the shrunken picture does not decode: %v", err)
	}
	if cfg.Width != 50 || cfg.Height != 40 {
		t.Errorf("the shrunken picture is %dx%d, want 50x40", cfg.Width, cfg.Height)
	}
}

// The model has to see one coordinate system: when its screenshot is half the
// original size, every existing image coordinate in its temporary file is half
// size too. Other numeric values and numbers in prose stay as written.
func TestExistingCoordinatesAreScaledInTheModelCopy(t *testing.T) {
	source := `moveTo(1000, 500)
click(300, 200) // keep this example at click(900, 800)
move(-40, :: how far down from 80? ::)
move(:: x, or y ::, 40)
takeScreenshot(100, 200, 300, 400)
scroll(0, 400)
sleep(500ms)
loop (10) {
  typeText("click(700, 600)")
}
/*
click(900, 800)
*/
/* keep 9 */ dragTo(600, 100)
drag(100, /* keep this note */ 200)`
	want := `moveTo(500, 250)
click(150, 100) // keep this example at click(900, 800)
move(-20, :: how far down from 80? ::)
move(:: x, or y ::, 20)
takeScreenshot(50, 100, 150, 200)
scroll(0, 400)
sleep(500ms)
loop (10) {
  typeText("click(700, 600)")
}
/*
click(900, 800)
*/
/* keep 9 */ dragTo(300, 50)
drag(50, /* keep this note */ 100)`

	if got := scaleMacroCoordinates(source, 0.5); got != want {
		t.Errorf("scaleMacroCoordinates =\n%s\nwant\n%s", got, want)
	}
}

func TestAFullSizeModelCopyLeavesTheFileByteForByteAlone(t *testing.T) {
	source := "  moveTo(1000, 500) // unchanged\n"
	if got := scaleMacroCoordinates(source, 1); got != source {
		t.Errorf("scaleMacroCoordinates at 1 = %q, want %q", got, source)
	}
}

// Averaging rather than sampling is the point of the box filter: a checkerboard
// picked from lands on one of its two colours, and averaged lands between them.
func TestShrinkingAveragesRatherThanSamples(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			shade := uint8(0)
			if (x+y)%2 == 0 {
				shade = 255
			}
			img.Set(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	out := boxDownscale(img, 4, 4)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			r, _, _, _ := out.At(x, y).RGBA()
			if got := uint8(r >> 8); got < 126 || got > 129 {
				t.Fatalf("pixel (%d, %d) came out %d, want the two shades averaged to about 127", x, y, got)
			}
		}
	}
}

// A coordinate the model read off a half-size picture is half the distance it
// meant on the screen, so the macro has to be written with the whole one.
func TestAnAnswerIsGrownBackToScreenPixels(t *testing.T) {
	statement := "    move(::the profile icon::)"
	start, end := 9, len(statement)-1
	got, done := rescaleFilled(statement, start, end, "    move(378, 245)", 0.5)
	if !done || got != "    move(756, 490)" {
		t.Errorf("rescaleFilled = %q, %v; want %q scaled back", got, done, "    move(756, 490)")
	}
}

// A position read off a half-size picture is half the position it means on the
// screen, the same as a distance is — so the calls that take an absolute (x, y)
// are grown back exactly like the relative ones.
func TestAnAbsoluteAnswerIsGrownBackToScreenPixels(t *testing.T) {
	cases := []struct{ statement, filled, want string }{
		{"moveTo(::the profile icon::)", "moveTo(378, 245)", "moveTo(756, 490)"},
		{"click(::the Save button::)", "click(378, 245)", "click(756, 490)"},
		{"rightClick(::the file in the list::)", "rightClick(378, 245)", "rightClick(756, 490)"},
		{"doubleClick(::the file in the list::)", "doubleClick(378, 245)", "doubleClick(756, 490)"},
		{"dragTo(::the folder to drop it in::)", "dragTo(378, 245)", "dragTo(756, 490)"},
	}
	for _, c := range cases {
		start := strings.IndexByte(c.statement, '(') + 1
		end := strings.LastIndexByte(c.statement, ')')
		got, done := rescaleFilled(c.statement, start, end, c.filled, 0.5)
		if !done || got != c.want {
			t.Errorf("rescaleFilled(%q) = %q, %v; want %q", c.statement, got, done, c.want)
		}
	}
}

// What the model wrote, told apart from the statement it went into — this is
// what the log says a slot came back as, before it is grown back to screen
// pixels.
func TestTheAnswerIsCutOutOfTheFilledStatement(t *testing.T) {
	cases := []struct {
		statement, filled, want string
		ok                      bool
	}{
		{"click(::the Save button::)", "click(180, 193)", "180, 193", true},
		{"move(40, ::how far down::)", "move(40, 120)", "120", true},
		{`typeText("Hi ::the name::!")`, `typeText("Hi Bob!")`, "Bob", true},
		// A line psl rewrote further than the slot: no part of it is the answer.
		{"click(::the Save button::)", "moveTo(180, 193)", "", false},
	}
	for _, c := range cases {
		start := strings.Index(c.statement, "::")
		end := strings.LastIndex(c.statement, "::") + 2
		got, ok := filledAnswer(c.statement, start, end, c.filled)
		if got != c.want || ok != c.ok {
			t.Errorf("filledAnswer(%q, %q) = %q, %v; want %q, %v", c.statement, c.filled, got, ok, c.want, c.ok)
		}
	}
}

// Once the model-facing statement's scaled surroundings are replaced with the
// original source text, only the slot's answer is grown.
func TestOnlyTheAnsweredPartOfAStatementIsScaled(t *testing.T) {
	statement := "move(40, ::how far down::)"
	start, end := 9, len(statement)-1
	got, done := rescaleFilled(statement, start, end, "move(40, 120)", 0.5)
	if !done || got != "move(40, 240)" {
		t.Errorf("rescaleFilled = %q, %v; want the 40 left alone and the 120 doubled", got, done)
	}
}

func TestScaledModelSurroundingsAreRestoredBeforeTheAnswerIsGrown(t *testing.T) {
	statement := "move(40, ::how far down::)"
	modelStatement := "move(20, ::how far down::)"
	start := strings.Index(statement, "::")
	end := start + len("::how far down::")
	modelStart := strings.Index(modelStatement, "::")
	modelEnd := modelStart + len("::how far down::")

	restored, ok := restoreFilledSurroundings(statement, start, end, modelStatement,
		modelStart, modelEnd, "move(20, 120)")
	if !ok || restored != "move(40, 120)" {
		t.Fatalf("restoreFilledSurroundings = %q, %v; want the original 40 restored", restored, ok)
	}
	got, done := rescaleFilled(statement, start, end, restored, 0.5)
	if !done || got != "move(40, 240)" {
		t.Errorf("rescaleFilled = %q, %v; want only the new answer grown", got, done)
	}
}

// A length of time, a key and a condition are not distances on the picture, and
// a slot that answers one comes back exactly as the model wrote it.
func TestStatementsThatAreNotDistancesAreLeftAlone(t *testing.T) {
	cases := []struct{ statement, filled string }{
		{"sleep(::how long to wait::)", "sleep(2s)"},
		{"sleep(::how long to wait::)", "sleep(10m)"},
		{"if (::zed is running::) {", "if (true) {"},
		{"typeText(::what to type::)", `typeText("120")`},
		{"scroll(::how far to the bottom::)", "scroll(0, 400)"},
	}
	for _, c := range cases {
		// Every one of these holds its slot between the parentheses.
		start := strings.IndexByte(c.statement, '(') + 1
		end := strings.LastIndexByte(c.statement, ')')
		got, done := rescaleFilled(c.statement, start, end, c.filled, 0.5)
		if done || got != c.filled {
			t.Errorf("rescaleFilled(%q) = %q, %v; want it untouched", c.statement, got, done)
		}
	}
}

// A slot on a line of its own fills to statements rather than to a value, and
// they were read off the same shrunken picture: each one is grown back by what
// it is, and the ones whose numbers are not distances on the picture come back
// exactly as the model wrote them.
func TestABlockIsScaledAStatementAtATime(t *testing.T) {
	statement := ":: click my mom and type a message ::"
	filled := `click(199, 61)
sleep(500ms)
typeText("hi mom")
	move(-50, 20)  // back to where it was
scroll(0, 400)`
	want := `click(398, 122)
sleep(500ms)
typeText("hi mom")
	move(-100, 40)  // back to where it was
scroll(0, 400)`

	got, done := rescaleFilled(statement, 0, len(statement), filled, 0.5)
	if !done || got != want {
		t.Errorf("rescaleFilled over a block =\n%s\n%v; want\n%s", got, done, want)
	}
}

// A statement the model left a slot in has not been answered yet. That slot is
// the generated file's own, filled off a screenshot of its own and grown back
// then — so nothing here touches it.
func TestABlockLeavesItsOwnSlotsAlone(t *testing.T) {
	statement := ":: reply to the message ::"
	filled := "click(:: the message box ::)\ntypeText(:: a short reply ::)"
	got, done := rescaleFilled(statement, 0, len(statement), filled, 0.5)
	if done || got != filled {
		t.Errorf("rescaleFilled = %q, %v; want the block untouched", got, done)
	}
}

// psl replaces the slot and leaves the rest of the line as it was. A line that
// came back with its surroundings moved is one Pob cannot tell the model's
// numbers from the macro's in, so it is taken as written rather than guessed at.
func TestAnAnswerIsNotScaledWhenTheLineWasRewritten(t *testing.T) {
	statement := "move(::the profile icon::)"
	got, done := rescaleFilled(statement, 5, len(statement)-1, "moveTo(378, 245)", 0.5)
	if done || got != "moveTo(378, 245)" {
		t.Errorf("rescaleFilled = %q, %v; want the rewritten line left as it is", got, done)
	}
}

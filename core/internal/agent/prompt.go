package agent

// macroPrompt is what Pob tells psl about the language the file it is filling
// is written in, handed over as psl's `--prompt`: the calls a statement can be,
// what their arguments mean, and in what units. It is context and never an
// instruction — what to write into a slot is what the slot itself says.
//
// The model is shown a file of statements and a screenshot, and most of what
// makes an answer right rather than merely plausible is in neither: that an
// offset is measured from the cursor rather than from the corner of the screen,
// which way a scroll is signed, that a condition comes back as one of two words.
// Unsaid, `move(dx, dy)` reads as a position on screen — the one misreading that
// puts the cursor somewhere else on every statement that follows.
//
// It is the Calls table of docs/03_Macro PSL.md and the switch in runMacroAction
// said a third way, and moves when they do: a call this leaves out is one the
// model has no reason to use, and one it invents is a line Pob logs and skips.
const macroPrompt = `This file is a Pob macro: one statement per line, replayed top to bottom to
drive the screen shown in the image. A statement is a call — name(argument,
argument) — or an if (condition) { block } closed by a } on a line of its own.
Numbers are written plainly (398, -615, 0.5), strings in double quotes with a
backslash escaping the character after it.

The whole vocabulary:

  move(dx, dy)         nudge the cursor by a relative pixel offset
  drag(dx, dy)         drag from the cursor by that offset, ending there
  scroll(dx, dy)       scroll at the cursor: dy > 0 down, dy < 0 up, dx > 0 right
  click()              left-click at the cursor
  rightClick()         right-click at the cursor
  doubleClick()        double-click at the cursor
  typeText("text")     type one string at the keyboard focus
  keyPress("key")      press one key, modifiers joined in front of it with + —
                       "return", "cmd+v", "ctrl+shift+t"
  sleep(milliseconds)  pause
  resetCursor()        send the cursor back to the origin a replay starts from
  take_screenshot()    capture the screen; with x, y, w, h, crop to that region

Coordinates are pixels in the image, origin top-left, x to the right, y down.
move, drag and scroll are relative to where the cursor is now — the arrow
visible in the image — so a slot in one of them is answered with the distance
from the cursor to the target, positive or negative, and never with the target's
own coordinates.

An if condition is answered with true or false and nothing else.`

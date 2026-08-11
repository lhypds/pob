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
// It is the Calls table of docs/Macro PSL/06_Calls.md and the switch in runMacroAction
// said a third way, and moves when they do: a call this leaves out is one the
// model has no reason to use, and one it invents is a line Pob logs and skips.
const macroPrompt = `This file is a Pob macro: one statement per line, replayed top to bottom to
drive the screen shown in the image. A statement is a call — name(argument,
argument) — or a block, closed by a } on a line of its own: if (condition) { }
runs what it holds when the condition holds, loop (count) { } runs it that many
times, and loop (condition, count) { } runs it while the condition holds, count
being the most passes it may make. Numbers are written plainly (398, -615, 0.5),
strings in double quotes with a backslash escaping the character after it. stop
is the one statement written without parentheses. Comments are C's — // to the
end of the line, /* … */ across lines — and are notes to read rather than
statements that run.

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
  takeScreenshot()     capture the screen; with x, y, w, h, crop to that region
  stop                 end the replay here; nothing under it runs
  call("other.psl")    replay another PSL file here, then carry on below it. The
                       path is relative to the file the call is written in

Coordinates are pixels in the image, origin top-left, x to the right, y down.
move, drag and scroll are relative to where the cursor is now — the arrow
visible in the image — so a slot in one of them is answered with the distance
from the cursor to the target, positive or negative, and never with the target's
own coordinates.

How much of a statement a slot stands for is what is written around it. A slot
that is one argument of several is answered with that argument alone, and one
written where the whole argument list goes is answered with the whole list,
commas and all: move(40, <instruction>) wants one number, and
move(<instruction>) wants both, written -120, 40. Either way the answer has to
leave a line that reads as one of the statements above.

An if or loop condition is answered with true or false and nothing else. A loop
asks its condition again before every pass, over a fresh image, and the answer
is about the screen as that image shows it — nothing in the file says which pass
is being asked about, and nothing has to.`

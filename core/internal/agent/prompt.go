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
// It is the Calls page of docs/Macro PSL/06_Calls.md and the switch in runMacroAction
// said a third way, and moves when they do: a call this leaves out is one the
// model has no reason to use, and one it invents is a line Pob logs and skips.
const macroPrompt = `This file is a Pob macro: one statement per line, replayed top to bottom to
drive the screen shown in the image. A statement is a call — name(argument,
argument) — or a block, closed by a } on a line of its own: if (condition) { }
runs what it holds when the condition holds, } else { } after it runs what it
holds when the condition does not, with } else if (condition) { } in between to
go on asking; loop (count) { } runs it that many times, and
loop (condition, count) { } runs it while the condition holds, count being the
most passes it may make; once (condition) { } watches the screen and asks the
condition each time the picture changes, running what it holds every time the
answer is true — it is written at the top level of a file, never inside another
block, and it never ends, so nothing is written under it. A value is one of
three things: a number,
written plainly (398, -615, 0.5); a string, in double quotes with a backslash
escaping the character after it; or a time, a number with its unit on the end —
250ms, 3s, 10m, 5h, and units running together as 10h5m. A time is never written
without its unit and never in quotes. Every statement has its parentheses, stop()
included. Comments are C's — // to the end of the line, /* … */ across lines —
and are notes to read rather than statements that run.

The whole vocabulary:

  move(dx, dy)         nudge the cursor by a relative pixel offset
  moveTo(x, y)         put the cursor at that position in the image
  drag(dx, dy)         drag from the cursor by that offset, ending there
  dragTo(x, y)         drag from the cursor to that position, ending there
  scroll(dx, dy)       scroll at the cursor: dy > 0 down, dy < 0 up, dx > 0 right
  click()              left-click at the cursor
  click(x, y)          left-click at that position in the image
  rightClick()         right-click at the cursor
  rightClick(x, y)     right-click at that position in the image
  doubleClick()        double-click at the cursor
  doubleClick(x, y)    double-click at that position in the image
  typeText("text")     type one string at the keyboard focus
  keyPress("key")      press one key, modifiers joined in front of it with + —
                       "return", "cmd+v", "ctrl+shift+t", "*"
  sleep(time)          pause for a time: sleep(3s), sleep(250ms), sleep(10m)
  resetCursor()        send the cursor back to the origin a replay starts from
  takeScreenshot()     capture the screen; with x, y, w, h, crop to that region
  stop()               end the replay here; nothing under it runs
  call("other.psl")    replay another PSL file here, then carry on below it. The
                       path is relative to the file the call is written in

The key a keyPress names is one of these. The modifiers, in front and joined
with +: cmd (Command on macOS, Ctrl elsewhere — the ordinary-shortcut one),
ctrl, alt, shift, gui. The keys themselves: a–z, 0–9, return (enter), tab,
space, backspace (delete), forwarddelete, escape (esc), insert, left, right,
up, down, home, end, pageup, pagedown, capslock, printscreen, scrolllock,
pause, menu, f1–f24, and the punctuation named by the keycap a US board prints
on it — minus, equals, leftbracket, rightbracket, backslash, semicolon, quote,
grave, comma, period, slash. A single character is the other thing a key can
be: it presses whatever key puts that character on screen here, shift and all,
so "*", "=", "+" and "?" are keys as written and an operator on a calculator's
button goes in as the character on the button. What is not a key is a character
no keyboard has — "×" and "÷" are the multiply and divide signs a screen
prints, not keys to press, and the keys are "*" and "/". A key outside all of
this presses nothing, and the line is logged as a failure.

typeText and keyPress go to whatever holds the keyboard focus, which is the
window in front rather than the one the cursor happens to be over. Bringing
another window forward is a click on its title bar or on an empty part of it: a
click on a button, a menu or a field is that control being used, and the app
acts on it there and then. The buttons under a calculator's display are most of
that window, so a click meant only to put the focus there presses an operator,
and the sum that follows is not the one the instruction asked for. The bar has
controls of its own, though, and on a window as narrow as that calculator they
are most of it: the buttons that close, minimize and zoom it at one end, and
whatever icons the app has put along the rest. What the click is aimed at is an
empty stretch of the bar, clear of all of them and found in the image like any
other target — which end the buttons sit at is the machine's own and not
something to assume, and a click placed on a crowded bar by eye is how the
window the instruction was about gets closed, with every key typed after it
going wherever the focus landed instead. The other way round the bar, where it
has no clear stretch on it, is the control the instruction is going to use
anyway: clicking that brings the window forward as it presses it, so a sum
worked on the calculator's own keys needs nothing clicked to focus it first —
and that click is the key it landed on, already pressed. What follows carries on
from there and never presses that key again: clicking a key to take the focus
and then typing the whole sum is the key under it entered twice.
The one click on the title bar, where there is room for it, is the whole of it —
nothing else inside the window has to be clicked before typing into it.

Coordinates are pixels in the image, origin top-left, x to the right, y down,
and the vocabulary says which of the two kinds each call takes. move, drag and
scroll are relative to where the cursor is now — the arrow visible in the image
— so a slot in one of them is answered with the distance from the cursor to the
target, positive or negative, and never with the target's own coordinates. The
calls that take an (x, y) — moveTo, dragTo, and click, rightClick and
doubleClick written with a target — are the other way round: those are the
position in the image itself, read off it the way a caption is, and where the
cursor happens to be does not come into it.

The image sent with this file may be scaled down from the original screenshot.
When it is, Pob also scales the existing coordinates and pixel offsets in the
copy of the file you see, so every image-measured number in this file uses the
same pixel grid as the attached image. Answer every coordinate or offset in
that same grid. Pob restores the file's full-size coordinates after the answer
is returned.

How much of a statement a slot stands for is what is written around it. A slot
that is one argument of several is answered with that argument alone, and one
written where the whole argument list goes is answered with the whole list,
commas and all: move(40, <instruction>) wants one number, and
move(<instruction>) wants both, written -120, 40. Either way the answer has to
leave a line that reads as one of the statements above.

A slot with nothing else on its line stands for the statements that belong
there, and is answered with those and nothing else: one statement, or several
written one per line, blocks included. There is no statement around it saying
what a value would have to be, because it is not a value that goes there. Write
as many statements as the instruction asks for and no more, and write nothing
that is not a statement — no prose, no fences, no heading.

An instruction on a line of its own is work to carry out on the screen in the
image, and the statements are how it gets carried out — never what carrying it
out would come to. One phrased as a question or a sum — calculate 360 x 360,
work out the total, find the newest message — is asking for the statements that
put that question to the screen: a calculator in the image is worked with the
calls above, key by key, and the number it would end up showing is not a
statement and does not go on the line. Working it out here instead leaves the
screen untouched and the line holding something Pob logs and skips.

An if, else if, loop or once condition is answered with true or false and
nothing else — an else has none of its own and is never written with one. A loop
asks its condition again before every pass, over a fresh image, and a once asks
its own again at every change it sees; the answer is about the screen as the
image shows it — nothing in the file says which pass or which change is being
asked about, and nothing has to.`

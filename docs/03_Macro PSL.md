
Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps one per instance, in a file called `macro.psl`, and **Prompt Script Language** — PSL — is
what that file is written in. This page is both: the macro, and the language it is a macro in.

PSL is not Pob's own. It is a language with a compiler of its own — [psl](https://github.com/lhypds/psl)
— and Pob is one program that happens to write in it. The `:: … ::` slots below are filled by running
that compiler, which is where the models and the API keys live. Pob holds none of its own.

The language is small enough that a recording is readable, and readable enough that the recording is
worth editing afterwards. That is the whole point of the pair — you record a macro by doing the
thing once, and what you get back is a program you can open.

The name is what sets PSL apart from a scripting language that only ever does what it is told.
Anywhere a value would go, a statement can hold a prompt instead — `:: … ::`, an AI slot — and what
the AI answers is what that part of the line then says. A macro written in PSL is part instruction
and part question: it repeats what it was given, and asks about what it could not be.

```
move(398, 915)
click()
drag(-775, -615)
if (:: the window focus on a wechat user ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply to the message on screen ::)
}
keyPress("return")
```

Three things write a macro and one thing runs it. Recording writes it from what you, the AI and the
[MCP](08_MCP.md) clients do; you write it by hand in an editor; the AI writes it as it works.
Execute, `pob macro` and the [Control API](11_Control%20API.md) all run it the same way.


macro.psl
---------

Each instance has one macro, `macro.psl`, in its `~/.pob/<instance>/` directory. The PSL button
(🪄) in the toolbar opens it in your editor; Execute (▶) replays it; Reset (↻) empties it.

Use the record button (⏺) to write it instead of typing it — actions are appended to `macro.psl` as
they happen. Starting a recording while `macro.psl` still holds statements asks what to do with them
first: clear them, or keep them and record after them. Keeping them writes a `resetCursor()` between
the old lines and the new ones, since every move recorded next is relative to the origin a replay
starts at.

Recording captures every action that drives the machine, whichever one of the three is driving it:
your own mouse and keyboard, the AI's tool calls, and the tools an [MCP](08_MCP.md) client calls.
They all append to the same `macro.psl`, in the order things happened. The MCP tools that take an
absolute `(x, y)` are written down as the relative `move(dx, dy)` this vocabulary replays, so a
recording made through MCP plays back like any other.

Your own mouse and keyboard are recorded on macOS only, for now — watching the input of other
applications is a different mechanism on each system, and the Linux and Windows shells do not have
it yet. On those two the record button still captures everything the AI and MCP clients drive.

A macro recorded before the file was named `macro.psl` is carried over from `macro.txt` the first
time this Pob runs, so nothing that was recorded is lost to the rename.


Structure
---------

One statement per line, run top to bottom. Blank lines are nothing at all. There is no comment
syntax and no line continuation: a line is a whole statement or it is not one.

A line that cannot be read does not stop the run — it is written to the log and skipped, and the
statements around it stand. A macro is often half-recorded and half-typed, and one bad line in the
middle of it is a line to fix, not a reason to refuse the other forty.

There are three kinds of statement: a **call**, which does something to the machine or to the run,
an **if block**, which guards the statements inside it with a condition, and a **loop block**, which
runs the statements inside it again and again. Any of them can hold an **AI slot** — a piece of a
statement that is a prompt rather than a value, filled in as the replay reaches it.


AI slot
-------

An AI slot is an instruction written where a value would go, wrapped in `::` on both sides:

```
:: instruction ::
```

The spaces are optional. `::instruction::` and `:: instruction ::` are the same slot, and one end may
be closed up and the other not. What the markers may not do is touch a letter or a digit on the
outside: `typeText("std::cout")` types `std::cout`, and `typeText("a::b::c")` types `a::b::c`. That
is what tells a slot from a `::` in the text being typed.

(This is psl's own rule, read the same way on both sides. It has to be: psl fills the first slot in
the file it is handed and no other, so Pob picks which one by writing every other slot out of the
copy it hands over — and a slot Pob did not recognise is one it would leave in, for psl to answer
instead.)

The instruction between the markers is a question for the model rather than something Pob carries
out itself. It goes **anywhere in a statement** — a whole argument, part of one, the condition of an
`if` or of a `loop`:

```
move(:: the x offset to the Save button ::, 0)
typeText(:: what to reply to this message ::)
typeText("Hi :: the name at the top of the chat ::, thanks!")
if (:: a save dialog is on screen ::) {
    keyPress("return")
}
loop (:: another unread message in the list ::, 10) {
    typeText(:: a short reply to the message on screen ::)
}
```

When the replay reaches the statement, Pob takes a screenshot and runs the psl compiler over the
macro — the file itself, whole and unaltered, with that screenshot as the slot's image. psl asks a
model, writes the answer in where the markers were, and hands the file back; Pob reads the statement
out of it, parses it as PSL and executes it. Everything outside the markers is written down and
means exactly what it says.

Nothing is prepended to the file and no statement is rewritten on the way over. What does travel
beside it is a description of the vocabulary — the calls below, what their arguments mean, and that
an offset is measured from the cursor rather than from the corner of the screen — handed to psl as
its `--prompt`, which is the flag a compiler takes a briefing on an API for. It says what the calls
are and never what to write: what a value has to come back as is what the statement around it
already says, and how a slot is filled at all is psl's own business.

A psl older than that flag fills the macro all the same, from the statement and the screenshot and
nothing else. Pob asks the one on the machine what it takes before sending anything, and notes in
the log when a run goes without.

That is the *prompt* in Prompt Script Language, and the whole of what separates it from a scripting
language. A call is a macro doing what it was told. A slot is a macro asking about a screen nobody
could describe to it in advance, at the moment it is looking at that screen.

What comes back

Nothing states what kind of value is wanted — the model is shown the statement and works that out
from where the slot sits in it, which is why the whole macro goes with it. What it answers has to
leave a statement that reads as PSL:

| Written | What the slot has to come back as |
|---------|-----------------------------------|
| `move(:: … ::, 40)` | a bare number — `-120` |
| `typeText(:: … ::)` | a quoted string — `"Hello"` |
| `typeText("Hi :: … ::")` | bare text, the quotes are already there — `Bob` |
| `if (:: … ::)` | `true` or `false` |
| `loop (:: … ::, 5)` | `true` or `false` |

Coordinates come back as screenshot pixels, and `move` and `drag` are relative to where the cursor
is now — the arrow the model can see in the screenshot — so what it answers is an offset from there,
not a position on the screen.

They are screenshot pixels whatever `image_scale` is set to (see [Settings](06_Settings.md)), and it
is below `1` by default. So the model is shown a smaller picture than the screen and answers in that
picture's pixels, and Pob grows the answer back before it goes into the macro: a `move` filled from a
third-size screenshot and one filled from a whole one write the same line. Only the part the model wrote is grown — a
number already in the statement was never in the model's coordinates — and only where the numbers
are distances across the picture: `move`, `drag` and `take_screenshot`. `sleep` is milliseconds and
`scroll` is a wheel delta, so neither is touched.

A statement that does not read as PSL once its slots are filled is logged with what it was filled to
and skipped, like any other line that cannot be read. Nothing is retried: the macro goes on to the
next statement. A psl run that fails outright — no model configured, no answer — leaves the statement
unfilled and therefore unrun, the same way.

Writing one

Write an instruction a screenshot can settle — "a chat window is open", "the file list is empty",
"the x offset to the Save button". The model is given the macro, the statement and the picture and
nothing else: it has no memory of what the statements before it actually did, so an instruction that
turns on that is one it cannot answer.

Whitespace around the instruction is trimmed, so `::a::`, `:: a ::` and `::  a  ::` all ask the same
thing. A pair of markers with nothing between them asks nothing and is not a slot. What the model answers is a
value and never more program: a `::` in the answer is text, not another slot to fill.

A slot can name the model it wants, which is psl's own syntax passed straight through:

```
typeText(:: gpt-5.6> a short reply to the message on screen ::)
```

Each statement's slots are filled left to right, one psl run each, and each one is asked with the
earlier answers already in place — so the second slot of `move(:: … ::, :: … ::)` is asked about a
statement that already reads `move(-120, :: … ::)`. That is how psl and Pob stay on the same slot at
all: psl fills the first slot in the file it is given, Pob replays the file top to bottom, and every
answer goes back into the file before the next run, so the first slot left is always the one the
replay is waiting on.

Two kinds of statement would break that step, and both are settled by writing their slots out of the
file as `<instruction>` — there to be read, not to be answered. One is a statement that will never
run: the body of an `if` whose condition did not hold, or a line Pob could not read in the first
place. The other is a statement whose own fill failed, which the replay is finished with either way.
Neither is ever asked about, and a slot left on one of them would be answered in place of the
statement below it, from a screenshot taken for something else.

A `loop` is the one thing that puts a slot back into the file, which is the same step run backwards
and holds for the same reason. What it puts back is its own header and the statements under it —
everything above them is filled or written out by the time a pass begins, and everything below is
still as it was written — so the first slot left in the file is again the one the replay is waiting
on.

A macro with no slot never runs psl at all, and needs nothing installed. A macro that has one needs
psl to be found — Pob checks over the whole macro, and over the files it `call`s, before the first
statement runs rather than partway through, and puts up **psl needed** instead of moving the cursor.
Every fill is kept under `logs/<session>/slots/<n>/` with the screenshot it was answered from and
what psl said while filling it (see [Logs](05_Logs.md)); `pob --session <id>` lists them. The whole
macro as it ended up — every slot filled, in one file — is kept beside them as
`logs/<session>/macro.txt`.


Calls
-----

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below. Most of these are also the tools the AI
calls and the actions the [MCP](08_MCP.md) server exposes: one vocabulary, whoever is driving. Any
argument can be an AI slot instead of a value, or hold one inside it.

This table is also what Pob describes to psl on every fill — a model asked for part of a statement
is told what the statement is a call to.

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `move(dx, dy)` | numbers | Nudge the cursor by a relative pixel offset. Positive `dx` right, positive `dy` down |
| `click()` | — | Left-click at the cursor |
| `rightClick()` | — | Right-click at the cursor |
| `doubleClick()` | — | Double-click at the cursor |
| `drag(dx, dy)` | numbers | Drag from the cursor by `(dx, dy)`. The cursor ends at the new position |
| `scroll(dx, dy)` | numbers | Scroll at the cursor. `dy > 0` down, `dy < 0` up, `dx > 0` right |
| `typeText(text)` | one string | Type text at the current keyboard focus |
| `keyPress(key)` | one string | Press a key, with `+`-joined modifiers in front of it — `return`, `cmd+v`, `ctrl+shift+t`. See [Key names](04_Keys.md) |
| `sleep(milliseconds)` | number | Pause |
| `resetCursor()` | — | Send the cursor back to the origin it starts at |
| `take_screenshot(x?, y?, w?, h?)` | numbers, all four or none | Capture a screenshot into the session's `screenshots/`. With all four, crop to that region |
| `stop` | — | End the run here. Written without parentheses; `stop()` is read too |
| `call(path)` | one string | Replay another PSL file here, then carry on below it |

The last two are about the run rather than the machine, and have sections of their own below.

Numbers are written plainly — `398`, `-615`, `0.5`. Strings are written in double quotes, and a
backslash escapes the character after it, which is how a quote gets inside one:
`typeText("say \"hi\"")`. A quoted string is one whole argument, commas and all, so
`typeText("a, b")` types `a, b` rather than passing two arguments.

Whitespace around a statement and between arguments is ignored, which is what lets an `if` body be
indented.


if blocks
---------

A macro plays the same actions every time, which is the point of one — until whole parts of it
should not always happen. An `if` guards a block with a condition: it runs when the condition holds
and is skipped when it does not.

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
    sleep(500)
}
```

The keyword is `if`, the condition is the parenthesised expression between it and the `{` that ends
the line, and a `}` on a line of its own closes the block. Inside is ordinary PSL — including
another `if` or a `loop`, nested as deep as the macro needs. Lines after the `}` run either way;
there is no `else`.

The condition is either an AI slot, which is the usual way and the one above, or `true` / `false`
written out — which asks nothing and costs nothing, and is how a block is parked without deleting
it. Anything else in the parentheses is not a condition Pob can read, and the block is dropped
rather than run unguarded. A filled-in condition is read the same way, case and quotes allowed: a
model that answered `"True"` has answered true.

Write the keyword lowercase. `IF` is read too, and so is `If`: a block Pob failed to recognise would
run its body unguarded, which is the one thing the condition was written to prevent.

A slot inside a block that gets skipped is never reached, so it is never filled and costs nothing.
The check for psl runs the other way round — over the whole macro, before the first statement: with
a slot anywhere in it and no psl to be found, Execute puts up **psl needed** and the macro does not
run at all, before the cursor has moved. Finding out halfway through would leave everything above
the slot already played.

Recording never writes an `if`, or any other slot. They are the part you write by hand, into a macro
that is otherwise recorded.


loop blocks
-----------

An `if` runs a block once or not at all. A `loop` runs it again and again:

```
loop (3) {
    keyPress("down")
    click()
}
```

The keyword is `loop`, the parentheses hold a count, and a `}` on a line of its own closes the
block — the same shape as an `if`, and lowercase for the same reason, though `LOOP` and `Loop` are
read too. That block runs three times.

The count is written out as a whole number rather than asked. It is the bound on a loop that could
otherwise not end, and a bound the model picks fresh on every pass is not a bound.

A loop that should stop when the screen says so takes a condition in front of the count:

```
loop (:: the window is still open ::, 5) {
    move(:: the x offset to the Close button ::, 0)
    click()
}
```

The condition is checked before every pass, the first one included, and the loop ends the moment it
does not hold. The count is the most passes it may make — five here — so that block runs until the
window is closed, or five times, whichever comes first. It is the condition an `if` takes, read the
same way: a slot, or `true` / `false` written out. A comma inside a slot is part of the instruction,
so `loop (:: still loading, not empty ::, 4)` asks what it looks like it asks; the count is what
follows the last comma the header has of its own.

A condition written out asks nothing and costs nothing, the same as an `if`'s, which makes the two
ends of the language meet: `loop (3)` and `loop (true, 3)` are one and the same loop — a loop
written without a condition is a loop whose condition always holds, and the count is what ends
either of them. `loop (false, 3)` is the other end of that, and is how a loop is parked without
deleting it: the check fails before the first pass and the body never runs.

Inside is ordinary PSL, including another `loop` or an `if`, nested as deep as the macro needs.

Asked again on every pass

Every pass puts the loop's statements back the way they were written, so the slots in them are
asked again from the screen as it is at that moment. That is the whole point of the pair: `:: the x
offset to the Close button ::` is a different number once the first dialog is gone, and a condition
that could only be answered once would never turn false.

Each of those is a psl run of its own and is kept as its own numbered directory under
`logs/<session>/slots/`, so five passes over one slot leave five of them, in the order they were
filled (see [Logs](05_Logs.md)). The compiled `macro.txt` is one file and therefore holds the
answers of the pass that ran last.

The model is shown the macro and a screenshot and nothing else, so it does not know which pass it is
being asked about — a slot that would have to count them is one it cannot answer. Write instructions
about what is on the screen now: "the window is still open", "there is another unread message", "the
list is still loading".

A loop is one statement in the macro however many passes it makes, and Execute's count of what it is
about to run says so. What the log says is the passes: one line as each begins, and one for the
verdict that ended it.


stop
----

`stop` ends the run where it stands:

```
if (:: a password prompt is on screen ::) {
    stop
}
typeText("the next thing")
```

Nothing under it runs — not the statements after it, not the rest of the block it is in, not the
passes a `loop` around it had left, and not the file that called the file it is written in. It is
the Stop button reached from inside the macro, and it takes effect between one statement and the
next in exactly the same way. There is no delay after it: `macro_default_delay` is the gap before
the statement that would have been next, and there is no such statement.

It is the one statement written without parentheses, because the parentheses would hold nothing.
`stop()` is read as well, for anyone who would rather write every statement the same shape. The name
is lowercase like every other name and read that way — `STOP` is a line that cannot be read, and is
logged and skipped like any other.

A run that reaches a `stop` has finished, as against a run someone stopped: the session is written
out and `stop_hook` fires, the same as a run that fell off the last line. The Stop button is the one
that does neither.

Where this earns its place is beside an `if`. A macro that has noticed something it was not written
for — a login screen, an error dialog, a list that came back empty — has nothing useful to do with
the forty statements below, and `stop` is how it says so.


call
----

`call` replays another PSL file where it stands, and comes back to the statement under it:

```
call("../sign-in.psl")
move(398, 915)
click()
call("../sign-out.psl")
```

The argument is a path, and a relative one is relative to the directory of the file the `call` is
written in — not to wherever Pob was started from. The macro lives in `~/.pob/<instance>/`, so
`call("../sign-in.psl")` is `~/.pob/sign-in.psl`, beside the instance directories and shared by all
of them. A path beginning with `~/` is under the home directory, and an absolute path is itself.

The called file is ordinary PSL: the same statements, the same blocks, the same `:: … ::` slots, and
its own `call`s, resolved against its own directory. It is read at the moment the call is reached
rather than once at the start, so a `call` inside a `loop` replays the file as it is written on
every pass — and editing a called file between two runs takes effect on the second, the way editing
the macro does.

What it is for is the piece of a macro that is the same in five macros. Signing in, closing whatever
dialog this application opens on startup, the six statements that get from the home screen to the
place the work happens: recorded once, kept in one file, and called from each of the macros that
needs it.

A file that calls itself, or a ring of files that comes back round to one already running, is a
replay with no end in it. Pob refuses that call rather than making it, and says so in the log; the
statements around it still run. Eight files deep is as far as `call` goes, which is the bound on the
other shape of the same mistake — a chain where every file is a new one.

Each file is its own program as far as psl is concerned. It is the file handed over on a fill, and
the line numbers a slot comes back to are its own — so the log names the file in front of the line
once more than one is in play, and `logs/<session>/slots/<n>/slot.json` records which file each fill
was a line of. The slot directories are numbered straight through the session however many files
filled them.

The check for psl follows `call` as well: a macro with no slot of its own that calls a file with one
still needs psl, and Pob reads the called files before the cursor moves rather than finding out
partway down. A `call` whose path is itself a slot cannot be read ahead — but a macro with a slot in
it needs psl anyway, which is the same answer.

`logs/<session>/macro.psl` and `macro.txt` are the macro, as they have always been. A called file is
not copied into the session; what the run made of it is in the log and in the slots it filled.


When something is wrong
-----------------------

Nothing here stops the run. PSL is read as far as it makes sense and what is left of it is
played, because the alternative — refusing a forty-line macro over line thirty-one — helps nobody.
The one thing that is never done is running statements a broken `if` was written to guard.

| Written | What happens |
|---------|--------------|
| A line that is not a call — no parentheses, nothing after the `)` | Logged and skipped |
| A name that is not one of the statements above | Logged and skipped |
| A call whose numbers cannot be read — `move(1)`, `scroll(a, b)` | Does nothing, and says nothing |
| A statement that does not read as PSL once its slots are filled | Logged with what it was filled to, and skipped |
| A slot psl cannot fill — no model configured, no answer, no screenshot | The statement is skipped, with the reason in the log; a condition then reads as false, so its block is skipped |
| An `if` missing its parentheses or the `{` at the end of the line, or holding neither a slot nor `true`/`false` | Its whole block is dropped, and the drop is logged |
| An `if` whose condition fills to something other than `true` or `false` | Reads as false: the block is skipped, with what it filled to in the log |
| An `if` whose `}` is missing | The end of the macro closes it |
| A `loop` missing its parentheses or the `{`, or whose count is not a whole number of 1 or more — `loop (:: how many ::)`, `loop (2.5)`, `loop (0)` | Its whole block is dropped, and the drop is logged |
| A `loop` whose condition is neither a slot nor `true`/`false` | Its whole block is dropped, and the drop is logged |
| A `loop` whose condition fills to something other than `true` or `false` | Reads as false: the loop ends there, with what it filled to in the log |
| A `loop` whose `}` is missing | The end of the macro closes it |
| A `}` with no `if` or `loop` above it | Logged and skipped |
| Markers touching a letter or digit outside them — `std::cout` | Not a slot; it stays in the statement as written |
| A slot in a macro, or in a file it calls, with psl not installed | The macro does not run at all: **psl needed** goes up before the cursor moves |
| A `call` naming a file that is not there, or cannot be read | Logged and skipped; the statements around it still run |
| A `call` with no path — `call()` | Logged and skipped |
| A `call` reaching a file that is already running, directly or round a ring | Not made, and the refusal is logged. The statements around it still run |
| A `call` nine files deep | Not made, and the depth is logged |
| `stop` mis-spelled or mis-cased — `STOP`, `halt` | A line that cannot be read: logged and skipped, and the run carries on |


How it runs
-----------

The cursor starts at the origin — a replay resets it there first — and every `move` and `drag` is
relative to where it already is. That is why `resetCursor()` is recorded when something sent the
cursor home mid-sequence: skip the jump back and every move after it starts from the wrong place.

All coordinates are screenshot pixels, origin top-left, x right, y down. The cursor is held inside
the Pob window: a move that would take it past an edge stops at the edge, since everything the
macro addresses — what the screenshots show, what the clicks go through — is inside that window.

Between one call and the next Pob waits `macro_default_delay` milliseconds, one second by default
(see [Settings](06_Settings.md)). A UI that needs longer gets an explicit `sleep()`. Judging an `if`
adds no delay of its own — the model call is the wait — and neither does going round a `loop`, or
stepping into a `call`: the gap between one pass or one file and the next is the delay after the
last statement before it, as it would be anywhere else.

Stop halts the run between statements, and during a `sleep()` rather than after it. A run that
reaches the end fires `stop_hook`, if one is set; a stopped run does not. A run that reached a
`stop` statement did reach its end, and fires it.


See also
--------

- [UI](02_UI.md) — the PSL, record and Execute buttons
- [Key names](04_Keys.md) — what `keyPress` accepts
- [Settings](06_Settings.md) — `macro_default_delay`, `image_scale`, and where the `psl` executable is
- [psl](https://github.com/pob/psl) — the compiler, its `.pslrc`, and the models it is pointed at
- [Logs](05_Logs.md) — the session a run writes, and where each slot the AI filled is kept
- [CLI](07_CLI.md) — `pob macro` runs `macro.psl` from the terminal
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file


Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps one per instance, in a file called `macro.psl`, and **Prompt Script Language** — PSL — is
what that file is written in. This page is both: the macro, and the language it is a macro in.

PSL is not Pob's own. It is a language with a compiler of its own — [psl](https://github.com/pob/psl)
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

There are two kinds of statement: a **call**, which does something to the machine, and an **if
block**, which guards the statements inside it with a condition. Either one can hold an **AI
slot** — a piece of a statement that is a prompt rather than a value, filled in as the replay
reaches it.


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
`if`:

```
move(:: the x offset to the Save button ::, 0)
typeText(:: what to reply to this message ::)
typeText("Hi :: the name at the top of the chat ::, thanks!")
if (:: a save dialog is on screen ::) {
    keyPress("return")
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

Coordinates come back as screenshot pixels, and `move` and `drag` are relative to where the cursor
is now — the arrow the model can see in the screenshot — so what it answers is an offset from there,
not a position on the screen.

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

A macro with no slot never runs psl at all, and needs nothing installed. A macro that has one needs
psl to be found — Pob checks over the whole macro before the first statement runs rather than
partway through, and puts up **psl needed** instead of moving the cursor. Every fill is kept under
`logs/<session>/slots/<n>/` with the screenshot it was answered from and what psl said while filling
it (see [Logs](05_Logs.md)); `pob --session <id>` lists them. The whole macro as it ended up — every
slot filled, in one file — is kept beside them as `logs/<session>/macro.txt`.


Calls
-----

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below. These are also the tools the AI calls
and the actions the [MCP](08_MCP.md) server exposes: one vocabulary, whoever is driving. Any
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
another `if`, nested as deep as the macro needs. Lines after the `}` run either way; there is no
`else`.

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
| A `}` with no `if` above it | Logged and skipped |
| Markers touching a letter or digit outside them — `std::cout` | Not a slot; it stays in the statement as written |
| A slot in a macro, with psl not installed | The macro does not run at all: **psl needed** goes up before the cursor moves |


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
adds no delay of its own — the model call is the wait.

Stop halts the run between statements, and during a `sleep()` rather than after it. A run that
reaches the end fires `stop_hook`, if one is set; a stopped run does not.


See also
--------

- [UI](02_UI.md) — the PSL, record and Execute buttons
- [Key names](04_Keys.md) — what `keyPress` accepts
- [Settings](06_Settings.md) — `macro_default_delay`, and where the `psl` executable is
- [psl](https://github.com/pob/psl) — the compiler, its `.pslrc`, and the models it is pointed at
- [Logs](05_Logs.md) — the session a run writes, and where each slot the AI filled is kept
- [CLI](07_CLI.md) — `pob macro` runs `macro.psl` from the terminal
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file

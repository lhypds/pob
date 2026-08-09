
Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps one per instance, in a file called `macro.psl`, and **Prompt Script Language** — PSL — is
what that file is written in. This page is both: the macro, and the language it is a macro in.

The language is small enough that a recording is readable, and readable enough that the recording is
worth editing afterwards. That is the whole point of the pair — you record a macro by doing the
thing once, and what you get back is a program you can open.

The name is what sets PSL apart from a scripting language that only ever does what it is told. A
statement can hold a prompt instead of a value — `::…::`, an AI slot — and what the AI answers is
what the line then says. A macro written in PSL is part instruction and part question: it repeats
what it was given, and asks about what it could not be.

```
move(398, 915)
click()
drag(-775, -615)
if (::the window focus on a wechat user::) {
    move(128, 738)
    click()
}
typeText("done")
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
block**, which asks the AI whether to run the statements inside it. What it asks is written as an
**AI slot** — the piece of a statement that is a prompt rather than a value.


AI slot
-------

An AI slot is a prompt written where a value would go, wrapped in `::` on both sides:

```
::instruction::
```

The instruction between the markers is a question for the model rather than something Pob carries
out itself. When the replay reaches the statement holding it, Pob takes a screenshot, asks the
[model](06_Settings.md) that instruction against that picture, and the answer stands where the slot
was written — the statement then says what the AI answered. Everything outside the markers is
written down and means exactly what it says.

That is the *prompt* in Prompt Script Language, and the whole of what separates it from a scripting
language. A call is a macro doing what it was told. A slot is a macro asking about a screen nobody
could describe to it in advance, at the moment it is looking at that screen.

Write an instruction a screenshot can settle — "a chat window is open", "the file list is empty",
"the window focus on a wechat user". The model is given the instruction and the picture and nothing
else: it has no memory of the statements that ran before, so an instruction about what the macro has
already done is one it cannot see. Whitespace around the instruction is trimmed, so `::a::` and
`:: a ::` are the same slot; an empty one is not a slot at all, and the statement holding it is
malformed.

Every slot is one model call, made as the replay reaches it — so a macro with no slot never calls
the model at all, and runs with nothing configured. A macro that has one needs an
`openai_api_key`, and Pob checks for it before the first statement runs rather than partway through.

Today a slot goes in one place: the condition of an `if`, below, where the answer that replaces it
is true or false. The `if` is written so the parentheses can hold whatever else comes later, but the
slot is the only expression there is so far.


Calls
-----

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below. These are also the tools the AI calls
and the actions the [MCP](08_MCP.md) server exposes: one vocabulary, whoever is driving.

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

A macro plays the same actions every time, which is the point of one — until the screen it plays
against is not always the same screen. `if` is where the AI comes into a macro: its condition is an
AI slot, and the true or false that comes back is what the condition then is. The block runs when it
holds, and is skipped when it does not.

```
if (::a save dialog is on screen::) {
    keyPress("return")
    sleep(500)
}
```

The keyword is `if`, the condition is the parenthesised expression between it and the `{` that ends
the line, and a `}` on a line of its own closes the block. Inside is ordinary PSL — including
another `if`, nested as deep as the macro needs. Lines after the `}` run either way; there is no
`else`.

Write the keyword lowercase. `IF` is read too, and so is `If`: a block Pob failed to recognise would
run its body unguarded, which is the one thing the condition was written to prevent.

A condition inside a block that gets skipped is never reached, so it is never judged and costs
nothing. The check for the settings that judging needs runs the other way round — over the whole
macro, before the first statement: with a slot anywhere in it and no `openai_api_key`, Execute puts
up **Settings needed** and the macro does not run at all, before the cursor has moved. Finding out
halfway through would leave everything above the `if` already played. (`base_url` and `model` have
working defaults, so the key is what a fresh machine is missing — see [Settings](06_Settings.md).)

Each judgement is kept under `logs/<session>/conditions/<n>/`, with the screenshot it was judged
from (see [Logs](05_Logs.md)), and `pob --session <id>` lists them.

Recording never writes an `if`. It is the part you write by hand, into a macro that is otherwise
recorded.


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
| An `if` missing its parentheses, its `::…::` slot or the `{` at the end of the line — or holding an empty slot | Its whole block is dropped, and the drop is logged |
| An `if` whose `}` is missing | The end of the macro closes it |
| A `}` with no `if` above it | Logged and skipped |
| An `if` the model cannot judge — no answer, an unreadable one, no screenshot | Reads as false: the block is skipped, with the reason in the log |
| An `if` in a macro with no `openai_api_key` | The macro does not run at all: **Settings needed** goes up before the cursor moves |


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
- [Settings](06_Settings.md) — `macro_default_delay`, and the model an `if` is judged with
- [Logs](05_Logs.md) — the session a run writes, and where each `if` judgement is kept
- [CLI](07_CLI.md) — `pob macro` runs `macro.psl` from the terminal
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file

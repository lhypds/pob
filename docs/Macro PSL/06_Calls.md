
Calls
=====

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below. Any argument can be an
[AI slot](05_AI%20slot.md) instead of a value, or hold one inside it.

A value is one of three things.

**Numbers** are written plainly — `398`, `-615`, `0.5`.

**Strings** are written in double quotes, and a backslash escapes the character after it, which is
how a quote gets inside one: `typeText("say \"hi\"")`. A quoted string is one whole argument, commas
and all, so `typeText("a, b")` types `a, b` rather than passing two arguments.

A string nobody closed runs to the closing parenthesis: `typeText("hello)` types `hello`, the same
as `typeText("hello")`. That is what the quote is written back as too — in the log, in the slot
record, and in the line the replay goes on with — so a statement always reads as the one that ran.
This matters where a slot sits inside a string, since psl answers in the shape of the line it was
given: a macro written `typeText("::what to say::)` comes back closed.

**Times** are written as a number with its unit on the end — `250ms`, `3s`, `10m`, `5h` — with no
quotes around them and no space in the middle. The units are `ms`, `s`, `m` and `h`, and writing
them one after another adds them up: `10h5m` is ten hours and five minutes. The number in front of a
unit may be fractional, so `0.5s` and `500ms` are the same time said two ways.

The unit is what makes a time a time, and it is never left off: `sleep(500)` is a number where a
time goes, and the check says so rather than guessing which unit was meant. Nor does a time go in
quotes — `sleep("10m")` is a string, and a string is not a time however much it looks like one.
Both are refused before the run, and both are refused again if a slot fills to one. `sleep` is the
only call that takes a time.

Whitespace around a statement and between arguments is ignored, which is what lets an `if` body be
indented.

A call does one of three things, and the tables below are that split: it acts on the machine through
the window, it hands a command line to the machine's own shell, or it says something about the run
itself. All of them are what Pob describes to psl on every fill — a model asked for part of a
statement is told what the statement is a call to.


The machine
-----------

These are also the tools the AI calls and the actions the [MCP](../Pob/08_MCP.md) server exposes:
one vocabulary, whoever is driving.

Perception:

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `takeScreenshot(x?, y?, w?, h?)` | numbers, all four or none | Capture a screenshot into the session's `screenshots/`. With all four, crop to that region |

Pointer:

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `resetCursor()` | — | Send the cursor back to the origin it starts at. By default it's (20, 20). |
| `move(dx, dy)` | numbers | Nudge the cursor by a relative pixel offset. Positive `dx` right, positive `dy` down |
| `moveTo(x, y)` | numbers | Put the cursor at an absolute position, measured from the top-left of the screenshot |
| `click(x?, y?)` | numbers, both or none | Left-click at the cursor. With `(x, y)`, go to that absolute position and click there |
| `rightClick(x?, y?)` | numbers, both or none | Right-click, the same two ways |
| `doubleClick(x?, y?)` | numbers, both or none | Double-click, the same two ways |
| `drag(dx, dy)` | numbers | Drag from the cursor by `(dx, dy)`. The cursor ends at the new position |
| `dragTo(x, y)` | numbers | Drag from the cursor to an absolute position. The cursor ends there |
| `scroll(dx, dy)` | numbers | Scroll at the cursor. `dy > 0` down, `dy < 0` up, `dx > 0` right |

Keyboard and timing:

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `typeText(text)` | one string | Type text at the current keyboard focus |
| `keyPress(key)` | one string | Press a key, with `+`-joined modifiers in front of it — `return`, `cmd+v`, `ctrl+shift+t`. A single character presses the key that produces it here, shift and all: `"*"`, `"="`, `"+"`. See [Key names](../Pob/04_Keys.md) |
| `sleep(time)` | one time | Pause — `sleep(250ms)`, `sleep(3s)`, `sleep(10m)`, `sleep(10h5m)` |


Relative and absolute
---------------------

The pointer calls come in pairs, and the name says which kind of coordinate each one takes. `move`
and `drag` are measured from wherever the cursor is now; `moveTo` and `dragTo` are measured from the
top-left corner of the screenshot, and take no notice of where the cursor was. A click is both in
one call: written bare it clicks where the cursor already is, and written with a target it goes
there and clicks, which is a `moveTo` and a `click` said as one statement.

```
moveTo(398, 915)
click()

click(398, 915)     // the same two things, one statement
```

Both are screenshot pixels either way, and the cursor is left where it ended up, so the two kinds
mix freely — `click(398, 915)` followed by `move(0, -40)` moves forty pixels up from the click.

[Recording](02_src.md) writes the relative pair — that is what the shells watch happen, and
what an MCP tool's absolute `(x, y)` is written down as. The absolute calls are the ones to reach
for when writing or editing a macro by hand, and when an [AI slot](05_AI%20slot.md) is what fills in
the numbers: a model looking at a screenshot can read a position off it directly, while an offset is
that position minus wherever the cursor got to.


The shell
---------

One statement, and it is the machine under the window rather than the window: a sound played, a file
moved, a script the macro has no other way of asking for. What is written inside the quotes is a
command line, so the shell's own quoting, pipes and `&` are all there and mean what they always did.
It has a page of its own — [run](14_run.md).

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `run(command)` | one string | Hand one command line to this machine's shell and wait for it |

```
run("afplay /System/Library/Sounds/Morse.aiff")
```


The run
-------

These two never reach the machine. They are what a macro says about its own replay — where it ends,
and which file it goes on with — and each has a page of its own: [stop](10_stop.md) and
[call](11_call.md).

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `stop()` | — | End the run here. Nothing under it runs, in this file or in the one that called it |
| `call(path)` | one string | Replay another PSL file here, then carry on below it |


See also
--------

- [Key names](../Pob/04_Keys.md) — what `keyPress` accepts
- [AI slot](05_AI%20slot.md) — writing a prompt where an argument goes
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools
- [run](14_run.md) — the command line, the directory it runs from, and the minute it has to finish in
- [When something is wrong](12_When%20something%20is%20wrong.md) — a call the check refuses

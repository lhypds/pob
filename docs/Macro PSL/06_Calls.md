
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

A call does one of two things, and the two tables are that split: it acts on the machine, or it says
something about the run itself. Both tables are what Pob describes to psl on every fill — a model
asked for part of a statement is told what the statement is a call to.


The machine
-----------

These are also the tools the AI calls and the actions the [MCP](../Pob/08_MCP.md) server exposes:
one vocabulary, whoever is driving.

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `move(dx, dy)` | numbers | Nudge the cursor by a relative pixel offset. Positive `dx` right, positive `dy` down |
| `click()` | — | Left-click at the cursor |
| `rightClick()` | — | Right-click at the cursor |
| `doubleClick()` | — | Double-click at the cursor |
| `drag(dx, dy)` | numbers | Drag from the cursor by `(dx, dy)`. The cursor ends at the new position |
| `scroll(dx, dy)` | numbers | Scroll at the cursor. `dy > 0` down, `dy < 0` up, `dx > 0` right |
| `typeText(text)` | one string | Type text at the current keyboard focus |
| `keyPress(key)` | one string | Press a key, with `+`-joined modifiers in front of it — `return`, `cmd+v`, `ctrl+shift+t`. See [Key names](../Pob/04_Keys.md) |
| `sleep(time)` | one time | Pause — `sleep(250ms)`, `sleep(3s)`, `sleep(10m)`, `sleep(10h5m)` |
| `resetCursor()` | — | Send the cursor back to the origin it starts at |
| `takeScreenshot(x?, y?, w?, h?)` | numbers, all four or none | Capture a screenshot into the session's `screenshots/`. With all four, crop to that region |


The run
-------

These two never reach the machine. They are what a macro says about its own replay — where it ends,
and which file it goes on with — and each has a page of its own: [stop](09_stop.md) and
[call](10_call.md).

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `stop()` | — | End the run here. Nothing under it runs, in this file or in the one that called it |
| `call(path)` | one string | Replay another PSL file here, then carry on below it |


See also
--------

- [Key names](../Pob/04_Keys.md) — what `keyPress` accepts
- [AI slot](05_AI%20slot.md) — writing a prompt where an argument goes
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools
- [When something is wrong](11_When%20something%20is%20wrong.md) — a call the check refuses

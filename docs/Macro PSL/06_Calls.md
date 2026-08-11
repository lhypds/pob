
Calls
=====

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below. Most of these are also the tools the AI
calls and the actions the [MCP](../Pob/08_MCP.md) server exposes: one vocabulary, whoever is driving. Any
argument can be an [AI slot](05_AI%20slot.md) instead of a value, or hold one inside it.

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
| `keyPress(key)` | one string | Press a key, with `+`-joined modifiers in front of it — `return`, `cmd+v`, `ctrl+shift+t`. See [Key names](../Pob/04_Keys.md) |
| `sleep(milliseconds)` | number | Pause |
| `resetCursor()` | — | Send the cursor back to the origin it starts at |
| `take_screenshot(x?, y?, w?, h?)` | numbers, all four or none | Capture a screenshot into the session's `screenshots/`. With all four, crop to that region |
| `stop` | — | End the run here. Written without parentheses; `stop()` is read too |
| `call(path)` | one string | Replay another PSL file here, then carry on below it |

The last two are about the run rather than the machine, and have pages of their own:
[stop](09_stop.md) and [call](10_call.md).

Numbers are written plainly — `398`, `-615`, `0.5`. Strings are written in double quotes, and a
backslash escapes the character after it, which is how a quote gets inside one:
`typeText("say \"hi\"")`. A quoted string is one whole argument, commas and all, so
`typeText("a, b")` types `a, b` rather than passing two arguments.

Whitespace around a statement and between arguments is ignored, which is what lets an `if` body be
indented.


See also
--------

- [Key names](../Pob/04_Keys.md) — what `keyPress` accepts
- [AI slot](05_AI%20slot.md) — writing a prompt where an argument goes
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools
- [When something is wrong](11_When%20something%20is%20wrong.md) — a call the check refuses

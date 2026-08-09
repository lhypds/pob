
Macro
=====

A macro is a recorded or hand-written sequence of actions stored in `macro.txt`. Each line is one
function call using the same syntax as the AI tools below.

Example `macro.txt`:

```
move(100, 200)
click()
sleep(500)
typeText("hello")
keyPress("return")
```

Use the record button (⏺) in the toolbar to record actions during an AI session — they are appended to `macro.txt` automatically. Use the play button (▶) to run the macro directly without the AI.

Starting a recording while `macro.txt` still holds actions asks what to do with them first: clear
them, or keep them and record after them. Keeping them writes a `resetCursor()` between the old
lines and the new ones, since every move recorded next is relative to the origin a replay starts at.

Recording captures every action that drives the machine, whichever one of the three is
driving it: your own mouse and keyboard, the AI session's tool calls, and the tools an
[MCP](08_MCP.md) client calls. They all append to the same `macro.txt`, in the order
things happened. The MCP tools that take an absolute `(x, y)` are written down as the
relative `move(dx, dy)` this vocabulary replays, so a recording made through MCP plays
back like any other.

Your own mouse and keyboard are recorded on macOS only, for now — watching the
input of other applications is a different mechanism on each system, and the
Linux and Windows shells do not have it yet. On those two the record button
still captures everything the AI session and MCP clients drive.


Functions
---------

These are the tools the AI can call during a session, and the same vocabulary a
macro line is written in — written out as a language, with the quoting rules and
the blocks, in [Prompt Script Language](16_Prompt%20Script%20Language.md):  

| Function | Parameters | Description |
|----------|------------|-------------|
| `move(dx, dy)` | `dx`: number, `dy`: number | Nudge the cursor by a relative pixel offset. Positive `dx` = right, positive `dy` = down. Returns a new screenshot showing the updated cursor position. |
| `click()` | — | Left-click at the current cursor position. |
| `rightClick()` | — | Right-click at the current cursor position. |
| `doubleClick()` | — | Double-click at the current cursor position. |
| `drag(dx, dy)` | `dx`: number, `dy`: number | Drag from the current cursor position by `(dx, dy)` pixels. Cursor ends at the new position. |
| `scroll(dx, dy)` | `dx`: number, `dy`: number | Scroll at the current cursor position. `dy > 0` = down, `dy < 0` = up, `dx > 0` = right. |
| `typeText(text)` | `text`: string | Type text at the current keyboard focus. |
| `keyPress(key)` | `key`: string | Press a key, optionally with `+`-joined modifiers in front of it (see [Key names](04_Keys.md)) — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. |
| `sleep(milliseconds)` | `milliseconds`: number | Pause execution for the given number of milliseconds. |
| `resetCursor()` | — | Send the cursor back to the origin it starts at. Recorded when something reset it mid-sequence; replaying it keeps the relative moves around it landing where they did. |
| `take_screenshot(crop_x?, crop_y?, crop_width?, crop_height?)` | All optional: `crop_x`, `crop_y`, `crop_width`, `crop_height`: number | Capture a fresh screenshot. When all four crop parameters are provided, the image is cropped to that region (x, y, width, height in screenshot pixels). Saved to `logs/<instanceId>/<sessionId>/screenshots/<unixtime>.png`. |

All coordinates are in screenshot pixel space (origin = top-left, x increases right, y increases down).  
The cursor is held inside the Pob window: a move that would take it past an edge stops at the edge, since
everything it addresses — what the screenshots show, what the clicks are aimed through — is inside that window.  


if
--

A macro plays the same actions every time, which is the point of one — until the screen it plays
against is not always the same screen. `if` is where the AI comes into a macro: the condition goes in
parentheses, written in plain language between `::` and `::`, and when the replay reaches the line
Pob takes a screenshot and asks the [model](06_Settings.md) whether it holds right now. The block
runs when it does, and is skipped when it does not.

```
move(398, 915)
click()
drag(-775, -615)
if (::the window focus on a wechat user::) {
    move(128, 738)
    click()
}
typeText("done")
```

`::…::` is an AI slot: a prompt that stands where a value would, and is replaced by what the AI
answers when the line is reached. In the condition of an `if` that answer is true or false.

The condition is the parenthesised expression between the keyword and the `{` that ends the line,
and a `}` on a line of its own closes the block. What is inside is ordinary macro lines — including
another `if`, nested as deep as the macro needs. Lines after the `}` run either way. Write the
keyword lowercase; `IF` is read too, since a block that went unrecognised would run its body
unguarded.

Write the condition as something a screenshot can settle — "a chat window is open", "the file list
is empty", "a save dialog is on screen". The model is given the condition and the picture and
nothing else: it has no memory of the lines that ran before, so a condition about what the macro
has already done is one it cannot see.

Each `if` is one model call, and it is judged as the replay reaches it — a condition inside a block
that gets skipped costs nothing. A macro that never uses `if` never calls the model, and runs with
nothing configured exactly as it always has.

A macro that does use one needs the settings that model call is made with — `openai_api_key`, and
the `base_url` and `model` that already have working defaults (see [Settings](06_Settings.md)).
Without them Play puts up **Settings needed** and the macro does not run at all, before the cursor
has moved: finding out halfway through would leave everything above the `if` already played.

Anything that goes wrong once it is running reads as false: no answer from the provider, an answer
that cannot be read, no screenshot to judge from. The block is skipped and the reason is written to
the log, since a condition was put there to hold actions back and running them on a failed check is
the one outcome nobody asked for. A malformed `if` — one missing its parentheses, its `::…::` slot
or its `{` — skips its block the same way. An `if` whose `}` is missing is closed by the end of the
macro.

Each judgement is kept with the session, under `logs/<session>/conditions/<n>/` — the condition, the
verdict and the model's one-line reason in `condition.json`, beside the screenshot it was judged
from and the messages that were sent (see [Logs](05_Logs.md)). `pob --session <id>` lists them in the
order they were judged. Recording never writes an `if`: it is written by hand, into a macro that is
otherwise recorded.


See also
--------

- [Prompt Script Language](16_Prompt%20Script%20Language.md) — PSL, the language reference: every statement, quoting, blocks, and what a wrong line does
- [Key names](04_Keys.md) — what `keyPress` accepts
- [UI](02_UI.md) — the record and play buttons
- [MCP Server](08_MCP.md) — the same actions as MCP tools
- [CLI](07_CLI.md) — `pob macro` runs `macro.txt` from the terminal
- [Settings](06_Settings.md) — the API key and model an `if` is judged with
- [Logs](05_Logs.md) — where each `if` judgement is kept

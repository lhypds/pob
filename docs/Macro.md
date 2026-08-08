
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


Functions
---------

These are the tools the AI can call during a session, and the same vocabulary a
macro line is written in:  

| Function | Parameters | Description |
|----------|------------|-------------|
| `move(dx, dy)` | `dx`: number, `dy`: number | Nudge the cursor by a relative pixel offset. Positive `dx` = right, positive `dy` = down. Returns a new screenshot showing the updated cursor position. |
| `click()` | — | Left-click at the current cursor position. |
| `rightClick()` | — | Right-click at the current cursor position. |
| `doubleClick()` | — | Double-click at the current cursor position. |
| `drag(dx, dy)` | `dx`: number, `dy`: number | Drag from the current cursor position by `(dx, dy)` pixels. Cursor ends at the new position. |
| `scroll(dx, dy)` | `dx`: number, `dy`: number | Scroll at the current cursor position. `dy > 0` = down, `dy < 0` = up, `dx > 0` = right. |
| `typeText(text)` | `text`: string | Type text at the current keyboard focus. |
| `keyPress(key)` | `key`: string | Press a key, optionally with `+`-joined modifiers in front of it (see [Key names](Keys.md)) — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. |
| `sleep(milliseconds)` | `milliseconds`: number | Pause execution for the given number of milliseconds. |
| `take_screenshot(crop_x?, crop_y?, crop_width?, crop_height?)` | All optional: `crop_x`, `crop_y`, `crop_width`, `crop_height`: number | Capture a fresh screenshot. When all four crop parameters are provided, the image is cropped to that region (x, y, width, height in screenshot pixels). Saved to `logs/<instanceId>/<sessionId>/screenshots/<unixtime>.png`. |

All coordinates are in screenshot pixel space (origin = top-left, x increases right, y increases down).  
The cursor is held inside the Pob window: a move that would take it past an edge stops at the edge, since
everything it addresses — what the screenshots show, what the clicks are aimed through — is inside that window.  


See also
--------

- [Key names](Keys.md) — what `keyPress` accepts
- [UI](UI.md) — the record and play buttons
- [MCP Server](MCP.md) — the same actions as MCP tools
- [CLI](CLI.md) — `pob macro` runs `macro.txt` from the terminal


MCP Server
==========

Pob speaks MCP, so an MCP-compatible client — Claude Code, Claude Desktop,
Gemini CLI — can see the Pob window and drive the machine through it.

The server is built into `pob-core` (SSE transport). It does not start with the
app; start it from the CLI.


Starting it
-----------

```
pob mcp start [port]
```

The port defaults to `8032`; pass a different one after `start` if something
else has it. `pob mcp status` prints the URL, the tool list and a client config
snippet at any time, and `pob mcp stop` shuts the server down.

`mcp start` also registers the server (as `pob`) in the user settings of any
installed agent CLIs — Claude Code (`claude`) and Gemini CLI (`gemini`) — and
`mcp stop` removes those registrations again, so no manual setup is needed
there.

For other clients, register the printed URL manually. Claude Desktop
(`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "pob": {
      "url": "http://127.0.0.1:8032/sse"
    }
  }
}
```


Coordinates
-----------

All coordinates are **screenshot pixels**, origin at the top-left of the image
returned by `take_screenshot` — the client never deals with screen-level
positions. `take_screenshot` reports the image size alongside the PNG, so the
model can read a target's coordinates off the image and pass them straight to
the `*_to` / `move_and_*` tools. Every action returns the resulting cursor
position.

The cursor is held inside the Pob window: a move that would take it past an
edge stops at the edge, since everything it addresses — what the screenshots
show, what the clicks are aimed through — is inside that window.


Tools
-----

Perception:

| Tool | Parameters | Description |
|------|------------|-------------|
| `take_screenshot` | `crop_x?`, `crop_y?`, `crop_width?`, `crop_height?`: integer, `with_cursor?`: boolean | Capture the Pob window content area and return a PNG image plus its pixel dimensions. When all four crop parameters are provided, only that region is captured (coordinates read off a crop need the crop offset added back). `with_cursor` draws the virtual cursor into the image. |
| `get_cursor_position` | — | Current virtual cursor position, without moving or clicking. |

Pointer:

| Tool | Parameters | Description |
|------|------------|-------------|
| `reset_cursor` | — | Return the cursor to its home position. |
| `move_cursor` | `dx`, `dy`: number | Nudge the cursor by a relative offset. |
| `move_cursor_to` | `x`, `y`: number | Move the cursor to an absolute position. |
| `click` / `right_click` / `double_click` | — | Click at the current cursor position. |
| `move_and_click` | `x`, `y`: number | Move to an absolute position and left-click there, in one step. |
| `move_and_right_click` | `x`, `y`: number | Move and right-click — e.g. to open a context menu. |
| `move_and_double_click` | `x`, `y`: number | Move and double-click — e.g. to open an item. |
| `drag` | `dx`, `dy`: number | Drag from the cursor position by a relative offset. |
| `drag_to` | `x`, `y`: number | Drag from the cursor position to an absolute position. |
| `scroll` | `dx`, `dy`: number | Scroll at the cursor position. `dy > 0` scrolls down, `dx > 0` scrolls right. |
| `move_and_scroll` | `x`, `y`, `dx`, `dy`: number | Move to an absolute position and scroll there, to target one pane. |

Keyboard and timing:

| Tool | Parameters | Description |
|------|------------|-------------|
| `type_text` | `text`: string | Type text at the current keyboard focus (click the field first). |
| `key_press` | `key`: string | Press a key or shortcut — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. See [Key names](Keys.md). |
| `wait` | `milliseconds`: number | Pause to let the UI settle. Capped at 10000 ms. |


See also
--------

- [Key names](Keys.md) — what `key_press` accepts
- [Pob Server API](API.md) — the same actions over plain HTTP
- [CLI](CLI.md) — `pob mcp status` / `start` / `stop`


MCP Server
==========

Pob speaks MCP, so an MCP-compatible client — Claude Code, Claude Desktop,
Gemini CLI — can see the Pob window and drive the machine through it.

The server is built into `pob-core` (SSE transport) and starts with the
instance, on port `8032`:

```
http://127.0.0.1:8032/sse
```

A client is told that address once, in its own config, and reads it again on
every launch after that — so the server is up whenever the app is, rather than
waiting for someone to remember a command. `mcp_port` in
[`settings.json`](06_Settings.md) moves it, and `"mcp": false` keeps the port
closed on a machine that does not want it open.


Driving one machine from another
--------------------------------

The server binds every interface, so a client on another machine reaches it
with nothing configured first. Give that client the machine's own address in
place of `127.0.0.1`:

```json
{
  "mcpServers": {
    "pob": {
      "type": "sse",
      "url": "http://192.168.0.60:8032/sse"
    }
  }
}
```

Loopback keeps working — a wildcard bind holds `127.0.0.1` alongside every
other address — so a client pointed at `localhost` is unaffected, and the same
machine can be driven from itself and from another one at once.

What is open here. The endpoints take no credentials: anyone who can reach the
port can move the pointer, type, and read the screen through `take_screenshot`.
That is deliberately the same posture as the [Pob Server](09_Server.md) on
`8033`, which has bound every interface since it existed and whose
[Operation API](10_Operation%20API.md) types on this machine too — a closed
`8032` beside an open `8033` would have been a locked door in a wall with none.
So the decision is made once, for the machine: run it on a network you trust,
or close both.

Closing it is `mcp_host` in [`settings.json`](06_Settings.md), and the port
answers only this machine again:

```json
{
  "mcp_host": "127.0.0.1"
}
```

`"mcp": false` closes it altogether, and `"server": false` does the same for
`8033`. The setting is read when the server starts, so the app has to be
restarted after it changes; `POB_MCP_HOST=127.0.0.1` sets it for one launch
without touching the file.

A machine that already has `mcp_host` in its `settings.json` keeps the value
that is written there — backfilled defaults never overwrite a key that exists,
which is what stops a file someone has edited from being edited back. A machine
set up on an earlier version therefore has `127.0.0.1` in the file and stays
loopback-bound until that line is changed; `pob mcp status` says which it is.

One more thing before another machine can reach it: the host firewall has to
allow the port. It drops the connection rather than refusing it, which looks
exactly like a server that is not running.


A client that will not connect
------------------------------

Two things stop it, and they are told apart by what the failed connection does.
Ask the machine that is meant to be driven where its server is:

```
pob mcp status
```

It reports the bind alongside the addresses, and the bind is the first thing to
check:

```
MCP server: running
URL:        http://127.0.0.1:8032/sse
            http://192.168.0.60:8032/sse
Host:       0.0.0.0 (every interface — reachable from the network)
```

A `Host:` of `127.0.0.1` is a server that answers only its own machine, whatever
the client is pointed at — a machine set up on a version that defaulted to
loopback still has that line in its `settings.json`. Change it to `0.0.0.0` and
restart the app. The index page on [`8033`](09_Server.md) reports the same list,
so a machine with no terminal on it can be asked from a browser.

With the bind open and the client still hanging, it is the firewall. A
connection **refused** straight away means nothing is listening on that address
— the bind again. A connection that **hangs and times out** means the packets
are being dropped, which is what a firewall does:

```
curl -v http://192.168.0.60:8032/sse
```

Allow the port on the machine being driven — Windows Defender Firewall inbound
rule, `ufw allow 8032/tcp`, or the macOS application firewall, which allows per
program rather than per port and so covers `8033` and `8032` together. That the
Pob server on `8033` is reachable says nothing about `8032`: a rule was allowed
for one port, not for the app.


Registering it with a client
----------------------------

```
pob mcp start [port]
```

registers the server (as `pob`) in the user settings of any installed agent CLI
— Claude Code (`claude`) and Gemini CLI (`gemini`) — so no manual setup is
needed there. It is the running server that is registered; passing a `[port]`
moves it there first, for a machine where something else has `8032`. `pob mcp
stop` shuts the server down and removes those registrations again, and
`pob mcp status` prints the URL, the tool list and a client config snippet at
any time.

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

The virtual cursor is on screen while a client is connected, and gone again
once the last one disconnects: a connected client can move it at any moment,
and a move nobody can see looks like nothing happened. A server merely
listening — which it does from the moment the app starts — shows nothing.


Coordinates
-----------

All coordinates are **screenshot pixels**, origin at the top-left of the image
returned by `take_screenshot` — the client never deals with screen-level
positions. `take_screenshot` reports the image size alongside the PNG, so the
model can read a target's coordinates off the image and pass them straight to
the tools that take an absolute position. Every action returns the resulting
cursor position.

The name says which kind of coordinate a tool takes: `move`, `drag` and `scroll`
are measured from wherever the cursor is now, and `move_to` and `drag_to` are
measured from the top-left corner of the image. A click is both, and there is no
separate move-and-click tool because of it — hand `click` an `x` and `y` and it
goes there first, leave them out and it clicks where the cursor already is.

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
| `move` | `dx`, `dy`: number | Nudge the cursor by a relative offset. |
| `move_to` | `x`, `y`: number | Move the cursor to an absolute position. |
| `click` / `right_click` / `double_click` | `x?`, `y?`: number, both or neither | Click. With `x` and `y`, move to that absolute position and click there, in one step; with neither, click where the cursor already is. |
| `drag` | `dx`, `dy`: number | Drag from the cursor position by a relative offset. |
| `drag_to` | `x`, `y`: number | Drag from the cursor position to an absolute position. |
| `scroll` | `dx`, `dy`: number | Scroll at the cursor position. `dy > 0` scrolls down, `dx > 0` scrolls right. To scroll one pane, `move_to` it first. |

Keyboard and timing:

| Tool | Parameters | Description |
|------|------------|-------------|
| `type_text` | `text`: string | Type text at the current keyboard focus (click the field first). |
| `key_press` | `key`: string | Press a key or shortcut — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. See [Key names](04_Keys.md). |
| `sleep` | `seconds`: number | Pause to let the UI settle — fractions are fine, `0.25` is a quarter of a second. Capped at 10 s. |


While the record button (⏺) is on, tool calls made over MCP are appended to `macro.psl`
alongside the actions the AI and your own hand perform — see
[Macro PSL](03_Macro%20PSL.md).


See also
--------

- [Key names](04_Keys.md) — what `key_press` accepts
- [Macro PSL](03_Macro%20PSL.md) — recording MCP tool calls, and replaying them
- [Operation API](10_Operation%20API.md) — the same actions over plain HTTP
- [CLI](07_CLI.md) — `pob mcp status` / `start` / `stop`

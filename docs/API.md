
Pob Server API
==============

The [Pob server](Server.md) takes its commands as `text/plain`
POSTs, which is what the [Web UI](WebUI.md) sends and what makes the
same thing scriptable — anything that can send an HTTP request can move the
pointer and type on the machine.

```
curl -X POST --data 'typing=hello'          http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'keycode=CTRL+c,CTRL+v' http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'mouse=MOVE(40,10)'     http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'mouse=CLICK(0,0)'      http://192.168.1.40:8033/pb-a703/
```


Endpoint
--------

```
http://<host>:<server_port>/<instance-id>/
```

`pob status` prints the address — one line per network the machine is on. The
instance ID is the one in the toolbar, and since a machine keeps it for good
the address is worth writing down.

The bare root works for commands too — there is one instance to reach, so a
POST to `http://192.168.1.40:8033/` lands the same way. It is served where it
lands, never redirected, since most HTTP clients would turn a redirected POST
into a GET and lose the keystroke.

A GET on either address serves the [Web UI](WebUI.md) page instead.

The port is yours to set: `server_port` in `settings.json`, or `POB_SERVER_PORT`
in the environment. It is the same on every machine unless someone changes it,
so the address can be typed from memory.


Commands
--------

The protocol is the pico-hid board's, so its clients work against Pob
unchanged. One command per request body:

| Command | Form | Description |
|---------|------|-------------|
| `typing` | `typing=<text>` | Type `<text>` at the current keyboard focus |
| `keycode` | `keycode=<chord>` | Press keys. `,` separates keys pressed in turn, `+` joins keys held together — `CTRL+c,CTRL+v`. Uses the HID names in [Key names](Keys.md) |
| `mouse` | `mouse=ACTION(x,y)` | Pointer action: `MOVE`, `CLICK`, `RIGHT_CLICK`, `DOUBLE_CLICK`, `PRESS`, `RELEASE`, `SCROLL` |
| `consumer` | `consumer=<usage>` | Media and brightness keys. Accepted and ignored — the shells post plain key events and have nowhere to put a consumer-control usage |

An optional `seq=<token>&` prefix makes a retry safe to send twice:

```
curl -X POST --data 'seq=42&typing=hello' http://192.168.1.40:8033/pb-a703/
```


Reach
-----

**The server listens on every network interface**, so anyone on the same
network who knows the address can move this machine's pointer and type on it.
That is the point of it — but it is also why `"server": false` in
`settings.json` turns it off.


See also
--------

- [Pob Server](Server.md) — what serves these commands
- [Web UI](WebUI.md) — the page that sends them
- [Pob Keyboard](Keyboard.md) — a desktop client for this API
- [Key names](Keys.md) — what `keycode` accepts
- [MCP Server](MCP.md) — the same actions as MCP tools

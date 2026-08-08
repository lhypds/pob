
Operation API
=============

The API that *operates* the machine — pointer, keys, text. The [Pob
server](09_Server.md) takes its commands as `text/plain` POSTs, which is what the
[Web UI](12_WebUI.md) sends and what makes the same thing scriptable — anything
that can send an HTTP request can move the pointer and type on the machine.

(The other one is the [Control API](11_Control%20API.md), which drives the *app* —
run an instruction, stop a session — and is the `pob` CLI's private channel.)

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

That address is the machine itself, so the method says what you want of it:

| Method | Path | What it answers |
|--------|------|-----------------|
| `POST` | `/<instance-id>/` | Runs a command — the rest of this page |
| `GET` | `/<instance-id>/` | A PNG of what the machine looks like right now, virtual cursor included |
| `GET` | `/<instance-id>/control` | The [Web UI](12_WebUI.md) control page — text field, keyboard mirror, trackpad |
| `GET` | `/<instance-id>/view` | The [Web UI](12_WebUI.md) view page, which refetches that PNG on a clock you can set |
| `GET` | `/<instance-id>/status` | What is running here, as JSON — the same facts `pob status` prints |
| `GET` | `/` | The [Web UI](12_WebUI.md) index page: those facts, and the address above to go on with |

The three pages answer without the instance in the path as well —
`http://192.168.1.40:8033/control` is the same page. A **GET** on the bare root
is the one exception: it is the index, not the machine, so the shortest address
on the network does not answer with a picture of someone's screen. Anything
else under the address is a 404; nothing else is there.

The frame is a plain image, so watching the machine needs no more than an
`<img>` — or a shell:

```
curl -o now.png http://192.168.1.40:8033/pb-a703/
```

It is sent `Cache-Control: no-store`: a cached frame is a moment that has
already passed.

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
| `keycode` | `keycode=<chord>` | Press keys. `,` separates keys pressed in turn, `+` joins keys held together — `CTRL+c,CTRL+v`. Uses the HID names in [Key names](04_Keys.md) |
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

- [Pob Server](09_Server.md) — what serves these commands
- [Web UI](12_WebUI.md) — the page that sends them
- [Pob Keyboard](13_Keyboard.md) — a desktop client for this API
- [Key names](04_Keys.md) — what `keycode` accepts
- [MCP Server](08_MCP.md) — the same actions as MCP tools
- [Control API](11_Control%20API.md) — the other API: driving the app itself

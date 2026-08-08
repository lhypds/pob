
Pob Server
==========

Every instance runs a server, started with it — and since a machine runs one
instance, a machine has one address:

```
http://192.168.1.40:8033/pb-a703
```

`pob status` prints it — one line per network the machine is on. The instance
ID is the one in the toolbar, and since a machine keeps it for good the address
is worth writing down. The bare root is the same server, so
`http://192.168.1.40:8033` does just as well. Clicking the instance badge in
the toolbar copies the whole address.

The port is yours to set: `server_port` in `settings.json`, or
`POB_SERVER_PORT` in the environment. It is the same on every machine unless
someone changes it, so the address can be typed from memory.

**The server listens on every network interface**, so anyone on the same
network who knows the address can move this machine's pointer and type on it.
That is the point of it — but it is also why `"server": false` in
`settings.json` turns it off.


What it serves
--------------

The address itself is the machine, and the method says what you want of it:

- **POST** — a command, as `text/plain`, which makes driving the machine
  scriptable:

```
curl -X POST --data 'typing=hello'      http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'mouse=CLICK(0,0)'  http://192.168.1.40:8033/pb-a703/
```

- **GET** — a PNG of what the machine looks like right now, cursor included:

```
curl -o now.png                         http://192.168.1.40:8033/pb-a703/
```

Two pages sit under that address, both part of the [Web UI](12_WebUI.md):

- **`/control`** — the remote control page for a phone or any other browser on
  the network: text field, keyboard mirror, trackpad.
- **`/view`** — the same frame as the GET above, refetched once a second, so a
  second screen can watch the machine work.

Since the bare root is the same server, `http://192.168.1.40:8033/control`
reaches the control page too.

The protocol is the pico-hid board's, so its clients work against Pob
unchanged. The full command grammar is in
**[Operation API](10_Operation%20API.md)**.


See also
--------

- [Operation API](10_Operation%20API.md) — the command grammar
- [Web UI](12_WebUI.md) — the page this server hosts
- [Pob Keyboard](13_Keyboard.md) — a desktop client for the same API
- [Settings](06_Settings.md) — `server` and `server_port`

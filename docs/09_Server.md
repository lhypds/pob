
Pob Server
==========

Every instance runs a server, started with it — and since a machine runs one
instance, a machine has one address:

```
http://192.168.1.40:8033/pb-a703
```

`pob status` prints it — one line per network the machine is on. The instance
ID is the one in the toolbar, and since a machine keeps it for good the address
is worth writing down. Clicking the instance badge in the toolbar copies the
whole address.

If all you remember is the machine, `http://192.168.1.40:8033` opens the index:
what is running here, and this address to go on with.

The port is yours to set: `server_port` in `settings.json`, or
`POB_SERVER_PORT` in the environment. It is the same on every machine unless
someone changes it, so the address can be typed from memory.

**The server listens on every network interface**, so anyone on the same
network who knows the address can move this machine's pointer and type on it.
That is the point of it — but it is also why `"server": false` in
`settings.json` turns it off.


What it serves
--------------

The address that names the instance **is** the machine, and the method says
what you want of it:

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

  A full-size PNG is the frame to *read* and the most expensive one to make.
  For watching, `?format=jpeg&w=1280&q=70` asks for a JPEG no wider than
  1280 px — around a fifth of the bytes and an eighth of the time, which is
  what makes a watchable frame rate possible. See
  [Operation API](10_Operation%20API.md#asking-for-a-cheaper-frame).

Three pages sit around it, all part of the [Web UI](12_Web UI.md):

- **`/`** — the index. What is running here — instance, root, model, session,
  MCP, server — and the machine's own address to go on with. The bare root is
  deliberately *not* the machine: the shortest address on the network should
  not answer with a picture of someone's screen.
- **`/pb-a703/control`** — the remote control page for a phone or any other
  browser on the network: text field, keyboard mirror, trackpad.
- **`/pb-a703/view`** — the frame above, refetched at a rate you can set, with
  a text field under it. Clicking on the picture clicks on the machine.

`/status` answers with the index page's own facts as JSON, for anything that
would rather read them than look at them.

Since one instance runs, the two pages answer without the instance in the path
as well: `http://192.168.1.40:8033/control` reaches the same page. So does a
command POSTed to the bare root — that is a client that already has the
address, and it has always worked.

The protocol is the pico-hid board's, so its clients work against Pob
unchanged. The full command grammar is in
**[Operation API](10_Operation%20API.md)**.


See also
--------

- [Operation API](10_Operation%20API.md) — the command grammar
- [Web UI](12_Web UI.md) — the pages this server hosts
- [Pob Keyboard](13_Keyboard.md) — a desktop client for the same API
- [Settings](06_Settings.md) — `server` and `server_port`

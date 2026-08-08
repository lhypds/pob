
Web UI
======

Three pages, served from the machine's own address, so a phone on the same
network can drive it and a spare screen can watch it with nothing installed on
either.

```
http://192.168.1.40:8033                    what is running here
http://192.168.1.40:8033/pb-a703/control    drive the machine
http://192.168.1.40:8033/pb-a703/view       watch it
```

Since one instance runs, the last two answer without the instance in the path
as well — `http://192.168.1.40:8033/control`.


Index
-----

The page for when all you remember is the machine. It shows what `pob status`
prints — instance, root, model, whether a session is executing, whether
recording is on, the MCP server, the port this one is on — and, at the top, the
machine's own address, with the control and view pages under it.

It rereads the status every few seconds, so it can be left open on a spare
screen as a summary of the instance.


Control
-------

The API's own client. Three controls:

- a **text field** — type a line, press ↵, and it is typed on the machine
- a **keyboard mirror** button — while it is on, keys pressed here go straight
  through, shortcuts included. On a phone the soft keyboard is mirrored
  instead, so autocorrect and swipe typing still work.
- a **trackpad** — drag to move the pointer, tap to click, two-finger tap to
  right-click, two fingers to scroll, double-tap-and-hold to drag


View
----

A picture of the machine in a frame, and one setting: how often to refetch it.
The frame comes from the instance root — the plain `GET` of the [Operation
API](10_Operation%20API.md) — so it shows the machine as the agent, the
trackpad, or a person at the keyboard leaves it, virtual cursor included.

The rate is in seconds, from 0.2 to 60, and starts at 1. It is remembered in
the browser, so a screen set to watch slowly stays that way across reloads.

The clock starts when a frame lands rather than when it was asked for, so a
slow capture never queues up requests, and a backgrounded tab stops asking
altogether — every frame costs the machine a screen capture.

Watching does not count as driving. A view left open all afternoon will not
keep the virtual cursor pinned on screen the way the trackpad does while it is
in use.


Where they come from
--------------------

All three are served by the [Pob Server](09_Server.md), so `"server": false`
takes them down with the rest of it. Each is one self-contained file —
`server/webui/index.html`, `control.html` and `view.html` — compiled into
`pob-core`.

The index and the view read from the server rather than being rendered by it:
`GET /status` for the facts, `GET /<instance-id>/` for the frame. Both are
plain enough to build another client on.


See also
--------

- [Pob Server](09_Server.md) — the address to open, and how to turn it off
- [Operation API](10_Operation%20API.md) — what the pages send and fetch
- [Pob Keyboard](13_Keyboard.md) — the same thing as a desktop app

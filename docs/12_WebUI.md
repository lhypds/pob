
Web UI
======

Two pages, served from the machine's own address, so a phone on the same
network can drive it and a spare screen can watch it with nothing installed on
either.

```
http://192.168.1.40:8033/pb-a703/control    drive the machine
http://192.168.1.40:8033/pb-a703/view       watch it
```

The bare root reaches them just as well — `http://192.168.1.40:8033/control`.


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

A picture of the machine and nothing else. It refetches the frame from the
instance root once a second — the plain `GET` of the [Operation
API](10_Operation%20API.md) — so it shows the machine as the agent, the
trackpad, or a person at the keyboard leaves it, virtual cursor included.

The refresh clock starts when a frame lands rather than when it was asked for,
so a slow capture never queues up requests, and a backgrounded tab stops asking
altogether — every frame costs the machine a screen capture.

Watching does not count as driving. A view left open all afternoon will not
keep the virtual cursor pinned on screen the way the trackpad does while it is
in use.


Where they come from
--------------------

Both pages are served by the [Pob Server](09_Server.md), so `"server": false`
takes them down with the rest of it. Each is one self-contained file —
`server/webui/webcontrol.html` and `server/webui/webview.html` — compiled into
`pob-core`.


See also
--------

- [Pob Server](09_Server.md) — the address to open, and how to turn it off
- [Operation API](10_Operation%20API.md) — what the pages send and fetch
- [Pob Keyboard](13_Keyboard.md) — the same thing as a desktop app

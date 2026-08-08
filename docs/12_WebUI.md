
Web UI
======

Open the server's address in a browser and it serves a remote control page —
the API's own client, so a phone on the same network can drive the machine with
nothing installed on it.

```
http://192.168.1.40:8033/pb-a703
```

The page is three controls:

- a **text field** — type a line, press ↵, and it is typed on the machine
- a **keyboard mirror** button — while it is on, keys pressed here go straight
  through, shortcuts included. On a phone the soft keyboard is mirrored
  instead, so autocorrect and swipe typing still work.
- a **trackpad** — drag to move the pointer, tap to click, two-finger tap to
  right-click, two fingers to scroll, double-tap-and-hold to drag

It is served by the [Pob Server](09_Server.md), so `"server": false` takes the
page down with the rest of it. The page itself is one self-contained file,
`server/webui/index.html`, compiled into `pob-core`.


See also
--------

- [Pob Server](09_Server.md) — the address to open, and how to turn it off
- [Operation API](10_Operation%20API.md) — what the page sends
- [Pob Keyboard](13_Keyboard.md) — the same thing as a desktop app

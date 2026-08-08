
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

The page for when all you remember is the machine. Three boxes:

- **Instance** — what `pob status` prints: the instance ID, root, model,
  whether a session is executing, whether recording is on, the MCP server, and
  the port this one is on.
- **Pages** — the control and view pages, as links to follow.
- **Endpoints** — what to call rather than visit. The machine's address appears
  twice, once per method, since a `GET` there is a picture of it and a `POST`
  is a command to it. One click selects a whole address to copy.

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

The machine in a picture you can work in. The frame comes from the instance
root — the plain `GET` of the [Operation API](10_Operation%20API.md) — so it
shows the machine as the agent, the trackpad, or a person at the keyboard
leaves it, virtual cursor included. The border sits on the picture's own edge,
so what you see is exactly what is there.

**Click straight on it.** Where you click is where it lands: the point is read
off the picture and sent as `MOVE_TO`, which is in screenshot pixels already,
so nothing has to be aimed. Click, double-click, right-click, drag and scroll
all work, the same gestures as the trackpad — minus the guessing, since here
you can see what you are pointing at.

Along the bottom: **fps**, how many frames a second to fetch, from 0.1 to 10,
starting at 1 and remembered in the browser — then the same text field,
send button and keyboard mirror as the control page. With the machine in front
of you, being able to type at it is the other half of the same thing.

The clock starts when a frame *lands* rather than when it was asked for, so a
slow capture never queues up requests, and a backgrounded tab stops asking
altogether — every frame costs the machine a screen capture.

Frames swap without a blink, which took some doing. Every frame is its own
element, put into the page on top of the one before it and dropped a paint
later; no element is ever written over.

The obvious way — two images, load into the hidden one, swap — blinks, and not
in the way you would expect: what shows for that instant is not a blank but an
*older* picture. Writing a new `src` into an element does not replace what it
is presenting. The element goes on showing what it had until the new picture is
decoded and composited, which is some time after `decode()` reports it ready;
reveal it in between and the frame from two ticks ago comes back for one paint.
Reused elements have a past. A fresh one does not — it enters the page already
holding a decoded picture, so the first paint it takes part in is the new
frame, and nothing older can surface because it never held anything older.

Watching does not count as driving: a view left open all afternoon will not
keep the virtual cursor pinned on screen the way the trackpad does while it is
in use. Clicking on it does, of course.


Where they come from
--------------------

All three are served by the [Pob Server](09_Server.md), so `"server": false`
takes them down with the rest of it. They live in `server/public/` —
`index.html`, `control.html`, `view.html` — compiled into `pob-core`.

The control and view pages share `pob.js`: the send queue, the text field and
the keyboard mirror. Both drive the machine the same way, and being the same
code is the only thing that keeps it that way. What is left in each page is
its own — the trackpad in one, the frame and the clicking on it in the other.

Neither page is rendered by the server; both read from it. `GET /status` for
the facts, `GET /<instance-id>/` for the frame — plain enough to build another
client on.


See also
--------

- [Pob Server](09_Server.md) — the address to open, and how to turn it off
- [Operation API](10_Operation%20API.md) — what the pages send and fetch
- [Pob Keyboard](13_Keyboard.md) — the same thing as a desktop app

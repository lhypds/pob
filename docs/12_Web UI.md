
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
root — the `GET` of the [Operation API](10_Operation%20API.md) — so it shows
the machine as the agent, the trackpad, or a person at the keyboard leaves it,
virtual cursor included. The border sits on the picture's own edge, so what
you see is exactly what is there.

It is watching the machine rather than reading it, so it does not ask for the
full-size PNG the agent works from. It asks for a JPEG no wider than the box
it is about to draw the frame in — on a typical window, a fifth of the bytes
and an eighth of the time, which is what a watchable frame rate is made of.

**Click straight on it.** Where you click is where it lands: the point is read
off the picture, scaled back up by however much the frame was shrunk on the
way over, and sent as `MOVE_TO`. Click, double-click, right-click, drag and
scroll all work, the same gestures as the trackpad — minus the guessing, since
here you can see what you are pointing at.

Along the bottom: **fps**, how many frames a second to fetch, from 0.1 to 30,
starting at 5 and remembered in the browser — then the same text field,
send button and keyboard mirror as the control page. With the machine in front
of you, being able to type at it is the other half of the same thing.

The clock starts when a frame *lands* rather than when it was asked for, so a
slow capture never queues up requests, and a backgrounded tab stops asking
altogether — every frame costs the machine a screen capture.

Frames arrive without a blink, which took some doing — and in the end took
giving up on images altogether. The frame is a canvas, drawn over and over.

Swapping `<img>` elements blinks, twice over. Two of them — load into the
hidden one, reveal it — blinks with an *older* picture: writing a new `src`
into an element does not replace what it is presenting, and the element goes
on showing what it had until the new picture is not merely decoded but
composited, which is some time after `decode()` reports it ready. Reveal it in
between and the frame from two ticks ago comes back for one paint. Reused
elements have a past.

A fresh element per frame has no past, and the opposite problem: a white
flash. It has to be put in and the old one taken out, and
`requestAnimationFrame` — the obvious place to take the old one out — runs
*before* the paint rather than after. So the paint that first shows the new
element is the same paint that loses the old one, and if the new one is not
composited yet, what is in the box for that frame is the page's own
background. Once a second that is a blip; twenty times a second it is a
strobe.

A canvas has no such moment. It presents whatever was last drawn into it and
goes on presenting exactly that until something else is drawn — there is no
swap to be caught halfway through, no element entering the page with nothing
to show, and nothing to take out afterwards.

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

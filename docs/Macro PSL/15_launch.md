launch
======

`launch(application)` opens an application on this machine and puts its window in the frame:

```
launch("Firefox")
sleep(2s)
click(398, 915)
```

Every other statement is a position inside the content area, and every one of those positions was
written down while some window sat in a particular place under the frame — put there, by hand, once.
That is fine for a macro played on the desktop it was recorded on, and it is the one thing a macro
started on a fresh machine, or by a schedule at four in the morning, cannot arrange for itself. This
arranges it: the application is opened and its window is placed exactly where the coordinates below
expect to find it, so the first statement after it lands where it was recorded.

The window is fitted to the content area — the box the [screenshots](../Pob/02_UI.md) are of and the
clicks are aimed through, not the whole Pob window — less a margin on every side, which is what
[`launch_gap`](#the-gap) is for. What the frame shows afterwards is that application, with a border
of whatever was behind it around the edge.

An application that is already running is brought forward and fitted rather than opened a second
time. A macro replayed twice finds one browser in the frame, not two.


The name
--------

What names an application is what this desktop calls one, so the argument is not the same thing on
all three:

| | Written | What it is |
|-|---------|------------|
| macOS | `launch("Firefox")` | An application's name, `launch("org.mozilla.firefox")` a bundle id, `launch("/Applications/Firefox.app")` a path. A name is looked for in `/Applications`, `/System/Applications` and `~/Applications`, two levels down, and matched whatever its case |
| Windows | `launch("notepad")` | An executable, resolved the way a Run box resolves one — off `PATH` and the App Paths registry — or `launch("C:\\Program Files\\Mozilla Firefox\\firefox.exe")`, a full path |
| Linux | `launch("firefox")` | The command that starts it, handed to `/bin/sh`, so a name on `PATH`, a full path and arguments all read as they would at a prompt |

A macro written to be replayed on more than one of them asks the same thing three ways under an
[`if`](07_if%20blocks.md), the way a [`run`](14_run.md) does.


The gap
-------

The window is not fitted edge to edge. [`launch_gap`](../Pob/06_Settings.md) — forty screenshot
pixels by default — is left around it on every side, and the reason is not the frame but the desktop
underneath it.

A window whose edges sit exactly on Pob's own edges is a window every modern window manager wants to
help you line up with. macOS's tiling, Windows' snap and half the Linux ones pull a dragged window
onto an edge that is already there, and Pob fitted perfectly over an application is Pob dragged
through its own magnetism: it comes to rest with three of its four edges on the other window's, and
every small nudge from there snaps straight back. A few points of daylight is what makes those stop
being the same edge.

The gap is in screenshot pixels — the space every coordinate in a macro is in — so it reads off a
screenshot as the number it was written as. What that is worth on the screen is the display's
business: forty is forty points on an ordinary display and twenty on a Retina one.

Set it to `0` for the exact fit, on a desktop that does not do this or where the frame is wanted
whole. Raise it where the snapping is still there — a gap under what the desktop reaches across does
not stop it, it gives it something nearer to snap to. On macOS the root of it can also be turned off
outright, in System Settings ▸ Desktop & Dock ▸ Windows ▸ **Drag windows to screen edges to tile**.

Every coordinate under the statement is measured from the frame rather than from the window, so the
gap is part of them: a macro recorded with one gap and replayed with another is a macro whose clicks
have all moved by the difference.


Waiting
-------

The statement waits for the window. An application already running is found at once; a cold start of
something large is seconds, and those seconds are the statement's rather than the next one's — which
is what lets the click under it be written after it.

Twenty seconds is as long as it waits. Past that the application is taken to have opened no window
at all: the step ends `failed`, the log says so, and the replay carries on to the next statement the
way it does past every statement it could not carry out. It is worth a `sleep()` under a launch even
so — a window that exists is not always a window that has finished drawing what is in it, and what
the statement waits for is the window.

Stop ends the wait the way it ends a `sleep`. The application is not called back — a request to open
something is not a thing that can be taken back once it has been made — so what a Stop halfway
through a launch leaves behind is an application opening, and possibly a window that lands in the
frame a moment after the run ended.

What it does not wait for is the application to be *ready*. A browser with its window up and its
session still loading is a window in the frame, and the thing to wait for after it is on the screen
rather than on the clock — which is what a [`once`](09_once%20blocks.md) or a slot in an
[`if`](07_if%20blocks.md) is for.


What the log says
-----------------

The statement, the application as this machine knows it, and what became of its window:

```
Macro launch("Firefox") opened Firefox (pid 4213) and fitted its window to the frame
Macro launch("Firefox") opened Firefox (pid 4213) and fitted its window to the frame — its window would not resize past 1024×768
Macro launch("Firefox") opened Firefox (pid 4213), but its window is not in the frame: Firefox put no window on screen within 20 seconds
```

A window that was placed but would not take the size it was asked for is still a step that ran: it
has the frame's top-left corner, which is what every coordinate under it is measured from, and the
part of the macro that reaches past where the window ends is the part that will not work. A window
that was not placed at all is a step that failed — nothing is wrong with the application, and what
is wrong is that the coordinates below are now aimed at a frame the window is not in.

An application that could not be opened at all — a name that is nothing on this machine, a command
the shell could not run — fails the step too, and the log says which of the two it was.


The lock, and what comes after
------------------------------

Fitting places the window once. Nothing holds it there: an application free to move its own window
can, and a person can drag it. What holds the arrangement afterwards is the
[lock](../Pob/02_UI.md) — the frame keeps its size, and every window under it travels with it — and
Execute turns the lock on for the length of a run by itself.

So the ordinary shape of a macro that opens what it drives is a `launch` at the top and nothing else
about the window anywhere:

```
launch("Firefox")
sleep(3s)
click(:: the x of the address bar ::, :: the y of the address bar ::)
typeText("example.com")
keyPress("return")
```


Permission
----------

Placing somebody else's window is the same permission as moving the mouse in it. On macOS that is
Accessibility, which Pob already needs and asks for at startup — a `launch` without it opens the
application and says in the log that it could not place the window. On Windows and Linux the desktop
grants it as a matter of course; a Linux window manager that refuses to place windows on request
refuses this too, and the log says the window would not move.


See also
--------

- [Calls](06_Calls.md) — every statement, and what a call does
- [run](14_run.md) — the other statement that reaches outside the window, and the shell it goes to
- [UI](../Pob/02_UI.md) — the content area a window is fitted to, and the lock that holds it there
- [Settings](../Pob/06_Settings.md) — `launch_gap`, the margin left around the window
- [When something is wrong](12_When%20something%20is%20wrong.md) — what the check refuses and what the replay logs
- [Logs](../Pob/05_Logs.md) — where the line about a launch is written

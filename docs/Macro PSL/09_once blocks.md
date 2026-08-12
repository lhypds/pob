
once blocks
===========

An [`if`](07_if%20blocks.md) asks its condition where the replay reaches it, and a
[`loop`](08_loop%20blocks.md) asks its own before each of a counted number of passes. Both are
questions about the screen at a moment the macro picked. A `once` is the other way round — the
screen picks the moment, and the macro is standing there when it does:

```
once (:: there is a new message ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply to it ::)
    keyPress("return")
}
```

The keyword is `once`, the parentheses hold a condition, and a `}` on a line of its own closes the
block — the same shape as the other two, and lowercase for the same reason, though `ONCE` and `Once`
are read too.

It does not end. A macro that reaches a `once` runs the statements above it, and then watches for as
long as the run lasts: [Stop](13_How%20it%20runs.md) ends it, and so does a [`stop()`](10_stop.md)
inside the block. That is what the name is about — the statements above run once, and what is below
the header is what the macro does from then on.


A memory of a single picture
----------------------------

What it watches with is one screenshot, the last one it took.

Every second — `once_interval` in [Settings](../Pob/06_Settings.md) — it takes another and compares
the two, then holds the new one in place of the old. Both that and the threshold below are read
again on every interval, so a watch that is firing too often or not often enough is retuned by
saving `settings.json` rather than by stopping the run. A screen nobody has touched compares equal, and
that is the whole cost of an interval: one capture, no model call, nothing else. When the two
pictures differ the condition is asked, over a screenshot of that moment, and the block runs if the
answer is `true`.

So the two halves do different work, and both are needed. The comparison is cheap and knows nothing:
it says that *something on this screen moved*. The condition is expensive and knows exactly what it
was asked: it says whether what moved is *the thing this macro is waiting for*. A `once` written
without the first would put a question to a model every second, all day, about a screen where
nothing had happened since the last time it asked.

A change is more than `once_change_percent` of the picture different — a tenth of a percent by
default, about a 45×45 patch of a 1920×1080 screen — with a pixel counting as different once one of
its channels has moved 8 steps in 255. Both numbers are there for the same reason: a screen is never
quite still. A clock's minute, a caret blinking in a text box, the last frame of a fade would each
be a change if one pixel were enough, and a `once` woken by those is a `once` asking about nothing.
What these blocks are written to notice is usually small — a row arriving in a list, a badge
appearing on an icon — so the default is low enough to see one of those and no lower.

Which number is right is a thing about the screen being watched rather than about Pob, so it is a
setting (see [Settings](../Pob/06_Settings.md)). Lower is more sensitive: `0.01` acts on a patch a
tenth the size of the default's, for a badge or a single character appearing. Higher is less: a
window redrawing a graph every second, or playing video in a corner, is a screen where a tenth of a
percent is noise, and `5` or `10` is what makes the block wait for something that actually matters.
It is a percentage and reads as one written either way, `0.01` or `"10%"`.

The block's own work is not a change. After it runs, the picture the `once` remembers is the screen
the block left behind, so the next change is the next thing that happens rather than the last thing
this macro did.


Asked again at every change
---------------------------

Like a [`loop`](08_loop%20blocks.md)'s pass, every ask puts the block back the way it was written —
the header and the statements under it — so the slots in them are filled from the screen as it is at
that moment. That is the point of the pair: `:: there is a new message ::` has to be able to answer
`false` at the next change as easily as it answered `true` at this one, and `:: the x offset to the
message box ::` is a different number once the window has scrolled.

Each of those is a psl run of its own and is kept as its own numbered directory under
`logs/<session>/slots/`, in the order they were filled (see [Logs](../Pob/05_Logs.md)). A watch that
ran all afternoon leaves one per change it acted on, which is the only place they can be read.

The condition is the same one an `if` takes, read the same way: a slot, or `true` / `false` written
out. `once (true)` runs the block at every change the comparison sees, without a model call between
the two — a macro that wants to act on any movement at all and does not need to be told what moved.
`once (false)` is not how a `once` is parked, though: the block never runs, and the watching does not
stop, so the run sits there until Stop. Comment it out instead.

A condition Pob could not read is not a `false`, and does not end anything either: a fill that
failed or an answer that is neither word leaves the block unrun and the `once` watching, the same as
a `false` would.


Where it goes
-------------

At the top level of a file, and nothing under it.

Both halves of that are the check's business, and both are about the same thing — a `once` never
gives the run back. Inside an [`if`](07_if%20blocks.md) it would be a block that runs to the end of
the run in place of the statements after it; inside a [`loop`](08_loop%20blocks.md) or another
`once` it would be a second pass that never comes; and a statement written under one is a statement
nothing ever reaches. Pob says so before the run rather than leaving it to be discovered:

```
line 6: once watches the screen until the run is stopped and is written at the top
level of a file, not inside another block — so its whole block is dropped
line 9: nothing here runs — the once block opened on line 5 watches the screen
until the run is stopped, so the statements under it are never reached
```

Inside the block is ordinary PSL, including an `if`, a `loop` or a [`call`](11_call.md), nested as
deep as the macro needs. An [`else`](07_if%20blocks.md) belongs to an `if` and never to a `once`:
the condition is asked again at the next change rather than answered once, so there is no moment an
`else` would be the thing that runs instead.

A file a [`call`](11_call.md) names may hold one, and then the call is the last statement that
happens: the called file watches, and the statements under the call are never reached. The check
does not follow a `call` that far, so that one is worth remembering rather than being told.


What the log says
-----------------

`>> ONCE START` opens the watch, with the interval it is using. After that, `ONCE CHANGE` for each
change the comparison saw and how much of the picture it was, the condition's own `> STEP START` /
`STEP END` rows for the ask that followed, and `ONCE RUN` when the answer was `true` and the block
ran. `ONCE STOP` closes it with how many changes were seen and how many times the block ran. The
intervals where nothing happened say nothing at all — a watch that sat quiet all afternoon leaves an
empty log, which is the right amount to say about it.


See also
--------

- [loop blocks](08_loop%20blocks.md) — the same restoring, over a counted number of passes
- [if blocks](07_if%20blocks.md) — the same condition, judged once where the replay reaches it
- [stop](10_stop.md) — how a `once` ends itself
- [Settings](../Pob/06_Settings.md) — `once_interval` and `once_change_percent`, the pause between pictures and how much of one has to move
- [Logs](../Pob/05_Logs.md) — one numbered slot directory per change acted on

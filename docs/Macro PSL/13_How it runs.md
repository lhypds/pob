
How it runs
===========

The cursor starts at the origin — a replay resets it there first — and every `move` and `drag` is
relative to where it already is. That is why `resetCursor()` is recorded when something sent the
cursor home mid-sequence: skip the jump back and every move after it starts from the wrong place.
The absolute calls — `moveTo`, `dragTo`, and a click written with a target — are measured from the
corner of the screenshot instead, so where the cursor got to does not reach them.

All coordinates are screenshot pixels, origin top-left, x right, y down. The cursor is held inside
the Pob window: a move that would take it past an edge stops at the edge, since everything the
macro addresses — what the screenshots show, what the clicks go through — is inside that window.
That holds for an absolute position too: `moveTo` past an edge stops at the edge, and the log says
where the cursor actually ended up.

Between one call and the next Pob waits `macro_default_delay` milliseconds, one second by default
(see [Settings](../Pob/06_Settings.md)). A UI that needs longer gets an explicit `sleep()`. Judging an `if`
adds no delay of its own — the model call is the wait — and neither does going round a `loop`, or
stepping into a `call`: the gap between one pass or one file and the next is the delay after the
last statement before it, as it would be anywhere else.

A [`once`](09_once%20blocks.md) has a wait of its own, `once_interval`, which is the pause between
the screenshots it compares rather than a delay between statements — the statements inside it are
spaced by `macro_default_delay` like everything else.

Stop halts the run between statements, and during a `sleep()` rather than after it. A run that
reaches the end fires `stop_hook`, if one is set; a stopped run does not. A run that reached a
`stop()` statement did reach its end, and fires it. A run watching inside a `once` has no end of its
own to reach: Stop is what ends it, so `stop_hook` fires only where the block itself reached a
`stop()`.


See also
--------

- [stop](10_stop.md) — the Stop button reached from inside the macro
- [once blocks](09_once%20blocks.md) — the run that waits for the screen instead of ending
- [Settings](../Pob/06_Settings.md) — `macro_default_delay`, `once_interval` and `stop_hook`
- [Calls](06_Calls.md) — `resetCursor()`, `sleep()` and the rest of the vocabulary


stop
====

`stop()` ends the run where it stands:

```
if (:: a password prompt is on screen ::) {
    stop()
}
typeText("the next thing")
```

Nothing under it runs — not the statements after it, not the rest of the block it is in, not the
passes a `loop` around it had left, not the watching a [`once`](09_once%20blocks.md) around it would
otherwise go on doing, and not the file that called the file it is written in. It is
the Stop button reached from inside the macro, and it takes effect between one statement and the
next in exactly the same way. There is no delay after it: `macro_default_delay` is the gap before
the statement that would have been next, and there is no such statement.

The parentheses hold nothing and are written all the same: a statement is `name(…)` everywhere in
the language, and `stop` on its own is not one — it is a line the check refuses and names the fix
for. The name is lowercase like every other name and read that way, so `STOP()` is refused as well.

A run that reaches a `stop()` has finished, as against a run someone stopped: the session is written
out and `stop_hook` fires, the same as a run that fell off the last line. The Stop button is the one
that does neither.

Where this earns its place is beside an [`if`](07_if%20blocks.md). A macro that has noticed something
it was not written for — a login screen, an error dialog, a list that came back empty — has nothing
useful to do with the forty statements below, and `stop()` is how it says so.

It is also the only end a [`once`](09_once%20blocks.md) has of its own. That block watches the
screen until something stops the run, so a watch meant to finish on its own — after the file it was
waiting for arrives, when the last message has been answered — says so with a `stop()` under an
`if` inside it.


See also
--------

- [if blocks](07_if%20blocks.md) — the condition a `stop()` usually sits under
- [once blocks](09_once%20blocks.md) — the block a `stop()` is the only end of
- [How it runs](13_How%20it%20runs.md) — Stop, `stop_hook`, and the delay between statements
- [call](11_call.md) — the file a `stop()` also ends

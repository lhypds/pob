
stop
====

`stop` ends the run where it stands:

```
if (:: a password prompt is on screen ::) {
    stop
}
typeText("the next thing")
```

Nothing under it runs — not the statements after it, not the rest of the block it is in, not the
passes a `loop` around it had left, and not the file that called the file it is written in. It is
the Stop button reached from inside the macro, and it takes effect between one statement and the
next in exactly the same way. There is no delay after it: `macro_default_delay` is the gap before
the statement that would have been next, and there is no such statement.

It is the one statement written without parentheses, because the parentheses would hold nothing.
`stop()` is read as well, for anyone who would rather write every statement the same shape. The name
is lowercase like every other name and read that way — `STOP` is a line that cannot be read, and is
logged and skipped like any other.

A run that reaches a `stop` has finished, as against a run someone stopped: the session is written
out and `stop_hook` fires, the same as a run that fell off the last line. The Stop button is the one
that does neither.

Where this earns its place is beside an [`if`](07_if%20blocks.md). A macro that has noticed something
it was not written for — a login screen, an error dialog, a list that came back empty — has nothing
useful to do with the forty statements below, and `stop` is how it says so.


See also
--------

- [if blocks](07_if%20blocks.md) — the condition a `stop` usually sits under
- [How it runs](12_How%20it%20runs.md) — Stop, `stop_hook`, and the delay between statements
- [call](10_call.md) — the file a `stop` also ends


loop blocks
===========

An [`if`](07_if%20blocks.md) runs a block once or not at all. A `loop` runs it again and again:

```
loop (3) {
    keyPress("down")
    click()
}
```

The keyword is `loop`, the parentheses hold a count, and a `}` on a line of its own closes the
block — the same shape as an `if`, and lowercase for the same reason, though `LOOP` and `Loop` are
read too. That block runs three times.

The count is written out as a whole number rather than asked. It is the bound on a loop that could
otherwise not end, and a bound the model picks fresh on every pass is not a bound.

A loop that should stop when the screen says so takes a condition in front of the count:

```
loop (:: the window is still open ::, 5) {
    move(:: the close button ::)
    click()
}
```

The condition is checked before every pass, the first one included, and the loop ends the moment it
does not hold. The count is the most passes it may make — five here — so that block runs until the
window is closed, or five times, whichever comes first. It is the condition an [`if`](07_if%20blocks.md)
takes, read the same way: a slot, or `true` / `false` written out. A comma inside a slot is part of
the instruction, so `loop (:: still loading, not empty ::, 4)` asks what it looks like it asks; the
count is what follows the last comma the header has of its own.

A condition written out asks nothing and costs nothing, the same as an `if`'s, which makes the two
ends of the language meet: `loop (3)` and `loop (true, 3)` are one and the same loop — a loop
written without a condition is a loop whose condition always holds, and the count is what ends
either of them. `loop (false, 3)` is the other end of that, and is how a loop is parked without
deleting it: the check fails before the first pass and the body never runs.

Inside is ordinary PSL, including another `loop` or an `if`, nested as deep as the macro needs. An
[`else`](07_if%20blocks.md) belongs to an `if` and never to a `loop`: what a loop does when its
condition stops holding is end, and the statements under the `}` are what happens instead.

Asked again on every pass

Every pass puts the loop's statements back the way they were written, so the slots in them are
asked again from the screen as it is at that moment. That is the whole point of the pair: `:: the x
offset to the Close button ::` is a different number once the first dialog is gone, and a condition
that could only be answered once would never turn false.

Each of those is a psl run of its own and is kept as its own numbered directory under
`logs/<session>/slots/`, so five passes over one slot leave five of them, in the order they were
filled (see [Logs](../Pob/05_Logs.md)). That is the only place a loop's fills can be read: a slot asked
once per pass has an answer per pass, and no single copy of the macro could hold more than the last of
them, which is why the session keeps none.

The model is shown the macro and a screenshot and nothing else, so it does not know which pass it is
being asked about — a slot that would have to count them is one it cannot answer. Write instructions
about what is on the screen now: "the window is still open", "there is another unread message", "the
list is still loading".

A loop is one statement in the macro however many passes it makes, and Execute's count of what it is
about to run says so. What the log says is the passes: one line as each begins, and one for the
verdict that ended it.


See also
--------

- [if blocks](07_if%20blocks.md) — the same condition, judged once
- [AI slot](05_AI%20slot.md) — how a slot goes back into the file before a pass
- [Logs](../Pob/05_Logs.md) — one numbered slot directory per pass


if blocks
=========

A macro plays the same actions every time, which is the point of one — until whole parts of it
should not always happen. An `if` guards a block with a condition: it runs when the condition holds
and is skipped when it does not.

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
    sleep(500)
}
```

The keyword is `if`, the condition is the parenthesised expression between it and the `{` that ends
the line, and a `}` on a line of its own closes the block. Inside is ordinary PSL — including
another `if` or a `loop`, nested as deep as the macro needs. Lines after the `}` run either way;
there is no `else`.

The condition is either an [AI slot](05_AI%20slot.md), which is the usual way and the one above, or
`true` / `false` written out — which asks nothing and costs nothing, and is how a block is parked
without deleting it. Anything else in the parentheses is not a condition Pob can read, and the block
is dropped rather than run unguarded. A filled-in condition is read the same way, case and quotes
allowed: a model that answered `"True"` has answered true.

Write the keyword lowercase. `IF` is read too, and so is `If`: a block Pob failed to recognise would
run its body unguarded, which is the one thing the condition was written to prevent.

A slot inside a block that gets skipped is never reached, so it is never filled and costs nothing.
The check for psl runs the other way round — over the whole macro, before the first statement: with
a slot anywhere in it and no psl to be found, Execute puts up **psl needed** and the macro does not
run at all, before the cursor has moved. Finding out halfway through would leave everything above
the slot already played.

Recording never writes an `if`, or any other slot. They are the part you write by hand, into a macro
that is otherwise recorded.


See also
--------

- [loop blocks](08_loop%20blocks.md) — the same condition, checked before every pass
- [stop](09_stop.md) — what an `if` that noticed something usually does
- [AI slot](05_AI%20slot.md) — what a condition is filled from

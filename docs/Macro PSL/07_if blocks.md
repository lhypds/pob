
if blocks
=========

A macro plays the same actions every time, which is the point of one — until whole parts of it
should not always happen. An `if` guards a block with a condition: it runs when the condition holds
and is skipped when it does not.

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
    sleep(500ms)
}
```

The keyword is `if`, the condition is the parenthesised expression between it and the `{` that ends
the line, and a `}` on a line of its own closes the block. Inside is ordinary PSL — including
another `if` or a `loop`, nested as deep as the macro needs. Lines after the `}` run either way.

The condition is either an [AI slot](05_AI%20slot.md), which is the usual way and the one above, or
`true` / `false` written out — which asks nothing and costs nothing, and is how a block is parked
without deleting it. Anything else in the parentheses is not a condition Pob can read, and the block
is dropped rather than run unguarded — the `else` under it, if it has one, dropped with it.
A filled-in condition is read the same way, case and quotes allowed: a model that answered `"True"`
has answered true.

Write the keyword lowercase. `IF` is read too, and so is `If`: a block Pob failed to recognise would
run its body unguarded, which is the one thing the condition was written to prevent.

A slot inside a block that gets skipped is never reached, so it is never filled and costs nothing.
The check for psl runs the other way round — over the whole macro, before the first statement: with
a slot anywhere in it and no psl to be found, Execute puts up **psl needed** and the macro does not
run at all, before the cursor has moved. Finding out halfway through would leave everything above
the slot already played.

Recording never writes an `if`, or any other slot. They are the part you write by hand, into a macro
that is otherwise recorded.


else
----

What to do instead goes under an `else`:

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
} else {
    keyPress("escape")
}
```

One of the two blocks runs and the other does not, and the statements under the whole thing run
whichever it was. An `else` belongs to the `if` whose block the `}` in front of it closes — the
nearest one still open — and takes no condition of its own: the condition was asked above it, and
the `else` is the other half of that one answer.

The `}` and the `else` are one statement written on one line or on two. `} else {` is the shape C
put in everyone's hands, and

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
}
else {
    keyPress("escape")
}
```

is the same thing written under the rule the rest of the braces keep, that a `}` closes a block on a
line of its own. Pob reads both, and nothing but blank lines goes between the two of them. `ELSE`
and `Else` are read as well, for the reason `IF` is.

A condition Pob could not read is not a `false`. A fill that failed, an answer that is neither
word — none of those is a verdict, and an `if` written with an `else` runs **neither** block rather
than taking the non-answer as a no. The log says `skipping both blocks`, and the macro goes on at
the statement under them.


else if
-------

An `else` that goes on asking is an `else if`, and the chain is read from the top until a condition
holds:

```
if (:: a save dialog is on screen ::) {
    keyPress("return")
} else if (:: an error dialog is on screen ::) {
    keyPress("escape")
} else if (:: the window is still loading ::) {
    sleep(2s)
} else {
    stop()
}
```

The first condition that holds is the only block that runs, and the conditions under it are never
asked — each one is a model call, and a chain that stopped at the second has made two of them. One
`}` closes the whole chain: an `else if` is the `if` written inside the `else` of the one above it,
which is what the same macro nested longhand says, and Pob keeps it as exactly that.


See also
--------

- [loop blocks](08_loop%20blocks.md) — the same condition, checked before every pass
- [stop](09_stop.md) — what an `if` that noticed something usually does
- [AI slot](05_AI%20slot.md) — what a condition is filled from

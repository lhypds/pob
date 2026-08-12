
Comments
========

Comments are C's, because a macro is read by people who have read code before. `//` runs to the end
of the line, and `/* … */` runs until it is closed, however many lines that takes:

```
// Reply to whatever is waiting in the chat window.
move(398, 915)   // the message box
click()

/* Left out until the new dialog settles down:
   typeText("Hi")
   keyPress("return")
*/
drag(-775, -615)
```

A comment is taken out of the line before the line is read, so a line that is only a comment is a
line with no statement on it, and a comment on the end of a statement does not stop it being one.
A `/*` that nothing closes runs to the end of the file. They do not nest — the first `*/` closes the
comment it is in, and anything after that is code again.

Nothing is taken out of the *file*. Every statement is found by its line number — that is how an
answer goes back where it came from, and how a `loop` puts its statements back before a pass — so
commenting a line out costs it its meaning and never its place. A macro of forty lines stays forty
lines however much of it is commented out, and the line numbers in the log go on naming the lines
you would count to in an editor.

The text stays for one more reason. psl is handed the macro whole, so a comment is something the
model filling a slot two lines down can read, and what a comment says about the screen is often the
thing it has no other way of knowing.

A comment marker is only a marker where it is not something the statement was already saying. In a
double-quoted string it is text — `typeText("http://example.com")` types the URL — and inside a
`:: … ::` it is part of the instruction, since an instruction is a sentence and may well have a `//`
in it.

What a comment cannot be is a question. `::` markers inside one are written out of the copy that
goes to psl, the same `<instruction>` a slot gets when the replay is finished with it, so a
commented-out `move(:: the x offset ::, 0)` asks nothing and costs nothing. That includes a lone
marker with nothing to close it: `// TODO :: put the offset back` holds no slot of its own, but psl
does not read a file one line at a time, and the next real `::` below would close it — what got
filled would be the span from the middle of a comment down into a live statement. So every marker in
a comment goes, paired or not.

Recording never writes a comment. They are the part you write by hand, into a macro that is
otherwise recorded — which is most of what they are for: saying why a `move` goes where it goes,
before the recording stops being readable.


See also
--------

- [AI slot](05_AI%20slot.md) — the `:: … ::` a comment is written out of
- [When something is wrong](12_When%20something%20is%20wrong.md) — a `/*` nothing closes, and a `*/` with no `/*` above it

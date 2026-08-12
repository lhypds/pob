
Structure
=========

One statement per line, run top to bottom. Blank lines are nothing at all, and so is a line that is
only a comment. There is no line continuation: a line is a whole statement or it is not one.

A line that cannot be read is found before the run rather than during it. Pob checks the whole
macro — and every file it `call`s — when Execute is pressed, and puts up **Macro problems** with
the line numbers instead of moving the cursor. A macro is often half-recorded and half-typed, and
what a bad line in the middle of one costs is not the thirty-nine statements around it but the
half of them that would have played before anyone noticed. See
[When something is wrong](12_When%20something%20is%20wrong.md), and `pob macro --check` to read a
macro without running it.

There are four kinds of statement: a **[call](06_Calls.md)**, which does something to the machine or
to the run, an **[if block](07_if%20blocks.md)**, which guards the statements inside it with a
condition and holds the `else` that runs when the condition does not hold, a
**[loop block](08_loop%20blocks.md)**, which runs the statements inside it again and again, and a
**[once block](09_once%20blocks.md)**, which watches the screen for as long as the run lasts and
runs the statements inside it each time it changes into something the condition holds of. Any of them can hold an **[AI slot](05_AI%20slot.md)** — a piece of a statement that is a
prompt rather than a value, filled in as the replay reaches it.

A slot can also stand where a whole statement goes, written on a line of its own. That one is
answered with the statements themselves — one of them or several, blocks and all — and they are
replayed where the line stands. It is the one line of a macro that is not a statement as written and
is a statement by the time it runs.

A value is a number (`398`, `0.5`), a string (`"Hello"`) or a time (`250ms`, `3s`, `10h5m`). See
[Calls](06_Calls.md) for how each is written and which arguments take which.


See also
--------

- [Comments](04_Comments.md) — `//` and `/* … */`
- [Calls](06_Calls.md) — every statement and its arguments
- [How it runs](13_How%20it%20runs.md) — the origin, and the delay between statements

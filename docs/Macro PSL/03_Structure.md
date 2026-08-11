
Structure
=========

One statement per line, run top to bottom. Blank lines are nothing at all, and so is a line that is
only a comment. There is no line continuation: a line is a whole statement or it is not one.

A line that cannot be read is found before the run rather than during it. Pob checks the whole
macro — and every file it `call`s — when Execute is pressed, and puts up **Macro problems** with
the line numbers instead of moving the cursor. A macro is often half-recorded and half-typed, and
what a bad line in the middle of one costs is not the thirty-nine statements around it but the
half of them that would have played before anyone noticed. See
[When something is wrong](11_When%20something%20is%20wrong.md), and `pob macro --check` to read a
macro without running it.

There are three kinds of statement: a **[call](06_Calls.md)**, which does something to the machine or
to the run, an **[if block](07_if%20blocks.md)**, which guards the statements inside it with a
condition, and a **[loop block](08_loop%20blocks.md)**, which runs the statements inside it again and
again. Any of them can hold an **[AI slot](05_AI%20slot.md)** — a piece of a statement that is a
prompt rather than a value, filled in as the replay reaches it.


See also
--------

- [Comments](04_Comments.md) — `//` and `/* … */`
- [Calls](06_Calls.md) — every statement and its arguments
- [How it runs](12_How%20it%20runs.md) — the origin, and the delay between statements

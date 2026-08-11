
When something is wrong
=======================

A macro is read twice: once before it runs, and once as it runs.

The first reading is the check. It goes over the whole macro and every file it `call`s before the
cursor moves, and what it finds stops the run: **Macro problems** goes up instead, listing each
line and what to fix on it. Nothing is played until it comes back with nothing.

That reading is the strict one because the quiet mistakes are the expensive ones. `move(1)` is a
call whose numbers cannot be read, so the cursor stays where it was — and the `click()` under it
then lands wherever the statement before happened to leave it. A run that plays thirty-nine of
forty statements and mentions the fortieth in a log nobody has open is worse than a run that does
not start.

| Written | What the check says |
|---------|--------------------|
| A line that is not a call — no parentheses, nothing after the `)` | `"halt" is not a statement — a call is name(argument, argument)` |
| A name that is not one of the statements in [Calls](06_Calls.md) | `there is no statement called "clik"`, and which one it was nearly, since names are case-sensitive |
| A call written with the wrong number of arguments — `move(1)`, `click(1, 2)`, `typeText("a", "b")`, `call()` | `move takes 2 arguments, and 1 was written`. Counted before the fill, so a slot standing for a whole argument list is counted as the list it may come back as — `move(:: the profile icon ::)` is not one of these, and `move(:: … ::, 40, 60)` is |
| A call whose numbers are not numbers — `scroll(a, b)` | `scroll wants numbers, and its first argument is "a"` |
| An `if` missing its parentheses or the `{` at the end of the line, or holding neither a slot nor `true`/`false` | The header, and that its whole block is dropped |
| A `loop` missing its parentheses or the `{`, or whose count is not a whole number of 1 or more — `loop (:: how many ::)`, `loop (2.5)`, `loop (0)` | The header, and that its whole block is dropped |
| A `loop` whose condition is neither a slot nor `true`/`false` | The same |
| An `if` or `loop` whose `}` is missing | The line the block was opened on, and that the end of the file closes it |
| A `}` with no `if` or `loop` above it | `} closes a block that was never opened` |
| A `/*` nothing closes | The line it opened on, and that the comment runs to the end of the file |
| A `*/` with no `/*` above it | Not a comment: it stays in the line, which is then not a statement |
| A statement and a comment that closes mid-line, leaving two statements on it — `click() /* why */ move(1, 2)` | One line, which is not one statement — read as a call with arguments nobody wrote |
| `stop` mis-spelled or mis-cased — `STOP`, `halt` | `"STOP" is not a statement — stop is spelled lowercase` |
| A `call` naming a file that is not there, or cannot be read | The path it worked out, and that there is no such file |
| A `call` reaching a file that is already running, directly or round a ring | That it is a replay with no end in it |
| A `call` nine files deep | How deep it is, and that eight is as far as `call` goes |
| A slot in a macro, or in a file it calls, with psl not installed | The one thing checked separately: **psl needed** goes up instead, since it is fixed by installing something rather than by editing the file |

Everything in a called file is checked too, and named by the file it is in: `sign-in.psl line 4`.
A statement inside a block the check is dropping is checked all the same — it will still be there
once the header is fixed, and a macro is worth fixing in one pass rather than as many as it has
mistakes.

The second reading is the replay, and it is the forgiving one — it has to be, because what is left
is what could not be known before the fill. A statement that goes wrong here is logged and skipped
and the ones around it still run:

| Written | What happens |
|---------|--------------|
| A statement that does not read as PSL once its slots are filled | Logged with what it was filled to, and skipped |
| A slot psl cannot fill — no model configured, no answer, no screenshot | The statement is skipped, with the reason in the log; a condition then reads as false, so its block is skipped |
| An `if` whose condition fills to something other than `true` or `false` | Reads as false: the block is skipped, with what it filled to in the log |
| A `loop` whose condition fills to something other than `true` or `false` | Reads as false: the loop ends there, with what it filled to in the log |
| A `call` whose path is itself a slot, filled to a file that is not there | Logged and skipped; the statements around it still run |

Two things are not mistakes at all, and neither reading has anything to say about them:

| Written | What it means |
|---------|---------------|
| Markers touching a letter or digit outside them — `std::cout` | Not a slot; it stays in the statement as written |
| A `:: … ::` in a comment | Not a question: written out as `<instruction>` before the file goes to psl, and never filled |

Checking it without running it

```
pob macro --check
```

is the same reading Execute does, said in the terminal: it reads `macro.psl` and the files it calls,
prints what is wrong with them and runs nothing. It talks to no one, so it is the one macro command
that works with Pob closed — which is the point of it, since a hand-edited macro is worth checking
before it is ever loaded. It exits `1` when there is anything to fix, so a commit hook can hold the
line.

Nothing else checks. Opening the macro with the Macro PSL button (🪄) opens it and does no more,
and a macro is read when it is about to be played or when you ask.


See also
--------

- [Calls](06_Calls.md) — the names and argument counts the check reads against
- [CLI](../Pob/07_CLI.md) — `pob macro --check` and the rest of the command
- [Logs](../Pob/05_Logs.md) — where a skipped statement is written down


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
| A call written with the wrong number of arguments — `move(1)`, `click(398)`, `typeText("a", "b")`, `call()` | `move takes 2 arguments, and 1 was written`, and for the calls written with a target or with none, `click takes both arguments or none at all, and 1 was written`. Counted before the fill, so a slot standing for a whole argument list is counted as the list it may come back as — `move(:: the profile icon ::)` is not one of these, and `move(:: … ::, 40, 60)` is |
| A call whose numbers are not numbers — `scroll(a, b)` | `scroll wants numbers, and its first argument is "a"` |
| A `sleep` whose argument is not a time — `sleep(500)`, `sleep(soon)`, `sleep(-3s)` | `sleep was written with "500", which is not a time — a number with its unit on the end: 250ms, 3s, 10m, 5h, 10h5m` |
| A time written in quotes — `sleep("10m")` | `sleep was written with "10m" — a time is not a string, so it goes in without the quotes: 10m` |
| An `if` missing its parentheses or the `{` at the end of the line, or holding neither a slot nor `true`/`false` | The header, and that its whole block is dropped |
| A `loop` missing its parentheses or the `{`, or whose count is not a whole number of 1 or more — `loop (:: how many ::)`, `loop (2.5)`, `loop (0)` | The header, and that its whole block is dropped |
| A `loop` whose condition is neither a slot nor `true`/`false` | The same |
| A `once` missing its parentheses or the `{`, or holding neither a slot nor `true`/`false` — `once {`, `once () {` | The header, and that its whole block is dropped. A `once` is never written without a condition |
| A `once` inside an `if`, a `loop` or another `once` | `once watches the screen until the run is stopped and is written at the top level of a file, not inside another block`, and that its whole block is dropped |
| Anything written under a `once` — another `once`, a statement, a block | `nothing here runs — the once block opened on line 5 watches the screen until the run is stopped`. Said at the first of them, since a tail of unreachable statements is one mistake |
| An `else` on a `once` | That an `else` belongs to an `if`, and that its whole block is dropped |
| An `if`, `loop` or `once` whose `}` is missing | The line the block was opened on, and that the end of the file closes it |
| A `}` with no block above it | `} closes a block that was never opened` |
| An `else` with no `if` above it, or one whose `}` is missing | `else belongs to the if whose block the } above it closes — } else {, or the else on a line of its own under the }`, and that its whole block is dropped |
| An `else` on a `loop` | That an `else` belongs to an `if`, and that its whole block is dropped |
| A second `else` on one `if` | `the if above this one already has an else`, and that its whole block is dropped |
| An `else` written with anything but a `{` after it — `} else (:: is it? ::) {` | That an `else` takes no condition of its own, and that its whole block is dropped |
| An `else if` missing its parentheses or the `{`, or holding neither a slot nor `true`/`false` | The header, and that its whole block is dropped — the same as the `if` it is |
| A `/*` nothing closes | The line it opened on, and that the comment runs to the end of the file |
| A `*/` with no `/*` above it | Not a comment: it stays in the line, which is then not a statement |
| A statement and a comment that closes mid-line, leaving two statements on it — `click() /* why */ move(1, 2)` | One line, which is not one statement — read as a call with arguments nobody wrote |
| `stop` written without its parentheses | `stop is written stop(), with the parentheses every other statement has` |
| `stop` mis-spelled or mis-cased — `STOP`, `halt` | `"STOP" is not a statement — stop is written stop(), lowercase and with parentheses` |
| A `call` naming a file that is not there, or cannot be read | The path it worked out, and that there is no such file |
| A `call` reaching a file that is already running, directly or round a ring | That it is a replay with no end in it |
| A `call` nine files deep | How deep it is, and that eight is as far as `call` goes |
| A line that is neither kind of slot — two slots on one line, or a slot with a word beside it | `a slot stands for statements when it is the whole line and for a value when it is written inside one, and this is neither`. A slot on a line of its own is not one of these: it is a [statement slot](05_AI%20slot.md), and what it fills to is read when it arrives |
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
| A `sleep` whose slot fills to something that is not a time — `500`, `"10m"` | Logged and skipped, in the same words the check would have used. The check never saw this line, so the replay says it instead |
| A slot psl cannot fill — no model configured, no answer, no screenshot | The statement is skipped, with the reason in the log; a condition then has no verdict, so its block is skipped — and an `if` written with an `else` skips that too |
| An `if` whose condition fills to something other than `true` or `false` | No verdict: the block is skipped, with what it filled to in the log. An `if` written with an `else` runs neither block — an `else` is what runs when the answer is false, and there is no answer |
| A `loop` whose condition fills to something other than `true` or `false` | Reads as false: the loop ends there, with what it filled to in the log |
| A `once` whose condition fills to something other than `true` or `false` | No verdict: the block does not run, and the `once` goes back to watching. Nothing ends it but Stop |
| A `once` whose screenshot cannot be taken, or cannot be read | Logged, and the watch carries on: a picture that cannot be compared is taken as a change, since the cost of asking about a still screen is one model call and the cost the other way is a watch that never notices anything |
| A `call` whose path is itself a slot, filled to a file that is not there | Logged and skipped; the statements around it still run |
| A statement in a generated block that does not read as PSL | Logged and skipped, named by the block it is in — `macro-line2.psl line 3` — and the statements around it still run |
| A statement slot that fills to nothing that reads as a statement | Logged with what it filled to, and nothing is replayed for that line |
| A statement slot nine files deep, counting the `call`s above it | How deep it is, and that it is not filled at all — a block this replay would not run is not worth a model call |

Two things are not mistakes at all, and neither reading has anything to say about them:

| Written | What it means |
|---------|---------------|
| Markers touching a letter or digit outside them — `std::cout` | Not a slot; it stays in the statement as written |
| A `:: … ::` in a comment | Not a question: written out as `<instruction>` before the file goes to psl, and never filled |

Checking it without running it

```
pob macro --check
```

is the same reading Execute does, said in the terminal: it reads `src/main.macro.psl` and the files it calls,
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


Prompt Script Language
======================

Prompt Script Language — PSL — is the language a macro is written in. A `macro.txt` is a program in
it, and so is every line the record button appends while you work: the language is small enough that
a recording is readable, and readable enough that the recording is worth editing afterwards.

The name is what sets it apart from a scripting language that only ever does what it is told. A
statement can hold a prompt instead of a value — `::…::`, an AI slot — and what the AI answers is
what the line then says. A macro written in PSL is part instruction and part question.

```
move(398, 915)
click()
drag(-775, -615)
if (::the window focus on a wechat user::) {
    move(128, 738)
    click()
}
typeText("done")
keyPress("return")
```

Three things write it and one thing runs it. Recording writes it from what you, the AI session and
the [MCP](08_MCP.md) clients do; you write it by hand in an editor; the AI writes it as it works.
Play, `pob macro` and the [Control API](11_Control%20API.md) all run it the same way — see
[Macro](03_Macro.md) for the buttons and the file, this page for the language.


Structure
---------

One statement per line, run top to bottom. Blank lines are nothing at all. There is no comment
syntax and no line continuation: a line is a whole statement or it is not one.

A line that cannot be read does not stop the run — it is written to the log and skipped, and the
statements around it stand. A macro is often half-recorded and half-typed, and one bad line in the
middle of it is a line to fix, not a reason to refuse the other forty.

There are two kinds of statement: a **call**, which does something to the machine, and an **if
block**, which asks the AI whether to run the statements inside it.

`::` … `::` is an **AI slot**: a prompt that stands where a value would, and is replaced by what the
AI answers when the line is reached. Everything outside the markers is written down and means
exactly what it says. In the condition of an `if`, the answer that replaces the slot is true or
false.


Calls
-----

A call is `name(argument, argument)` — the name, then arguments in parentheses, and nothing after
the closing one. Names are case-sensitive, spelled as below.

| Statement | Arguments | What it does |
|-----------|-----------|--------------|
| `move(dx, dy)` | numbers | Nudge the cursor by a relative pixel offset. Positive `dx` right, positive `dy` down |
| `click()` | — | Left-click at the cursor |
| `rightClick()` | — | Right-click at the cursor |
| `doubleClick()` | — | Double-click at the cursor |
| `drag(dx, dy)` | numbers | Drag from the cursor by `(dx, dy)`. The cursor ends at the new position |
| `scroll(dx, dy)` | numbers | Scroll at the cursor. `dy > 0` down, `dy < 0` up, `dx > 0` right |
| `typeText(text)` | one string | Type text at the current keyboard focus |
| `keyPress(key)` | one string | Press a key, with `+`-joined modifiers in front of it — `return`, `cmd+v`, `ctrl+shift+t`. See [Key names](04_Keys.md) |
| `sleep(milliseconds)` | number | Pause |
| `resetCursor()` | — | Send the cursor back to the origin it starts at |
| `take_screenshot(x?, y?, w?, h?)` | numbers, all four or none | Capture a screenshot into the session's `screenshots/`. With all four, crop to that region |

Numbers are written plainly — `398`, `-615`, `0.5`. Strings are written in double quotes, and a
backslash escapes the character after it, which is how a quote gets inside one:
`typeText("say \"hi\"")`. A quoted string is one whole argument, commas and all, so
`typeText("a, b")` types `a, b` rather than passing two arguments.

Whitespace around a statement and between arguments is ignored, which is what lets an `if` body be
indented.


if blocks
---------

`if` is where the AI comes into a macro. Its condition goes in parentheses, and what fills them is
an AI slot: when the replay reaches the line Pob takes a screenshot and asks the
[model](06_Settings.md) the prompt inside the slot. The answer — true or false — is what the
condition then is, and the block runs or is skipped on it.

```
if (::a save dialog is on screen::) {
    keyPress("return")
    sleep(500)
}
```

The keyword is `if`, the condition is the parenthesised expression between it and the `{` that ends
the line, and a `}` on a line of its own closes the block. Inside is ordinary PSL — including
another `if`, nested as deep as the macro needs. Lines after the `}` run either way; there is no
`else`.

An AI slot is the only expression the parentheses take so far, so `if (::…::)` is the whole of the
form today — the parentheses are what will hold anything else that comes.

Write the keyword lowercase. `IF` is read too, and so is `If`: a block Pob failed to recognise would
run its body unguarded, which is the one thing the condition was written to prevent.

Write a condition a screenshot can settle — "a chat window is open", "the file list is empty". The
model is given the condition and the picture and nothing else: it has no memory of the statements
that ran before, so a condition about what the macro has already done is one it cannot see.

Each `if` is one model call, made as the replay reaches it — a condition inside a block that gets
skipped costs nothing, and a macro with no `if` never calls the model at all. Each judgement is kept
under `logs/<session>/conditions/<n>/`, with the screenshot it was judged from (see
[Logs](05_Logs.md)), and `pob --session <id>` lists them.

Recording never writes an `if`. It is the part you write by hand, into a macro that is otherwise
recorded.


When something is wrong
-----------------------

Nothing here stops the run. PSL is read as far as it makes sense and what is left of it is
played, because the alternative — refusing a forty-line macro over line thirty-one — helps nobody.
The one thing that is never done is running statements a broken `if` was written to guard.

| Written | What happens |
|---------|--------------|
| A line that is not a call — no parentheses, nothing after the `)` | Logged and skipped |
| A name that is not one of the statements above | Logged and skipped |
| A call whose numbers cannot be read — `move(1)`, `scroll(a, b)` | Does nothing, and says nothing |
| An `if` missing its parentheses, its `::…::` slot or the `{` at the end of the line — or holding an empty slot | Its whole block is dropped, and the drop is logged |
| An `if` whose `}` is missing | The end of the macro closes it |
| A `}` with no `if` above it | Logged and skipped |
| An `if` the model cannot judge — no answer, an unreadable one, no screenshot | Reads as false: the block is skipped, with the reason in the log |
| An `if` in a macro with no `openai_api_key` | The macro does not run at all: **Settings needed** goes up before the cursor moves |


How it runs
-----------

The cursor starts at the origin — a replay resets it there first — and every `move` and `drag` is
relative to where it already is. That is why `resetCursor()` is recorded when something sent the
cursor home mid-sequence: skip the jump back and every move after it starts from the wrong place.

All coordinates are screenshot pixels, origin top-left, x right, y down. The cursor is held inside
the Pob window: a move that would take it past an edge stops at the edge, since everything the
script addresses — what the screenshots show, what the clicks go through — is inside that window.

Between one call and the next Pob waits `macro_default_delay` milliseconds, one second by default
(see [Settings](06_Settings.md)). A UI that needs longer gets an explicit `sleep()`. Judging an `if`
adds no delay of its own — the model call is the wait.

Stop halts the run between statements, and during a `sleep()` rather than after it.


See also
--------

- [Macro](03_Macro.md) — `macro.txt`, the record and play buttons, and what a recording captures
- [Key names](04_Keys.md) — what `keyPress` accepts
- [Settings](06_Settings.md) — `macro_default_delay`, and the model an `if` is judged with
- [Logs](05_Logs.md) — the session a run writes, and where each `if` judgement is kept
- [CLI](07_CLI.md) — `pob macro` runs `macro.txt` from the terminal
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file

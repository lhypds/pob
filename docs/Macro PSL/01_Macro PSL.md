
Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps one per instance, in a file called `macro.psl`, and **Prompt Script Language** — PSL — is
what that file is written in. These pages are both: the macro, and the language it is a macro in.

PSL is not Pob's own. It is a language with a compiler of its own — [psl](https://github.com/lhypds/psl)
— and Pob is one program that happens to write in it. The `:: … ::` slots below are filled by running
that compiler, which is where the models and the API keys live. Pob holds none of its own.

The language is small enough that a recording is readable, and readable enough that the recording is
worth editing afterwards. That is the whole point of the pair — you record a macro by doing the
thing once, and what you get back is a program you can open.

The name is what sets PSL apart from a scripting language that only ever does what it is told.
Anywhere a value would go, a statement can hold a prompt instead — `:: … ::`, an AI slot — and what
the AI answers is what that part of the line then says. Written on a line of its own, where a whole
statement would go, what the AI answers is the statements. A macro written in PSL is part instruction
and part question: it repeats what it was given, and asks about what it could not be.

```
move(398, 915)
click()
drag(-775, -615)
if (:: the window focus on a wechat user ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply to the message on screen ::)
}
keyPress("return")
```

Three things write a macro and one thing runs it. Recording writes it from what you, the AI and the
[MCP](../Pob/08_MCP.md) clients do; you write it by hand in an editor; the AI writes it as it works.
Execute, `pob macro` and the [Control API](../Pob/11_Control%20API.md) all run it the same way.


The pages
---------

| Page | What's in it |
|------|--------------|
| [macro.psl](02_macro.psl.md) | The file itself: where it lives, and how recording writes it |
| [Structure](03_Structure.md) | One statement per line, and the three kinds of statement |
| [Comments](04_Comments.md) | `//` and `/* … */`, and why the text stays in the file |
| [AI slot](05_AI%20slot.md) | `:: … ::` — what it is filled from, and what it has to come back as |
| [Calls](06_Calls.md) | Every statement, its arguments and what it does |
| [if blocks](07_if%20blocks.md) | Guarding a block with a condition |
| [loop blocks](08_loop%20blocks.md) | Running a block again and again, with a count and a condition |
| [stop](09_stop.md) | Ending the run from inside the macro |
| [call](10_call.md) | Replaying another PSL file where the statement stands |
| [When something is wrong](11_When%20something%20is%20wrong.md) | The check before the run, the replay's own forgiveness, and `pob macro --check` |
| [How it runs](12_How%20it%20runs.md) | The origin, the delay between statements, and what Stop does |


See also
--------

- [UI](../Pob/02_UI.md) — the Macro PSL, record and Execute buttons
- [Key names](../Pob/04_Keys.md) — what `keyPress` accepts
- [Settings](../Pob/06_Settings.md) — `macro_default_delay`, `image_scale`, and where the `psl` executable is
- [psl](https://github.com/pob/psl) — the compiler, its `.pslrc`, and the models it is pointed at
- [Logs](../Pob/05_Logs.md) — the session a run writes, and where each slot the AI filled is kept
- [CLI](../Pob/07_CLI.md) — `pob macro` runs `macro.psl` from the terminal
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools, recorded into the same file

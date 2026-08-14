
Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps them per instance, in that instance's `src/` folder, and **Prompt Script Language** — [PSL](https://github.com/lhypds/psl) —
is what those files are written in. The entry point is `main.macro.psl`.

```
move(398, 915)
click()
if (:: the window focus on a wechat user ::) {
    move(:: the x offset to the message box ::, 738)
    click()
    typeText(:: a short reply to the message on screen ::)
}
keyPress("return")
```

One statement per line, run top to bottom. Anywhere a value would go, a statement can hold a prompt
instead — `:: … ::`, an AI slot — and what the AI answers is what that part of the line then says.
Written on a line of its own, where a whole statement would go, what the AI answers is the
statements, and they are replayed there. The slots are filled by running PSL, the compiler the
language has of its own, which is where the models and the API keys live.

The language has documentation of its own, a page per part of it. Please refer to
**[Macro PSL](../Macro%20PSL/01_Macro%20PSL.md)** for all of it:


See also
--------

- [UI](02_UI.md) — the Macro PSL, record and Execute buttons
- [Key names](04_Keys.md) — what `keyPress` accepts
- [Settings](06_Settings.md) — `macro_default_delay`, `image_scale`, and where the `psl` executable is
- [Logs](05_Logs.md) — the session a run writes, and where each slot the AI filled is kept
- [CLI](07_CLI.md) — `pob start` runs `src/main.macro.psl` from the terminal, and `pob check` reads it without running it
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file

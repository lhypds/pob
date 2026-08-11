
Macro PSL
=========

A macro is a sequence of actions Pob plays back — recorded from what you do, or written by hand.
Pob keeps one per instance, in a file called `macro.psl`, and **Prompt Script Language** — PSL — is
what that file is written in.

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
The slots are filled by running [psl](https://github.com/lhypds/psl), the compiler the language has
of its own, which is where the models and the API keys live.

The language has documentation of its own, a page per part of it. Please refer to
**[Macro PSL](../Macro%20PSL/01_Macro%20PSL.md)** for all of it:


See also
--------

- [UI](02_UI.md) — the Macro PSL, record and Execute buttons
- [Key names](04_Keys.md) — what `keyPress` accepts
- [Settings](06_Settings.md) — `macro_default_delay`, `image_scale`, and where the `psl` executable is
- [Logs](05_Logs.md) — the session a run writes, and where each slot the AI filled is kept
- [CLI](07_CLI.md) — `pob macro` runs `macro.psl` from the terminal
- [MCP Server](08_MCP.md) — the same actions as MCP tools, recorded into the same file

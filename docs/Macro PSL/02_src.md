
src/
====

Each instance keeps its macros in `src/`, under its `~/.pob/<instance>/` directory. The Macro PSL
button (🪄) in the toolbar opens that folder; Execute (▶) checks and replays `main.macro.psl`, the
entry point; Reset (↻) empties it.

```
~/.pob/<instance>/src/
    +--- main.macro.psl      the entry point — what Execute runs and Record writes
    +--- sign-out.macro.psl  called by main, with slots of its own
    +--- open-inbox.macro    called by main, with no slots in it at all
```

A [call](10_call.md) names a file relative to the one the call is written in, so files sitting beside
each other in `src/` are named by their bare names — `call("sign-out.macro.psl")`.


The two extensions
------------------

A macro says in its name whether the compiler has anything to do with it.

| Extension | What it means |
|-----------|---------------|
| `.macro.psl` | May hold `:: … ::` [AI slots](05_AI%20slot.md). Running it means running [psl](https://github.com/lhypds/psl) once per slot, so psl has to be installed |
| `.macro` | No slots. Every statement already says what it does, so it is replayed exactly as written and psl is never started |

The distinction is in the name rather than in the contents because of *when* it has to be known.
Whether psl is installed is checked before the cursor moves, and what a `call()` three files down
will need is a question asked of a name on a line long before that file is ever read. A name answers
it; contents answer it only once something has gone looking.

It also gives the two kinds different costs. A `.macro` is a recording — it replays the same way on a
machine with no compiler and no API key behind it. A `.macro.psl` is a program with judgement in it,
and the price of that judgement is psl on the `PATH`.

A `:: … ::` written in a `.macro` is a contradiction, and the [check](11_When%20something%20is%20wrong.md)
names it before the run starts rather than letting the replay skip the statement halfway down:

```
line 2: steps.macro is a .macro, which is replayed without psl, so this :: … :: would never be
        filled — remove the slot, or rename the file to steps.macro.psl
```


Recording
---------

Use the record button (⏺) to write `main.macro.psl` instead of typing it — actions are appended to it
as they happen. Starting a recording while it still holds statements asks what to do with them first:
clear them, or keep them and record after them. Keeping them writes a `resetCursor()` between the old
lines and the new ones, since every move recorded next is relative to the origin a replay starts at.

Recording captures every action that drives the machine, whichever one of the three is driving it:
your own mouse and keyboard, the AI's tool calls, and the tools an [MCP](../Pob/08_MCP.md) client calls.
They all append to the same `main.macro.psl`, in the order things happened. The MCP tools that take an
absolute `(x, y)` are written down as the relative `move(dx, dy)` from wherever the cursor was, so a
recording made through MCP plays back like any other — the language has
[absolute calls](06_Calls.md) too, and they are there for writing a macro by hand rather than for
recording one.

Your own mouse and keyboard are recorded on macOS only, for now — watching the input of other
applications is a different mechanism on each system, and the Linux and Windows shells do not have
it yet. On those two the record button still captures everything the AI and MCP clients drive.

A macro recorded before the files moved is carried over the first time this Pob runs — from
`macro.txt` to `macro.psl` to `src/main.macro.psl`, in one go — so nothing that was recorded is lost
to the renames.


See also
--------

- [Structure](03_Structure.md) — what the lines in the file are
- [call](10_call.md) — replaying another file where the statement stands
- [UI](../Pob/02_UI.md) — the Macro PSL, record and Execute buttons
- [MCP Server](../Pob/08_MCP.md) — the same actions as MCP tools, recorded into the same file
- [Logs](../Pob/05_Logs.md) — the session a run writes

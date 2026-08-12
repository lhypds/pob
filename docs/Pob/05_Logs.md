
Logs
====

Structure  

```
~/.pob/  
    +--- INSTANCE                                 names the instance directory below.
    +--- settings.json                            this machine's [settings](06_Settings.md), shared by every instance.
    +--- app.log                                  the machine's short record across instances: the app starting and stopping, an instance starting and stopping, and errors.

    +--- pb-<uid>/                                an instance directory; the one INSTANCE names is the one in use.
         +--- instance.json                       which instance this is: its id, the name `pob new` gave it, when it last ran, and how the shell last left the window — where it was (`window_x`, `window_y`, `window_width`, `window_height`) and whether it was locked (`is_locked`). While it runs it also carries the pid and the [Control API](11_Control%20API.md) port the `pob` CLI reaches it on.
         +--- instance.log                        timestamped instance and macro lifecycle, every executed step, important core messages, and what each `:: … ::` slot was filled with.
         +--- src/                                this instance's [macros](03_Macro%20PSL.md).
         |    +--- main.macro.psl                 the entry point: what Record writes and Execute replays. `.macro.psl` says psl fills its `:: … ::` slots.
         |    +--- <name>.macro.psl               anything `main` calls that has slots of its own.
         |    +--- <name>.macro                   anything it calls that has none — replayed as written, without psl.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over.
         +--- screenshots/                        screenshots taken with the toolbar Screenshot button. Yours, not a run's, so they sit here rather than under logs/.

         +--- logs/
              +--- <session>/                    one replay of main.macro.psl.
                   +--- session.json              session details, start time and end time.
                   +--- main.macro.psl            the macro as it stood when this session ran, slots and all.
                   +--- slots/                    one directory per `:: … ::` slot filled, numbered in the order they were filled.
                        +--- <n>/
                             +--- slot.json       the instruction, the statement and the file and line it came from, what was filled in, and which model filled it.
                             +--- psl.txt         what the compiler said while filling it.
                             +--- screenshot.png  what the slot was filled from.
                   +--- screenshots/              screenshots taken during the session with `takeScreenshot()` tool.
```

`<instance>` is the instance ID, of the form `pb-<4 hex>` (the last two bytes of a fresh UID in
lowercase hex). It is shown in the toolbar beside the window buttons — so the ID on screen names the
directory to look in — and a machine keeps the same one for good: it is worked out on first run and
recorded in `~/.pob/INSTANCE`, so every session ever run lands in the same directory.

What an instance works on is inside its own directory and nothing of it is shared between IDs.
Point `INSTANCE` at another one — write a different `pb-<4 hex>` into it, or delete the file to have
one drawn — and Pob starts from an empty macro, with the old directory left
untouched beside it. That is what changing it is for: `INSTANCE` is the only thing that says which
directory is in use, so deleting it always starts a new instance rather than picking up one of the
directories already there.

The [settings](06_Settings.md) are the exception, and sit at the root for it: where psl is and which
port the server takes are how the machine works whichever instance is running, so a new instance is a
clean sheet of work rather than a machine to set up again.

The two logs are kept apart on purpose. `app.log` answers "did it come up, and did anything break":
`Pob started` and `Pob stopped` for the app, `pob-core started (instance …)` and `pob-core stopping`
for each instance, and failures, written `ERROR …` after the timestamp. Nothing else goes in it — a
log that has to be scrolled cannot answer that at a glance. The dev start scripts and `pob launch`
redirect the shell's own output there too, so a crash lands beside the line it stopped after.

Everything else is detail, and detail belongs to the instance. Both the shell and pob-core write
`instance.log`, so the toolbar's, the shell's and the core's side of a run read in order in the one
file — and the lifecycle and error lines are repeated there, beside the detail that led to them.
This is what the toolbar's `ins.log` button opens.

`instance.log` is append-only across starts and sessions. Every row begins with a fixed-width RFC
3339 UTC timestamp with six fractional digits and an event name. Multiline content is logged as
separately timestamped rows, so nothing leaves unlabelled continuation lines.

A few events carry a marker so a run can be found by eye in a long file, one arrow per level of it:
`>>> MACRO START REQUEST` opens a run, `>> LOOP START` a loop inside it — `>> ONCE START` a
[`once`](03_Macro%20PSL.md) — and `> STEP START` each
statement. What ends is left unmarked — an opening is what gets scanned for. `STEP START` and `STEP END` name the
line, resolved statement, and completion state for each statement that reaches execution; condition
checks and loop passes are included too. A `once` adds `ONCE CHANGE` for each change it saw in the
screen, with how much of the picture moved, and `ONCE RUN` where the condition then held and the
block ran; the intervals where the screen sat still say nothing at all. The session and macro file appear on the surrounding `MACRO
START` event instead of being repeated on every step and loop row; `MACRO STOP` repeats only the
session so the boundary remains explicit.

What a `:: … ::` slot became is the row for the line it was on:
`Macro <where>: <as written> -> <as filled>`, with which model answered and how long it took in
brackets after it — one bracket per slot, since a line with two of them was two model calls. The
statement on it is the one that runs, in screen pixels.

A slot answered off a shrunken picture (see `image_scale` in [Settings](06_Settings.md)) adds the
one row that statement cannot show: `Macro slot :: <instruction> :: -> <answer>, scaled back to
<statement>` — what the model wrote, and what it became once grown back to the screen's own pixels.

The macro source sent to psl, the raw response, the system prompt, and the compiler output are not
copied into this log; the per-session `slots/<n>/` directory keeps `slot.json`, `psl.txt` and the
screenshot for detailed diagnostics.

The file still contains resolved statements, which can include text a macro typed or other sensitive
screen-related instructions. Protect or remove `instance.log` when sharing an instance directory.

`pob new "Work laptop"` is that move done for you: it creates the directory, records the name in
`instance.json`, and points `INSTANCE` at it. `pob launch` lists the instances by name and asks
which one to start — see [CLI](07_CLI.md).  
`<session>` is a unique session ID named as a unixtime.  
`<n>` is the position of an [AI slot](03_Macro%20PSL.md) in the order the macro filled them (e.g. `1`, `2`, `3`) — a `loop` asks the slots inside it once per pass and a `once` asks its own at every change it acts on, and each of those is one of these.  
A slot written on a line of its own is filled with statements rather than with a value, and `slot.json` holds the whole block it filled to.  
There is no second copy of the macro with the answers written into it. For much of a macro there is no one answer to write down: a slot inside a `loop` is asked once per pass and only the last pass's could go in, and a slot the replay never reached was never asked at all. `main.macro.psl` is the macro, and `slots/` is what each fill was, in the order the fills happened — the `line` in every `slot.json` counts to that one file, which does not move.  


See also
--------

- [UI](02_UI.md) — the toolbar buttons that open this tree
- [CLI](07_CLI.md) — `pob` reads it directly, so it works with the app closed
- [Control API](11_Control%20API.md) — what `instance.json` advertises while it runs
- [Settings](06_Settings.md) — the `settings.json` kept at the root, and where psl keeps its own
- [Macro PSL](03_Macro%20PSL.md) — the `main.macro.psl` a session replays

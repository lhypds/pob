
Logs
====

Structure  

```
~/.pob/  
    +--- INSTANCE                                 names the instance directory below.
    +--- settings.json                            this machine's [settings](06_Settings.md), shared by every instance.
    +--- app.log                                  the app's own log, across instances.

    +--- pb-<uid>/                                an instance directory; the one INSTANCE names is the one in use.
         +--- instance.json                       which instance this is: its id, the name `pob new` gave it, when it last ran, and how the shell last left the window — where it was (`window_x`, `window_y`, `window_width`, `window_height`) and whether it was locked (`is_locked`). While it runs it also carries the pid and the [Control API](11_Control%20API.md) port the `pob` CLI reaches it on.
         +--- instance.log                        timestamped instance and macro lifecycle, every executed step, important core messages, psl request source, and response summaries and answers.
         +--- macro.psl                           the [macro](03_Macro%20PSL.md) Record writes and Execute replays.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over.
         +--- screenshots/                        screenshots taken with the toolbar Screenshot button. Yours, not a run's, so they sit here rather than under logs/.

         +--- logs/
              +--- <session>/                    one replay of macro.psl.
                   +--- session.json              session details, start time and end time.
                   +--- macro.psl                 the macro as it stood when this session ran.
                   +--- macro.txt                 the same macro compiled: every `:: … ::` replaced by what psl filled it with, the ones never asked about written out as `<instruction>`, and a slot that stood for statements opened out into the block it filled to.
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

`instance.log` is append-only across starts and sessions. Every row begins with a fixed-width RFC
3339 UTC timestamp with six fractional digits and an event name. Multiline request source is logged as separately timestamped rows under
`PSL REQUEST CONTENT`, so the exact file remains readable without unlabelled continuation lines.
The separate PSL system prompt, raw response file, and compiler output are not copied into this log;
response metadata is under `PSL RESPONSE` and the accepted value is under `PSL ANSWER`. The existing
per-slot `psl.txt` keeps compiler output for detailed diagnostics. `STEP START` and `STEP END` name
the line, resolved statement, and completion state for each statement that reaches execution;
condition checks and loop passes are included too. The session and macro file appear on the
surrounding `MACRO START` event instead of being repeated on every step, loop, and psl row; `MACRO
STOP` repeats only the session so the boundary remains explicit.

The file deliberately contains the complete macro text sent to psl and its complete response. That
can include text typed by a macro or other sensitive screen-related instructions. Protect or remove
`instance.log` when sharing an instance directory.

`pob new "Work laptop"` is that move done for you: it creates the directory, records the name in
`instance.json`, and points `INSTANCE` at it. `pob launch` lists the instances by name and asks
which one to start — see [CLI](07_CLI.md).  
`<session>` is a unique session ID named as a unixtime.  
`<n>` is the position of an [AI slot](03_Macro%20PSL.md) in the order the macro filled them (e.g. `1`, `2`, `3`) — a `loop` asks the slots inside it once per pass, and each of those is one of these.  
A slot written on a line of its own is filled with statements rather than with a value, and `slot.json` holds the whole block it filled to. `macro.txt` holds it too, opened out into lines where the one line that asked for it was — so its line numbers are its own from that point, while the `line` in each `slot.json` is a line of `macro.psl`.  


See also
--------

- [UI](02_UI.md) — the toolbar buttons that open this tree
- [CLI](07_CLI.md) — `pob` reads it directly, so it works with the app closed
- [Control API](11_Control%20API.md) — what `instance.json` advertises while it runs
- [Settings](06_Settings.md) — the `settings.json` kept at the root, and where psl keeps its own
- [Macro PSL](03_Macro%20PSL.md) — the `macro.psl` a session replays


Logs
====

Structure  

```
~/.pob/  
    +--- INSTANCE                                 names the instance directory below.
    +--- settings.json                            this machine's [settings](06_Settings.md), shared by every instance.
    +--- app.log                                  the app's own log, across instances.

    +--- pb-<uid>/                                an instance directory; the one INSTANCE names is the one in use.
         +--- instance.json                       which instance this is: its id, the name `pob new` gave it, when it last ran, and where the shell last left the window (`window_x`, `window_y`, `window_width`, `window_height`). While it runs it also carries the pid and the [Control API](11_Control%20API.md) port the `pob` CLI reaches it on.
         +--- macro.psl                           the [macro](03_Macro%20PSL.md) Record writes and Execute replays.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over.

         +--- logs/
              +--- screenshots/                   screenshots taken with the toolbar Screenshot button.

              +--- <session>/                    one replay of macro.psl.
                   +--- session.json              session details, start time and end time.
                   +--- macro.psl                 the macro as it stood when this session ran.
                   +--- macro.txt                 the same macro compiled: every `:: … ::` replaced by what psl filled it with, and the ones never asked about written out as `<instruction>`.
                   +--- slots/                    one directory per `:: … ::` slot filled, numbered in the order they were filled.
                        +--- <n>/
                             +--- slot.json       the instruction, the statement and the file and line it came from, what was filled in, and which model filled it.
                             +--- psl.txt         what the compiler said while filling it.
                             +--- screenshot.png  what the slot was filled from.
                   +--- screenshots/              screenshots taken during the session with `take_screenshot()` tool.
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

`pob new "Work laptop"` is that move done for you: it creates the directory, records the name in
`instance.json`, and points `INSTANCE` at it. `pob launch` lists the instances by name and asks
which one to start — see [CLI](07_CLI.md).  
`<session>` is a unique session ID named as a unixtime.  
`<n>` is the position of an [AI slot](03_Macro%20PSL.md) in the order the macro filled them (e.g. `1`, `2`, `3`) — a `loop` asks the slots inside it once per pass, and each of those is one of these.  


See also
--------

- [UI](02_UI.md) — the toolbar buttons that open this tree
- [CLI](07_CLI.md) — `pob` reads it directly, so it works with the app closed
- [Control API](11_Control%20API.md) — what `instance.json` advertises while it runs
- [Settings](06_Settings.md) — the `settings.json` kept at the root, and where psl keeps its own
- [Macro PSL](03_Macro%20PSL.md) — the `macro.psl` a session replays

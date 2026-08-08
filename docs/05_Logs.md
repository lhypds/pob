
Logs
====

Structure  

```
~/.pob/  
    +--- INSTANCE                                 names the instance directory below; the only thing above it, apart from app.log.
    +--- app.log                                  the app's own log, across instances.

    +--- pb-<uid>/                                an instance directory; the one INSTANCE names is the one in use.
         +--- instance.json                       which instance this is: its id, the name `pob new` gave it, and when it last ran. While it runs it also carries the pid and the [Control API](11_Control%20API.md) port the `pob` CLI reaches it on.
         +--- settings.json                       this instance's settings.
         +--- instruction.txt                     what Execute runs.
         +--- macro.txt                           what Record writes and Play replays.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over.

         +--- logs/
              +--- screenshots/                   screenshots taken with the toolbar Screenshot button.

              +--- <session>/ (instruction)       session executed from instruction.  
                   +--- instruction.txt
                   +--- session.json              session details, usage, etc.
                   +--- <plan>/
                        +--- plan.json
                        +--- messages.json
                        +--- response.json
                        +--- <step>/              the sequence of plan steps (eg, 1, 2, 3...).
                              +--- <log>          the step log.
                              +--- step.json      the step details, instruction, expectation, etc.
                              +--- verification/  verification results for the step
                                  +--- messages.json
                                  +--- response.json
                   +--- screenshots/              screenshots taken during the session with `take_screenshot()` tool.  

              +--- <session>/ (macro)             session executed from macro.
                   +--- session.json              session details, start time, end time, etc.
                   +--- macro.txt
                   +--- screenshots/              screenshots taken during the session with `take_screenshot()` tool.
```

`<instance>` is the instance ID, of the form `pb-<4 hex>` (the last two bytes of a fresh UID in
lowercase hex). It is shown in the toolbar beside the window buttons — so the ID on screen names the
directory to look in — and a machine keeps the same one for good: it is worked out on first run and
recorded in `~/.pob/INSTANCE`, so every session ever run lands in the same directory.

Everything an instance works with is inside its own directory and nothing is shared between IDs.
Point `INSTANCE` at another one — write a different `pb-<4 hex>` into it, or delete the file to have
one drawn — and Pob starts from clean settings, an empty instruction and an empty macro, with the
old directory left untouched beside it. That is what changing it is for: `INSTANCE` is the only
thing that says which directory is in use, so deleting it always starts a new instance rather than
picking up one of the directories already there.

`pob new "Work laptop"` is that move done for you: it creates the directory, records the name in
`instance.json`, and points `INSTANCE` at it. `pob launch` lists the instances by name and asks
which one to start — see [CLI](07_CLI.md).  
`<session>` is a unique session ID named as a unixtime.  
`<plan>` is a unique plan ID named as a unixtime.  
`<step>` is the sequence number of the step (e.g. `1`, `2`, `3`).  
`<log>` is a unique log ID named as a unixtime.  


See also
--------

- [UI](02_UI.md) — the toolbar buttons that open this tree
- [CLI](07_CLI.md) — `pob` reads it directly, so it works with the app closed
- [Control API](11_Control%20API.md) — what `instance.json` advertises while it runs
- [Settings](06_Settings.md) — the `settings.json` kept in the instance directory

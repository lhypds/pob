
Logs
====

Structure  

```
~/.pob/logs/  
    +--- pb-<uid>/                                the machine's instance directory, the same one every run.
         +--- screenshots/                        screenshots taken with the toolbar Screenshot button.
         +--- settings.json                       this instance's settings file (copied from the root `settings.json`).
         +--- instance.json                       instance start/end times, etc.
         +--- control.json                        written while the instance runs; advertises the control API port used by the `pob` CLI.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over, and what Clear Logs checks.

         +--- <session>/ (instruction)            session executed from instruction.  
              +--- instruction.txt
              +--- session.json                   session details, usage, etc.
              +--- <plan>/
                   +--- plan.json
                   +--- messages.json
                   +--- response.json
                   +--- <step>/                   the sequence of plan steps (eg, 1, 2, 3...).
                         +--- <log>               the step log.
                         +--- step.json           the step details, instruction, expectation, etc.
                         +--- verification/       verification results for the step
                             +--- messages.json
                             +--- response.json
              +--- screenshots/                   screenshots taken during the session with `take_screenshot()` tool.  

         +--- <session>/ (macro)                  session executed from macro.
              +--- session.json                   session details, start time, end time, etc.
              +--- macro.txt
              +--- screenshots/                   screenshots taken during the session with `take_screenshot()` tool.
```

`<instance>` is the instance ID, of the form `pb-<4 hex>` (the last two bytes of a fresh UID in
lowercase hex). It is shown in the toolbar beside the window buttons — so the ID on screen names the
directory to look in — and a machine keeps the same one for good: it is worked out on first run and
recorded in `~/.pob/instance`, so every session ever run lands in the same directory. A machine
upgrading from the versions that took a fresh ID per launch adopts the `pb-*` directory it used
last; the others stay where they are as history.  
`<session>` is a unique session ID named as a unixtime.  
`<plan>` is a unique plan ID named as a unixtime.  
`<step>` is the sequence number of the step (e.g. `1`, `2`, `3`).  
`<log>` is a unique log ID named as a unixtime.  


See also
--------

- [UI](UI.md) — the toolbar buttons that open and clear this tree
- [CLI](CLI.md) — `pob` reads it directly, so it works with the app closed
- [Settings](Settings.md) — the per-instance `settings.json` kept here

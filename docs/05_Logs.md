
Logs
====

Structure  

```
~/.pob/  
    +--- INSTANCE                                 names the instance directory below.
    +--- settings.json                            this machine's [settings](06_Settings.md), shared by every instance.
    +--- app.log                                  the app's own log, across instances.
    +--- llm.log                                  one block per model call, across instances: what was asked, what came back, and what it cost.

    +--- pb-<uid>/                                an instance directory; the one INSTANCE names is the one in use.
         +--- instance.json                       which instance this is: its id, the name `pob new` gave it, when it last ran, and where the shell last left the window (`window_x`, `window_y`, `window_width`, `window_height`). While it runs it also carries the pid and the [Control API](11_Control%20API.md) port the `pob` CLI reaches it on.
         +--- macro.psl                           the [macro](03_Macro%20PSL.md) Record writes and Execute replays.
         +--- .lock                               held locked while Pob runs; this is what a second launch trips over.

         +--- logs/
              +--- screenshots/                   screenshots taken with the toolbar Screenshot button.

              +--- <session>/                    one replay of macro.psl.
                   +--- session.json              session details, start time, end time, and the usage of the `::…::` slots, if it had any.
                   +--- macro.psl                 the macro as it stood when this session ran.
                   +--- slots/                    one directory per `::…::` slot filled, numbered in the order they were filled.
                        +--- <n>/
                             +--- slot.json       the prompt, the statement and the line of macro.psl it came from, what the AI filled in, and the reason.
                             +--- messages.json
                             +--- response.json
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

The [settings](06_Settings.md) are the exception, and sit at the root for it: the API key, the model
and the port are how the machine works whichever instance is running, so a new instance is a clean
sheet of work rather than a machine to set up again.

`pob new "Work laptop"` is that move done for you: it creates the directory, records the name in
`instance.json`, and points `INSTANCE` at it. `pob launch` lists the instances by name and asks
which one to start — see [CLI](07_CLI.md).  
`<session>` is a unique session ID named as a unixtime.  
`<n>` is the position of an [AI slot](03_Macro%20PSL.md) in the order the macro filled them (e.g. `1`, `2`, `3`).  


llm.log
-------

Every model call Pob makes is one block in `~/.pob/llm.log`, written whether it succeeded or not.
There is one place in the code they all go through, so this is the whole of what the app spends —
across instances, since the account being billed is the machine's rather than one instance's.

```
[2026-08-10T14:23:01Z] macro slot ::the x offset to the Save button::  (session 1752712400, macro.psl line 4)
  endpoint   https://api.openai.com/v1/chat/completions
  model      gpt-5.6
  request    2 messages, 1 image, json_schema, 234.7 KB
  duration   2.413s
  status     ok
  usage      1870 tokens = 1843 prompt (512 cached) + 27 completion (8 reasoning)
  cost       $0.002574 estimated — in 1843 × $1.25/M + out 27 × $10/M
  response   {"value":"-120","reason":"The Save button sits 120px left of the cursor."}
```

A call that failed says so and carries what the provider said instead of the usage — including one
that never left the machine, like a request made with no `openai_api_key`:

```
  status     failed
  error      HTTP 429: {"error":{"message":"Rate limit reached for gpt-5.6"}}
```

`cost` is the one line that needs setting up. A provider that reports what it charged is believed
outright — that number is the one on the bill. Otherwise Pob works it out from
`price_input_per_1m` and `price_output_per_1m` in [settings.json](06_Settings.md), which is why the
line says *estimated*: it prices prompt and completion tokens flat, and does not know what your
account pays for a cached token. With neither, the line names the two settings and the token counts
are logged anyway — a bill is worked out from those either way.

What the file does not hold is the messages. A request carries screenshots, and a base64 PNG per
entry would make it unopenable within a day; the full conversation, images stripped, is in the
session's own `slots/<n>/messages.json`, which the purpose line names the session and line for.

- [UI](02_UI.md) — the toolbar buttons that open this tree
- [CLI](07_CLI.md) — `pob` reads it directly, so it works with the app closed
- [Control API](11_Control%20API.md) — what `instance.json` advertises while it runs
- [Settings](06_Settings.md) — the `settings.json` kept at the root, shared by every instance, and the prices `llm.log` works the money out from
- [Macro PSL](03_Macro%20PSL.md) — the `macro.psl` a session replays

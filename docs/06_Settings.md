
Settings
========

`~/.pob/settings.json` is this machine's settings file — the one the Settings
menu opens, and the one both the shell and the Go core read and edit. It is
created from the defaults below the first time Pob starts.

It sits at the root rather than inside an instance directory because it is how
the machine works, not what one instance is doing with it: where psl is and
which port the server takes are the same whichever instance is running. Pointing
[`~/.pob/INSTANCE`](05_Logs.md) at another id therefore starts Pob on a clean
`macro.psl`, on a machine that is already set up.

A settings file from an older Pob — one per instance, inside its directory — is
moved up to the root on the next run, so a machine that was set up stays set
up. Only the first one moves: if several instances were configured separately,
the rest are left where they are to be copied across by hand.

| Key | Default | Description |
|-----|---------|-------------|
| `psl` | `psl` | The [psl](03_Macro%20PSL.md) compiler Pob runs to fill a macro's `:: … ::` slots — a name to find on the `PATH`, or a path to the executable. **Which model it uses and what key that takes are psl's own, kept in its `.pslrc`; Pob holds no API key.** |
| `image_scale` | `1` | How much of the screenshot a [`:: … ::`](03_Macro%20PSL.md) slot is filled from the model is shown: `1` the picture as taken, `0.5` half as wide and half as tall — a quarter of the pixels, and roughly a quarter of the image tokens it spends reading them (`0.1`–`1`, clamped). Pob grows the answer back, so the macro is written in screen pixels either way |
| `macro_default_delay` | `1000` | Milliseconds Pob waits between one [`macro.psl`](03_Macro%20PSL.md) statement and the next. A UI that needs longer gets an explicit `sleep()` |
| `editor` | `system` | Editor used to open config files (`system`, `vscode`, `zed`, `sublime_text`, `vim`) |
| `terminal` | `system` | Terminal used when editor is `vim` (`system`, `iterm2`) |
| `stop_hook` | — | Shell command to run when a macro runs to its end (e.g. `afplay /System/Library/Sounds/Morse.aiff`). A stopped run does not fire it |
| `server` | `true` | Run the [Pob Server](09_Server.md). `false` stops Pob accepting pointer and keyboard commands from the network, and takes the [Web UI](12_Web UI.md) down with it |
| `server_port` | `8033` | The port the [Pob Server](09_Server.md) is reached through. `POB_SERVER_PORT` overrides it |
| `webui_view_fps` | `5` | How often the [Web UI](12_Web UI.md)'s view page refetches the picture, in frames per second (`0.1`–`30`, clamped). Every frame is a screen capture on this machine, which is why the rate is set here and not on the page |
| `mcp` | `true` | Run the [MCP server](08_MCP.md), which starts with the instance so a client that has Pob registered finds it there. `false` keeps the port closed |
| `mcp_port` | `8032` | The port [MCP](08_MCP.md) clients reach this machine through — the one written into their config, so changing it means changing theirs. `POB_MCP_PORT` overrides it |
| `mcp_host` | `0.0.0.0` | The interface the [MCP server](08_MCP.md) binds. Every one of them by default, so a client on another machine reaches it — loopback keeps working alongside. `127.0.0.1` closes it to this machine. `POB_MCP_HOST` overrides it |

`editor` names a preference, not a requirement: an editor that is not installed
on this machine falls back to the system one, since the setting travels between
machines and the editor does not. On Linux that means `xdg-open` and, where
nothing is registered for `.json` and `.txt`, the text editor the desktop ships
— so a toolbar button always opens something.

Everything here is something you set, and it applies to the machine. Where the
window was last left is neither — it is written by the shell as `window_x`,
`window_y`, `window_width` and `window_height` in
[`instance.json`](05_Logs.md), per instance, with the rest of what an instance
records about itself. A settings file still holding those keys has them moved
across on the next run.

Example:

```json
{
  "psl": "psl",
  "image_scale": 1,
  "macro_default_delay": 1000,
  "editor": "vscode",
  "stop_hook": "afplay /System/Library/Sounds/Morse.aiff",
  ...
}
```


The model
---------

There is no model here, and no API key. A `:: … ::` slot is filled by running
the [psl](03_Macro%20PSL.md) compiler, and psl is configured on its own terms —
`.pslrc`, which it looks for in `~/.pob` (where Pob runs it) and then in your
home directory:

```text
default_model=claude-opus-5

[claude-opus-5]
base_url=https://api.anthropic.com
api_key=${ANTHROPIC_API_KEY}

[gpt-5.6]
base_url=https://api.openai.com
api_key=${OPENAI_API_KEY}
```

`.pslrc` is optional: with `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` in the
environment psl runs without one. A single slot can name the model it wants —
`:: gpt-5.6> the x offset to the Save button ::` — which is a psl feature Pob
passes straight through. See psl's own README for the rest.

Two settings are on this side: `psl`, which says where the executable is, and
`image_scale`, which says how much of the screenshot the model is shown.

A slot is two bills. One is the picture: a vision model reads it as a grid of
patches, and every patch has to go through the vision encoder before a single
token of the answer is written. The other is the answer itself, one token at a
time. `image_scale` is the first bill and nothing else, so which one is larger is
worth knowing before reaching for it.

The picture is charged by its pixels and not by its bytes. Re-encoding the same
grid smaller — PNG to JPEG — changes what crosses the wire and nothing the model
does; halving the scale quarters the patches, and that is the whole of what makes
a picture cheaper. On one local 8B vision model, filling the same coordinate slot
from a 1736×1384 screenshot took 15.1s, and 6.1s from the same screenshot at
`0.5` — the answer was 8 tokens either way, so almost all of it was the picture.
Measure on your own model rather than taking that number: it is a statement about
one vision encoder on one machine.

The second bill is the one to check first, because it can be far larger and no
picture size touches it. A model that reasons before answering spends thousands
of tokens on a slot that wants the word `true`, and on a local model that is
minutes. If a slot takes a minute, read the token counts in
[`logs/<session>/slots/<n>/psl.txt`](05_Logs.md) before shrinking anything: a
large output is a model to change, not a picture to cut down.

Shrinking is a trade and not a saving. The picture is where a coordinate comes
from, and a coarser picture is a coarser answer — which is why this is `1` until
someone sets it, and why what it is worth is a question about the model you are
actually running.


See also
--------

- [Logs](05_Logs.md) — the tree a run writes, and where every filled slot is kept
- [Macro PSL](03_Macro%20PSL.md) — `macro_default_delay` and `image_scale`, and the compiler a `:: … ::` slot is filled by
- [UI](02_UI.md) — the toolbar button that opens it
- [Pob Server](09_Server.md) — what `server` and `server_port` control
- [MCP Server](08_MCP.md) — what `mcp`, `mcp_port` and `mcp_host` control


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
| `image_scale` | `0.35` | How much of the screenshot a [`:: … ::`](03_Macro%20PSL.md) slot is filled from the model is shown: `0.35` a bit over a third as wide and as tall — an eighth of the pixels, and about a fifth of the input tokens — and `1` the picture as taken (`0.1`–`1`, clamped). Pob grows the answer back, so the macro is written in screen pixels either way. **The default is set aggressively and has no margin in it — read the note below before trusting it on a dense UI.** It applies to filling a slot and nothing else: an [MCP](08_MCP.md) client's `take_screenshot` gets the picture as taken |
| `macro_default_delay` | `1000` | Milliseconds Pob waits between one [`macro.psl`](03_Macro%20PSL.md) statement and the next. A UI that needs longer gets an explicit `sleep()`, which is written as a time — `sleep(3s)` |
| `editor` | `system` | Editor used to open config files (`system`, `vscode`, `zed`, `sublime_text`, `vim`) |
| `terminal` | `system` | Terminal used when editor is `vim` (`system`, `iterm2`) |
| `stop_hook` | — | Shell command to run when a macro runs to its end (e.g. `afplay /System/Library/Sounds/Morse.aiff`). A stopped run does not fire it |
| `server` | `true` | Run the [Pob Server](09_Server.md). `false` stops Pob accepting pointer and keyboard commands from the network, and takes the [Web UI](12_Web%20UI.md) down with it |
| `server_port` | `8033` | The port the [Pob Server](09_Server.md) is reached through. `POB_SERVER_PORT` overrides it |
| `webui_view_fps` | `5` | How often the [Web UI](12_Web%20UI.md)'s view page refetches the picture, in frames per second (`0.1`–`30`, clamped). Every frame is a screen capture on this machine, which is why the rate is set here and not on the page |
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
  "image_scale": 0.35,
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
a picture cheaper.

What shrinking costs is not the precision you would expect. Asked for the centre
of ten small controls in that window — four scattered, and six of them adjacent
glyphs in one row along the bottom edge, about 47px apart — one frontier vision
model answered like this. 300 answers, two screenshots, three runs a scale, every
error in full-resolution pixels after Pob grew the answer back:

| scale | input tokens | median error | p90 error | wrong control | wall clock |
|-------|--------------|--------------|-----------|---------------|------------|
| `1` | 2991 | 1.0px | 1.4px | none | 4.1s |
| `0.5` | 1018 | 0.0px | 2.2px | none | 4.3s |
| `0.4` | 754 | 1.1px | 2.5px | none | 5.0s |
| `0.35` | 643 | 1.7px | 3.6px | none | 7.5s |
| `0.3` | 544 | 2.3px | 44.7px | 7 of 60 | 22.2s |
| `0.25` | 463 | 3.6px | 48.0px | 8 of 60 | 24.0s |

Two or three pixels of error is the same click, so precision is not what decides
this. What decides it is the jump in the p90 column: below `0.35` the model stops
being imprecise about the right control and starts naming the one beside it. At
`0.3` that row of icons is 14px apart in the picture it is shown, and 7 answers in
60 pointed at a neighbour — a wrong click, not a coarse one.

So the number to reason about is how far apart the things you click are, in the
picture the model sees. It held together at 16px of separation and broke at 14px,
which makes the rule roughly:

    scale ≥ 15px ÷ (spacing between adjacent controls, in screenshot pixels)

The default is `0.35`: the cheapest scale that misread nothing, taken on purpose
rather than the safe one below it. A fifth of the whole picture's tokens for two
pixels of error nobody clicks differently is worth having.

What that buys is worth knowing exactly, because `0.35` clears the rule above by
a pixel or two and by nothing more. It is a statement about one window. Icons
32px apart instead of 47px — a denser toolbar, a compact web UI — are inside the
cliff at `0.35` and want `0.5` or more. If a macro starts clicking the control
next to the one it was asked for, this setting is the first thing to raise, and
the [session log](05_Logs.md) has the screenshot the answer was read off to
confirm it with.

The wall-clock column is the other half of the price, and it runs the wrong way:
a model given a smaller picture answers it less readily, so `0.35` is 37% fewer
tokens than `0.5` for 74% more waiting. Setting this to save time rather than
tokens means going *up*, not down — `0.5` was the fastest scale measured.

Raise it to `1` for a slot that asks about something finer than a control: a
character in a line of text, which side of a hairline border. That is asking
about the pixels `0.35` threw away.

The second bill is worth checking before touching this setting at all, because it
can be much the larger of the two and no picture size touches it. A model that
reasons at length before answering spends thousands of tokens on a slot that
wants the word `true` — enough to make the picture a rounding error. The counts
are in [`logs/<session>/slots/<n>/psl.txt`](05_Logs.md), written as `N in, N out`:
a large `out` is a model to change, not a picture to cut down.

Every number above is one model reading one window, and the model is the half of
that Pob does not own. Redoing the measurement is not hard: the same slot at two
scales, and the `N in` count in the slot's log beside the answer it produced.


See also
--------

- [Logs](05_Logs.md) — the tree a run writes, and where every filled slot is kept
- [Macro PSL](03_Macro%20PSL.md) — `macro_default_delay` and `image_scale`, and the compiler a `:: … ::` slot is filled by
- [UI](02_UI.md) — the toolbar button that opens it
- [Pob Server](09_Server.md) — what `server` and `server_port` control
- [MCP Server](08_MCP.md) — what `mcp`, `mcp_port` and `mcp_host` control

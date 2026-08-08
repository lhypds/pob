
Settings
========

`~/.pob/settings.json` is this machine's settings file — the one the Settings
menu opens, and the one both the shell and the Go core read and edit. It is
created from the defaults below the first time Pob starts.

It sits at the root rather than inside an instance directory because it is how
the machine works, not what one instance is doing with it: the API key, the
model and the port are the same whichever instance is running. Pointing
[`~/.pob/INSTANCE`](05_Logs.md) at another id therefore starts Pob on a clean
`instruction.txt` and `macro.txt`, on a machine that is already set up.

A settings file from an older Pob — one per instance, inside its directory — is
moved up to the root on the next run, so a machine that was set up stays set
up. Only the first one moves: if several instances were configured separately,
the rest are left where they are to be copied across by hand.

| Key | Default | Description |
|-----|---------|-------------|
| `openai_api_key` | — | API key for the model provider |
| `base_url` | `https://api.openai.com/v1` | Base URL of the OpenAI-compatible API (e.g. `https://api.anthropic.com/v1` for Claude) |
| `model` | `gpt-4o` | Model name (e.g. `claude-sonnet-4-5`, `gemini-2.5-flash`) |
| `max_tokens` | `2000` | Maximum tokens in the response |
| `max_steps` | `12` | Maximum tool-execution steps per plan before pausing with a warning |
| `max_resumes` | `5` | Maximum step-resume attempts per plan before the plan is force-stopped and regenerated |
| `max_steplogs` | `10` | Maximum AI log iterations for a single step before it is automatically resumed |
| `editor` | `system` | Editor used to open config files (`system`, `vscode`, `zed`, `sublime_text`, `vim`) |
| `terminal` | `system` | Terminal used when editor is `vim` (`system`, `iterm2`) |
| `stop_hook` | — | Shell command to run when a session completes (e.g. `afplay /System/Library/Sounds/Morse.aiff`) |
| `server` | `true` | Run the [Pob Server](09_Server.md). `false` stops Pob accepting pointer and keyboard commands from the network, and takes the [Web UI](12_Web UI.md) down with it |
| `server_port` | `8033` | The port the [Pob Server](09_Server.md) is reached through. `POB_SERVER_PORT` overrides it |
Everything here is something you set, and it applies to the machine. Where the
window was last left is neither — it is written by the shell as `window_x`,
`window_y`, `window_width` and `window_height` in
[`instance.json`](05_Logs.md), per instance, with the rest of what an instance
records about itself. A settings file still holding those keys has them moved
across on the next run.

Example:

```json
{
  "model": "gpt-5.5",
  "max_tokens": 2000,
  "max_steps": 12,
  "max_resumes": 5,
  "max_steplogs": 10,
  "editor": "vscode",
  "stop_hook": "afplay /System/Library/Sounds/Morse.aiff",
  ...
}
```

Pointed at Claude — the API is OpenAI-compatible, so only `base_url` and
`model` change:

```json
{
  "openai_api_key": "",
  "base_url": "https://api.anthropic.com/v1",
  "model": "claude-opus-4-8",
  "max_steps": 50,
  "max_resumes": 5,
  "max_steplogs": 10,
  "macro_default_delay": 1000,
  "editor": "system",
  "terminal": "iterm2",
  "stop_hook": ""
}
```

Pointed at Gemini, through its OpenAI-compatible endpoint:

```json
{
  "openai_api_key": "",
  "base_url": "https://generativelanguage.googleapis.com/v1beta/openai",
  "model": "gemini-2.5-flash",
  "max_steps": 50,
  "max_resumes": 5,
  "max_steplogs": 10,
  "macro_default_delay": 1000,
  "editor": "system",
  "terminal": "iterm2",
  "stop_hook": ""
}
```


See also
--------

- [Logs](05_Logs.md) — where the per-instance copy lives
- [UI](02_UI.md) — the toolbar button that opens it
- [Pob Server](09_Server.md) — what `server` and `server_port` control

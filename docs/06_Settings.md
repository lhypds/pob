
Settings
========

`~/.pob/settings.json` is this machine's settings file — the one the Settings
menu opens, and the one both the shell and the Go core read and edit. It is
created from the defaults below the first time Pob starts.

It sits at the root rather than inside an instance directory because it is how
the machine works, not what one instance is doing with it: the API key, the
model and the port are the same whichever instance is running. Pointing
[`~/.pob/INSTANCE`](05_Logs.md) at another id therefore starts Pob on a clean
`macro.psl`, on a machine that is already set up.

A settings file from an older Pob — one per instance, inside its directory — is
moved up to the root on the next run, so a machine that was set up stays set
up. Only the first one moves: if several instances were configured separately,
the rest are left where they are to be copied across by hand.

| Key | Default | Description |
|-----|---------|-------------|
| `base_url` | `https://api.openai.com/v1` | Base URL of the OpenAI-compatible API (e.g. `https://api.anthropic.com/v1` for Claude) |
| `openai_api_key` | — | API key for the model provider |
| `model` | `gpt-5.6` | Model name (e.g. `claude-sonnet-4-5`, `gemini-2.5-flash`) |
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
  "model": "gpt-5.5",
  "macro_default_delay": 1000,
  "editor": "vscode",
  "stop_hook": "afplay /System/Library/Sounds/Morse.aiff",
  ...
}
```

Pointed at Claude — the API is OpenAI-compatible, so only `base_url` and
`model` change:

```json
{
  "base_url": "https://api.anthropic.com/v1",
  "openai_api_key": "",
  "model": "claude-opus-4-8",
  "macro_default_delay": 1000,
  "editor": "system",
  "terminal": "iterm2",
  "stop_hook": ""
}
```

Pointed at Gemini, through its OpenAI-compatible endpoint:

```json
{
  "base_url": "https://generativelanguage.googleapis.com/v1beta/openai",
  "openai_api_key": "",
  "model": "gemini-2.5-flash",
  "macro_default_delay": 1000,
  "editor": "system",
  "terminal": "iterm2",
  "stop_hook": ""
}
```


See also
--------

- [Logs](05_Logs.md) — where the per-instance copy lives
- [Macro PSL](03_Macro%20PSL.md) — `macro_default_delay`, and the model a `::…::` slot is filled by
- [UI](02_UI.md) — the toolbar button that opens it
- [Pob Server](09_Server.md) — what `server` and `server_port` control
- [MCP Server](08_MCP.md) — what `mcp`, `mcp_port` and `mcp_host` control

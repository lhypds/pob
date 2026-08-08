
Settings
========

`~/.pob/settings.json` is the template. The first time the instance starts it
copies the template to `~/.pob/logs/<instance>/settings.json`, and both the
shell and the Go core read and edit that copy from then on — it is what the
Settings menu opens. Edit the root file to change what a fresh instance
starts from. `instruction.txt` and `macro.txt` stay shared at `~/.pob`.

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
| `server` | `true` | Run the [Pob Server](09_Server.md). `false` stops Pob accepting pointer and keyboard commands from the network, and takes the [Web UI](12_WebUI.md) down with it |
| `server_port` | `8033` | The port the [Pob Server](09_Server.md) is reached through. `POB_SERVER_PORT` overrides it |
| `window_x` | — | Window position X (auto-saved) |
| `window_y` | — | Window position Y (auto-saved) |
| `window_width` | — | Window width (auto-saved) |
| `window_height` | — | Window height (auto-saved) |

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


See also
--------

- [Logs](05_Logs.md) — where the per-instance copy lives
- [UI](02_UI.md) — the toolbar button that opens it
- [Pob Server](09_Server.md) — what `server` and `server_port` control

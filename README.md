
Pob
===


Perception and Operation Bridge.


Purpose
-------

Make the AI see the deskotp application with screenshot, and operate it.  


Roadmap
-------

Phase 1, Make AI see its frontend development result.  
         To improve the frontend development automation.  

Phase 2, make the AI can operate the desktop application.  

Phase 3, make AI learn users operation and do it for the user with instructions, or repeat.  


`settings.json`
---------------

Settings are stored in `settings.json` in the project root.

| Key | Default | Description |
|-----|---------|-------------|
| `model` | `gpt-4o` | OpenAI model to use |
| `max_tokens` | `2000` | Maximum tokens in the response |
| `editor` | `system` | Editor used to open config files (`system`, `vscode`, `zed`, `sublime_text`, `vim`) |
| `window_x` | — | Window position X (auto-saved) |
| `window_y` | — | Window position Y (auto-saved) |
| `window_width` | — | Window width (auto-saved) |
| `window_height` | — | Window height (auto-saved) |

Example:

```json
{
  "model": "gpt-4o",
  "max_tokens": 2000,
  "editor": "vscode"
}
```

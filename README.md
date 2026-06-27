

![Pob Icon](https://github.com/user-attachments/assets/8c4be5c7-0b4a-4f86-abc1-d5f8a7e92314)


Pob
===


Perception and Operation Bridge.  


Purpose
-------

Pob is designed to connect AI with desktop applications.

It allows AI to:

- View the current desktop or application window
- Move and click the mouse
- Type text and press keys
- Record and replay operation macros
- Work with MCP-compatible AI clients


Roadmap
-------

Phase 1. Make AI see its frontend development result.  
         To improve the frontend development automation. (DONE)  
Phase 2. Make the AI can operate the desktop application. (DONE)  
Phase 3. Make AI learn users operation and do it for the user with instructions, or repeat. (IN PROGRESS)  


Features
--------

<img width="702" height="610" alt="image" src="https://github.com/user-attachments/assets/347585a4-5850-421b-92ee-f439cbe0a15f" />

Toolbar buttons (left to right):

| # | Button | Description |
|---|--------|-------------|
| 1 | Settings | Open the settings file |
| 2 | Logs | Open the logs folder |
| 3 | Instruction | Open the instruction file |
| 4 | Macro | Open the macro file |
| 5 | Record Macro | Start/stop macro recording; clears macro on start |
| 6 | Target | Hover to inspect pixel coordinates; click to copy `(x, y)` to clipboard |
| 7 | Crop | Drag to select a region; release to copy `(x, y, width, height)` to clipboard |
| 8 | Execute / Stop | Run the instruction or macro; stop if already running |
| 9 | Click-Through | Toggle whether clicks pass through the window to apps behind it |
| 10 | Lock | Lock the window to prevent moving or resizing |
| 11 | Clear | Clear instruction, macro, logs, or all |

* Target and Crop are helper functions for when you hard to describe the GUI element.  


Functions
---------

These are the tools the AI can call during a session:  

| Function | Parameters | Description |
|----------|------------|-------------|
| `move(dx, dy)` | `dx`: number, `dy`: number | Nudge the cursor by a relative pixel offset. Positive `dx` = right, positive `dy` = down. Returns a new screenshot showing the updated cursor position. |
| `click()` | — | Left-click at the current cursor position. |
| `rightClick()` | — | Right-click at the current cursor position. |
| `doubleClick()` | — | Double-click at the current cursor position. |
| `drag(dx, dy)` | `dx`: number, `dy`: number | Drag from the current cursor position by `(dx, dy)` pixels. Cursor ends at the new position. |
| `scroll(dx, dy)` | `dx`: number, `dy`: number | Scroll at the current cursor position. `dy > 0` = down, `dy < 0` = up, `dx > 0` = right. |
| `typeText(text)` | `text`: string | Type text at the current keyboard focus. |
| `keyPress(key)` | `key`: string | Press a special key. Supported: `return`, `tab`, `space`, `delete`, `escape`, `left`, `right`, `up`, `down`, `home`, `end`, `pageup`, `pagedown`, `f1`–`f12`, `cmd+a/c/v/x/z/w/s/t/r`. |
| `sleep(milliseconds)` | `milliseconds`: number | Pause execution for the given number of milliseconds. |
| `take_screenshot(crop_x?, crop_y?, crop_width?, crop_height?)` | All optional: `crop_x`, `crop_y`, `crop_width`, `crop_height`: number | Capture a fresh screenshot. When all four crop parameters are provided, the image is cropped to that region (x, y, width, height in screenshot pixels). Saved to `logs/<sessionId>/screenshots/<unixtime>.png`. |

All coordinates are in screenshot pixel space (origin = top-left, x increases right, y increases down).  
These functions are also available in macros (see Macro below).  


Macro
-----

A macro is a recorded or hand-written sequence of actions stored in `macro.txt`. Each line is one function call using the same syntax as the AI tools above.

Example `macro.txt`:

```
move(100, 200)
click()
sleep(500)
typeText("hello")
keyPress("return")
```

Use the record button (⏺) in the toolbar to record actions during an AI session — they are appended to `macro.txt` automatically. Use the play button (▶) to run the macro directly without the AI.


MCP Server
----------

Install dependency:

Register with Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`):

When Pob is running with `start_mcp: true`, the server is already up on SSE — point Claude Desktop at it directly:

```json
{
  "mcpServers": {
    "pob": {
      "url": "http://127.0.0.1:8032/sse"
    }
  }
}
```

Alternatively, let Claude Desktop manage the process itself (stdio mode):

```json
{
  "mcpServers": {
    "pob": {
      "command": "python3",
      "args": ["/path/to/pob/mcp/pob_mcp_server.py"]
    }
  }
}
```

MCP tools:

| Tool | Parameters | Description |
|------|------------|-------------|
| `take_screenshot` | `crop_x?`, `crop_y?`, `crop_width?`, `crop_height?`: integer | Capture the primary display and return a PNG image. When all four crop parameters are provided, only that region is captured. Coordinates are in screen points (logical pixels), origin top-left. |

The server communicates over stdio and requires macOS (uses the built-in `screencapture` command).


`settings.json`
---------------

Settings are stored in `settings.json` in the project root.

| Key | Default | Description |
|-----|---------|-------------|
| `model` | `gpt-4o` | OpenAI model to use |
| `max_tokens` | `2000` | Maximum tokens in the response |
| `max_steps` | `12` | Maximum tool-execution steps before the run is stopped with a warning |
| `editor` | `system` | Editor used to open config files (`system`, `vscode`, `zed`, `sublime_text`, `vim`) |
| `terminal` | `system` | Terminal used when editor is `vim` (`system`, `iterm2`) |
| `stop_hook` | — | Shell command to run when a session completes (e.g. `afplay /System/Library/Sounds/Morse.aiff`) |
| `start_mcp` | `true` | Automatically start the MCP server (SSE on `http://127.0.0.1:8032`) when Pob launches |
| `window_x` | — | Window position X (auto-saved) |
| `window_y` | — | Window position Y (auto-saved) |
| `window_width` | — | Window width (auto-saved) |
| `window_height` | — | Window height (auto-saved) |

Example:

```json
{
  "model": "gpt-4o",
  "max_tokens": 2000,
  "max_steps": 12,
  "editor": "vscode",
  "stop_hook": "afplay /System/Library/Sounds/Morse.aiff"
}
```


Release
-------

Use `build.sh` to build the app to `macos_app/Pob.app`.  
Update `VERSION`.  
Then use `release.sh` to release to GitHub.  

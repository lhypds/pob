

![Pob Icon](https://github.com/user-attachments/assets/8c4be5c7-0b4a-4f86-abc1-d5f8a7e92314)


Pob
===


Perception & Operation Bridge.  


Purpose
-------

Pob is designed to connect AI with desktop applications.

It allows AI to:

- View the current desktop or application window
- Move and click the mouse
- Type text and press keys
- Record and replay operation macros
- Work with MCP-compatible AI clients


Architecture
------------

Pob is split into a platform-independent brain and a native shell:

```
core/    The brain (Go, zero dependencies). Agent loop (plan → execute →
         verify), OpenAI-compatible LLM client, session logs, macro engine,
         and the MCP SSE server. Compiled to a single binary: pob-core.

macos/   The hands and eyes (Swift). Overlay window UI, screenshot capture,
         virtual cursor, mouse/keyboard event injection, and the permission
         surface (Screen Recording / Accessibility).

linux-x11/  The same hands and eyes for Linux/Xorg (C + GTK 3). Identical UI
            and features; screenshots via XGetImage, input via XTest.
            See linux-x11/README.md.

win/     The same hands and eyes for Windows (C# / WPF). Identical UI and
         features; screenshots via GDI, input via SendInput.
         See win/README.md.
```

The shell spawns `pob-core` as a child process and the two talk over
stdin/stdout with line-delimited JSON-RPC:

- Shell → core: `run.instruction`, `run.macro`, `run.stop`, `recording.changed`
- Core → shell: `screenshot.capture`, `cursor.move`, `mouse.click`,
  `keyboard.type`, `ui.confirmMaxStep`, … and `session.state` notifications

All coordinates crossing the boundary are screenshot pixels; the shell owns
the conversion to real screen positions. Porting to a new platform means
reimplementing only the shell — the brain is shared.


Roadmap
-------

Phase 1. Make AI see its frontend development result.  
         To improve the frontend development automation. (DONE)  
Phase 2. Make the AI can operate the desktop application. (DONE)  
Phase 3. Make AI learn users operation and do it for the user with instructions, or repeat. (IN PROGRESS)  


Test
----

OpenAI
gpt-5.5, works

Claude
claude-opus-4-8, works, but the mouse fly outside of the window sometimes.

Google
gemini-2.5-flash, not working


Logs
----

Structure  

```
logs/  
    +--- <instance>/                              one directory per app launch (multi-instance support).
         +--- screenshots/                        screenshots taken with the toolbar Screenshot button.
         +--- settings.json                       the per-instance settings file (copied from the root `settings.json`).
         +--- instance.json                       instance start/end times, etc.

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

`<instance>` is a unique instance ID named as a unixtime, created when the app starts. Each running
app instance writes to its own directory, so multiple instances can run side by side without their
logs colliding (if two instances start within the same second, the later one bumps its ID by one).  
`<session>` is a unique session ID named as a unixtime.  
`<plan>` is a unique plan ID named as a unixtime.  
`<step>` is the sequence number of the step (e.g. `1`, `2`, `3`).  
`<log>` is a unique log ID named as a unixtime.  


Features
--------

<img width="839" height="762" alt="image" src="https://github.com/user-attachments/assets/e74edfe9-7bd7-40b1-a403-d0391477d2d8" />

Toolbar buttons (left to right):

| # | Button | Description |
|---|--------|-------------|
| 1 | Settings | Open the settings file |
| 2 | Logs | Open the logs folder |
| 3 | App Log | Open the app log file |
| 4 | Instruction | Open the instruction file |
| 5 | Macro | Open the macro file |
| 6 | Record Macro | Start/stop macro recording; clears macro on start |
| 7 | Execute / Stop | Run the instruction or macro; stop if already running |
| 8 | Target | Hover to inspect pixel coordinates; click to copy `(x, y)` to clipboard |
| 9 | Crop | Drag to select a region; release to copy `(x, y, width, height)` to clipboard |
| 10 | Screenshot | Capture the content area to `logs/<instance>/screenshots/`; while recording, also appends `take_screenshot()` to the macro |
| 11 | Click-Through | Toggle whether clicks pass through the window to apps behind it |
| 12 | Lock | Lock the window to prevent moving or resizing |
| 13 | Clear | Clear instruction, macro, logs, or all |

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
| `take_screenshot(crop_x?, crop_y?, crop_width?, crop_height?)` | All optional: `crop_x`, `crop_y`, `crop_width`, `crop_height`: number | Capture a fresh screenshot. When all four crop parameters are provided, the image is cropped to that region (x, y, width, height in screenshot pixels). Saved to `logs/<instanceId>/<sessionId>/screenshots/<unixtime>.png`. |

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

The MCP server is built into `pob-core` and starts automatically with the app
when `start_mcp: true` (SSE transport, port `8032` by default).

Register with Claude Desktop (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "pob": {
      "url": "http://127.0.0.1:8032/sse"
    }
  }
}
```

MCP tools:

| Tool | Parameters | Description |
|------|------------|-------------|
| `take_screenshot` | `crop_x?`, `crop_y?`, `crop_width?`, `crop_height?`: integer | Capture the Pob window content area and return a PNG image. When all four crop parameters are provided, only that region is captured. Coordinates are in screen points (logical pixels), origin top-left. |


Settings
--------

The `settings.json` in the project root is the shared template. When an
instance starts it copies the template to its own `logs/<instance>/settings.json`,
and both the shell and the Go core read and edit that per-instance copy from
then on — so multiple instances can run with independent settings (the
Settings menu opens the instance's copy). Edit the root file to change the
defaults new instances start with. `instruction.txt` and `macro.txt` stay
shared at the root.

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
| `start_mcp` | `true` | Automatically start the MCP server (SSE on `http://127.0.0.1:8032`) when Pob launches |
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


Development
-----------

Requirements: Go, plus the platform shell's toolchain — Xcode Command Line
Tools (Swift) on macOS, or GTK 3 development libraries on Linux (see
[linux-x11/README.md](linux-x11/README.md)).

```
./setup.sh      # select your OS (recorded in the SYSTEM file), then
                # check toolchains and build core + that OS shell
./start.sh      # build and run in the foreground
./restart.sh    # rebuild and relaunch in the background (logs to app.log)
./stop.sh       # stop the app and the core process
./build.sh      # release build (macOS: Pob.app, Linux: dist tarball)
```

The root scripts are dispatchers: `setup.sh` writes your choice (`macos` or
`linux-x11`) to the `SYSTEM` file, and the others read it and forward to the
matching folder's script (`macos/*.sh` or `linux-x11/*.sh`), which can also
be run directly.


Release
-------

Update `VERSION`, then run `release.sh`. What it builds follows the
`SYSTEM` file:

- `SYSTEM=macos` (requires Docker running) — builds all shells:
  - `Pob-<version>-macos.zip` — the app bundle from `macos/build.sh`
    (`pob-core` embedded)
  - `Pob-<version>-linux-amd64.zip` and `Pob-<version>-linux-arm64.zip` —
    `pob` + `pob-core` side by side, built by `linux-x11/build_docker.sh`
    (Go core cross-compiled on the host, GTK shell compiled in a Debian
    container; override the list with `LINUX_ARCHS="amd64 arm64"`)
  - `Pob-<version>-windows-amd64.zip` and `Pob-<version>-windows-arm64.zip` —
    `Pob.exe` (self-contained) + `pob-core.exe` side by side, built by
    `win/build_docker.sh` (Go core cross-compiled on the host, WPF shell
    compiled in the .NET SDK container; override the list with
    `WIN_ARCHS="amd64 arm64"`)
- `SYSTEM=linux-*` — builds `Pob-<version>-linux-<arch>.zip` natively via
  `linux-x11/build.sh` for the host architecture only



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

The same bridge works for people, not only for AI: every instance serves a web
page you can open on your phone, and a desktop app with a full-size keyboard
and a trackpad — see [Web UI](#web-ui) and [Pob Keyboard](#pob-keyboard).


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

webui/   The remote control (Go, zero dependencies), compiled into pob-core
         and started with the app: an HTTP server on one shared port serving
         index.html — a text field, a keyboard-mirror button and a trackpad —
         routing /<instance> to the process that owns it.

keyboard/  Pob Keyboard (Go + Fyne), a separate desktop app: a full-size
           on-screen keyboard and a trackpad in their own window, driving an
           instance through the same web UI API. Run it with ./keyboard.sh.
```

The shell spawns `pob-core` as a child process and the two talk over
stdin/stdout with line-delimited JSON-RPC:

- Shell → core: `run.instruction`, `run.macro`, `run.stop`, `recording.changed`
- Core → shell: `screenshot.capture`, `cursor.move`, `mouse.click`,
  `keyboard.type`, `ui.confirmMaxStep`, … and `session.state` notifications

Everything that drives the machine goes through those same calls — the agent
loop, the MCP server, and the web UI — so a tap on a phone and a tool call
from a model take exactly the same path.

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
claude-opus-4-8, works.

Google
gemini-2.5-flash, not working


Logs
----

Structure  

```
~/.pob/logs/  
    +--- pb-<uid>/                                one directory per app launch (multi-instance support).
         +--- screenshots/                        screenshots taken with the toolbar Screenshot button.
         +--- settings.json                       the per-instance settings file (copied from the root `settings.json`).
         +--- instance.json                       instance start/end times, etc.
         +--- control.json                        written while the instance runs; advertises the control API port used by the `pob` CLI.
         +--- .lock                               held locked while the instance runs; Clear Logs skips locked (running) instances.

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

`<instance>` is a unique instance ID of the form `pb-<4 hex>` (the last two bytes of a fresh UID in
lowercase hex), created when the app starts and shown in the toolbar beside the window buttons — so
the ID on screen names the directory to look in. Each running app instance writes to its own
directory, so multiple instances can run side by side without their logs colliding (if the drawn ID
is already taken, another one is drawn).  
`<session>` is a unique session ID named as a unixtime.  
`<plan>` is a unique plan ID named as a unixtime.  
`<step>` is the sequence number of the step (e.g. `1`, `2`, `3`).  
`<log>` is a unique log ID named as a unixtime.  


Features
--------

<img width="839" height="762" alt="image" src="https://github.com/user-attachments/assets/e74edfe9-7bd7-40b1-a403-d0391477d2d8" />

The toolbar shows this instance's ID (`pb-<uid>`) at the left, beside the window buttons; the action
buttons are packed against the right edge, in this order (left to right):

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
| 11 | Click-Through | Toggle whether clicks pass through the window to apps behind it (on by default) |
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
| `keyPress(key)` | `key`: string | Press a key, optionally with `+`-joined modifiers in front of it (see [Key names](#key-names)) — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. |
| `sleep(milliseconds)` | `milliseconds`: number | Pause execution for the given number of milliseconds. |
| `take_screenshot(crop_x?, crop_y?, crop_width?, crop_height?)` | All optional: `crop_x`, `crop_y`, `crop_width`, `crop_height`: number | Capture a fresh screenshot. When all four crop parameters are provided, the image is cropped to that region (x, y, width, height in screenshot pixels). Saved to `logs/<instanceId>/<sessionId>/screenshots/<unixtime>.png`. |

All coordinates are in screenshot pixel space (origin = top-left, x increases right, y increases down).  
The cursor is held inside the Pob window: a move that would take it past an edge stops at the edge, since
everything it addresses — what the screenshots show, what the clicks are aimed through — is inside that window.  
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

The MCP server is built into `pob-core` (SSE transport). It does not start
with the app — start it from the CLI. The target is the only running
instance, or an explicit `--instance` when several are running. The port
defaults to `8032`; pass a different one after `start` when running several
instances:

```
pob mcp start [port]
pob --instance <id> mcp start [port]
```

`mcp start` also registers the server (as `pob`) in the user settings of any
installed agent CLIs — Claude Code (`claude`) and Gemini CLI (`gemini`) — and
`mcp stop` removes those registrations again, so no manual setup is needed
there.

For other clients, register the printed URL manually. Claude Desktop
(`~/Library/Application Support/Claude/claude_desktop_config.json`):

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

All coordinates are **screenshot pixels**, origin at the top-left of the image
returned by `take_screenshot` — the client never deals with screen-level
positions. `take_screenshot` reports the image size alongside the PNG, so the
model can read a target's coordinates off the image and pass them straight to
the `*_to` / `move_and_*` tools. Every action returns the resulting cursor
position.

Perception:

| Tool | Parameters | Description |
|------|------------|-------------|
| `take_screenshot` | `crop_x?`, `crop_y?`, `crop_width?`, `crop_height?`: integer, `with_cursor?`: boolean | Capture the Pob window content area and return a PNG image plus its pixel dimensions. When all four crop parameters are provided, only that region is captured (coordinates read off a crop need the crop offset added back). `with_cursor` draws the virtual cursor into the image. |
| `get_cursor_position` | — | Current virtual cursor position, without moving or clicking. |

Pointer:

| Tool | Parameters | Description |
|------|------------|-------------|
| `reset_cursor` | — | Return the cursor to its home position. |
| `move_cursor` | `dx`, `dy`: number | Nudge the cursor by a relative offset. |
| `move_cursor_to` | `x`, `y`: number | Move the cursor to an absolute position. |
| `click` / `right_click` / `double_click` | — | Click at the current cursor position. |
| `move_and_click` | `x`, `y`: number | Move to an absolute position and left-click there, in one step. |
| `move_and_right_click` | `x`, `y`: number | Move and right-click — e.g. to open a context menu. |
| `move_and_double_click` | `x`, `y`: number | Move and double-click — e.g. to open an item. |
| `drag` | `dx`, `dy`: number | Drag from the cursor position by a relative offset. |
| `drag_to` | `x`, `y`: number | Drag from the cursor position to an absolute position. |
| `scroll` | `dx`, `dy`: number | Scroll at the cursor position. `dy > 0` scrolls down, `dx > 0` scrolls right. |
| `move_and_scroll` | `x`, `y`, `dx`, `dy`: number | Move to an absolute position and scroll there, to target one pane. |

Keyboard and timing:

| Tool | Parameters | Description |
|------|------------|-------------|
| `type_text` | `text`: string | Type text at the current keyboard focus (click the field first). |
| `key_press` | `key`: string | Press a key or shortcut — e.g. `return`, `escape`, `cmd+v`, `ctrl+shift+t`. See [Key names](#key-names). |
| `wait` | `milliseconds`: number | Pause to let the UI settle. Capped at 10000 ms. |


Key names
---------

`keyPress` / `key_press` takes one key, optionally preceded by `+`-joined
modifiers: `ctrl+alt+shift+f5`. A name is a *position* on the keyboard rather
than a character, so the machine's own layout decides what it produces — which
is what lets the [Web UI](#web-ui) and [Pob Keyboard](#pob-keyboard) forward
real keypresses verbatim.

| Modifier | Meaning |
|----------|---------|
| `cmd` | Command on macOS, Ctrl on Linux and Windows — the ordinary-shortcut modifier, so `cmd+c` copies everywhere |
| `ctrl` | Control on every platform |
| `alt` | Option / Alt |
| `shift` | Shift |
| `gui` | The key beside the space bar itself: Command / Windows / Super (aliases: `win`, `super`, `meta`) |

Keys: `a`–`z`, `0`–`9`, `return`, `tab`, `space`, `backspace`,
`forwarddelete`, `escape`, `insert`, `left`, `right`, `up`, `down`, `home`,
`end`, `pageup`, `pagedown`, `capslock`, `printscreen`, `scrolllock`, `pause`,
`menu`, `f1`–`f24`, `minus`, `equals`, `leftbracket`, `rightbracket`,
`backslash`, `semicolon`, `quote`, `grave`, `comma`, `period`, `slash`.

`delete` is an alias for `backspace`, as it always was; the key that deletes
forwards is `forwarddelete`. macOS has no `menu` key, and takes `printscreen`,
`scrolllock` and `pause` to mean F13–F15 — the same keycaps in the same place
on an Apple board.


Web UI
------

Every instance serves a remote control page, started with the app. One port
serves the whole machine and the path says which instance:

```
http://192.168.1.40:8033/pb-a703
```

`pob status` prints the address — one line per network the machine is on. The
instance ID is the one in the toolbar. With a single instance running the path
can be left off, and the root leads to it.

The port is yours to set: `webui_port` in `settings.json`, or `POB_WEBUI_PORT`
in the environment. It is the same for every instance, so the address can be
typed from memory instead of looked up per window.

The page is three controls:

- a **text field** — type a line, press ↵, and it is typed on the machine
- a **keyboard mirror** button — while it is on, keys pressed here go straight
  through, shortcuts included. On a phone the soft keyboard is mirrored
  instead, so autocorrect and swipe typing still work.
- a **trackpad** — drag to move the pointer, tap to click, two-finger tap to
  right-click, two fingers to scroll, double-tap-and-hold to drag

**The page is served on every network interface**, so anyone on the same
network who knows the address can move this machine's pointer and type on it.
That is the point of it — but it is also why `"webui": false` in
`settings.json` turns it off, per instance.


### How one port serves every instance

Only one process can hold a port, and every window is its own instance with
its own `pob-core`. So each instance binds a private loopback port and
publishes it in `logs/<instance>/webui.json`, and whichever instance gets the
shared port becomes the front door: it serves its own page directly and hands
the others' requests to the process that owns them. Close that window and
another instance takes the port within a few seconds — `pob status` shows
`holds_port` if you ever need to know which one has it.

With several instances running, the root lists them.


### API

The page POSTs commands to its own path as `text/plain`, which makes the same
thing scriptable:

```
curl -X POST --data 'typing=hello'          http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'keycode=CTRL+c,CTRL+v' http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'mouse=MOVE(40,10)'     http://192.168.1.40:8033/pb-a703/
curl -X POST --data 'mouse=CLICK(0,0)'      http://192.168.1.40:8033/pb-a703/
```

The protocol is the pico-hid board's, so its clients work against Pob
unchanged: `typing=<text>`, `keycode=<chord>` (`,` separates keys pressed in
turn, `+` joins keys held together, using the HID names in
[Key names](#key-names)), `mouse=ACTION(x,y)` with `MOVE`, `CLICK`,
`RIGHT_CLICK`, `DOUBLE_CLICK`, `PRESS`, `RELEASE` and `SCROLL`, and an optional
`seq=<token>&` prefix that makes a retry safe to send twice. `consumer=` —
media and brightness keys — is accepted and ignored: the shells post plain key
events and have nowhere to put a consumer-control usage.

A command posted to the bare root is served where it lands rather than
redirected, so a client that doesn't follow redirects — or one that would turn
a redirected POST into a GET, which is most of them — still gets its keystroke
through.


Pob Keyboard
------------

A desktop client for the same API: a full-size 104-key board and a trackpad in
one window, for driving a machine running Pob from another computer.

```
./keyboard.sh
./keyboard.sh -url http://192.168.1.40:8033/pb-3f9a
```

With no address it opens Settings… straight away, laid out as the address
itself — machine, port, instance ID — and remembered between runs. Pasting the
whole line `pob status` prints into the first field fills all three.

Keys pressed on your real keyboard are forwarded too while the window has
focus, and light up the matching keycap. Modifier keys latch — click once for
the next key only, twice to lock — so a shortcut can be built without holding
anything down. The **Target** setting (Windows / macOS) doesn't change what
gets sent, only how the keys either side of the space bar are labelled and
ordered.

Building it needs a C compiler (the UI draws through OpenGL): `xcode-select
--install` on macOS, `sudo apt install gcc libgl1-mesa-dev xorg-dev` on
Debian/Ubuntu.


CLI
---

The `pob` command controls and inspects instances from the terminal.

On macOS the packaged app ships the CLI inside the bundle
(`Pob.app/Contents/Helpers/pob`) — use **Pob → Install 'pob' Command…** in the
app menu to symlink it at `/usr/local/bin/pob` (asks for an admin password
when needed; the same menu item uninstalls it again). The dev scripts also
build it to `core/bin/pob` next to `pob-core` (add that folder to your `PATH`,
or call it by path).

All project files (`settings.json`, `instruction.txt`, `macro.txt`, `logs/`)
live in `~/.pob`, created on first use and shared by the app and the CLI.

Every running instance serves a small control API on an ephemeral localhost
port, advertised in `~/.pob/logs/<instance>/control.json`; the CLI scans that
directory to discover instances and talks to that API. Log and session
inspection reads the log tree directly, so it also works for stopped
instances.

```
Usage: pob [flags] [command] [args]

Flags:
  --instance <id>    Target instance (default: the only running one)
  --session <id>     Target session; with no command, shows its details
```

| Command | Description |
|---------|-------------|
| *(none)* | List running instances; with `--instance` show that instance; with `--session` show that session |
| `list [--all]` | List running instances with status, times and session count; `--all` includes stopped ones |
| `launch` | Start a new app instance and print its ID (alias: `new`). The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the shell build outputs for `core/bin/pob` |
| `status` | Live status of the target instance (executing, recording, model, MCP, web UI address) |
| `sessions` | List the target instance's sessions with duration and token usage |
| `start` | Execute `instruction.txt` (same as the toolbar Execute button) |
| `run <text...>` | Replace `instruction.txt` with `<text>`, then execute it |
| `macro` | Execute `macro.txt` |
| `stop` | Stop the running session |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Start the MCP server and print its info (port defaults to `8032`). Registers the server in the user settings of installed agent CLIs (`claude`, `gemini`) |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `version` | Print the Pob version |

With no `--instance` the commands target the only running instance; when
several are running the choice must be explicit, so an instruction never
lands on an instance you didn't pick. `pob launch` starts a fresh instance
and prints its ID.

Examples:

```
pob                                      # what's running?
pob launch                               # start a new app instance
pob run "click Save and close the dialog"
pob --instance 1752712345 start          # run instruction.txt on that instance
pob --instance 1752712345 --session 1752712400   # session detail: plans, steps, usage
pob --instance 1752712345 mcp start      # start MCP and print the connection info
```


Settings
--------

`~/.pob/settings.json` is the shared template. When an instance starts it
copies the template to its own `~/.pob/logs/<instance>/settings.json`, and
both the shell and the Go core read and edit that per-instance copy from
then on — so multiple instances can run with independent settings (the
Settings menu opens the instance's copy). Edit the root file to change the
defaults new instances start with. `instruction.txt` and `macro.txt` stay
shared at `~/.pob`.

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
| `webui` | `true` | Serve the [Web UI](#web-ui) remote control page. `false` stops this instance accepting pointer and keyboard commands from the network |
| `webui_port` | `8033` | The one port every instance on this machine is reached through; the instance is named in the path. `POB_WEBUI_PORT` overrides it |
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
./keyboard.sh   # build and run Pob Keyboard (its own Go module, not built
                # by the scripts above)
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

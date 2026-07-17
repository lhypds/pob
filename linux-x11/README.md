
Pob — Linux/X11 shell
=====================

A feature-for-feature port of the macOS shell (`macos/`) built with C, GTK 3
and Xlib/XTest. It spawns the shared Go core (`core/`, `pob-core`) and speaks
the same line-delimited JSON-RPC protocol over stdin/stdout; the core owns
the agent loop, LLM calls, logs, macros and the MCP server, while this shell
provides perception (screenshots) and operation (mouse/keyboard) plus the UI.

The UI mirrors macOS exactly — same toolbar buttons in the same order, same
colors (translucent gray overlay, #007AFF accent, #FF3B30 record), same
targeting / crop / click-through / lock / record / execute features — except
that window controls (close/minimize/maximize) follow your desktop
environment's native Unix button layout.


Requirements
------------

- A real X11 session (Xorg). On Wayland the app runs under XWayland but
  cannot see or control native Wayland windows — use an Xorg session.
- A running compositor (GNOME/KDE/Xfce defaults are fine); without one the
  overlay cannot be transparent. On Raspberry Pi OS (PIXEL/openbox has no
  compositor by default):

  ```
  sudo apt install xcompmgr && xcompmgr &          # simplest
  # or:
  sudo apt install picom && picom --backend xrender -b
  ```

  Make it permanent with `echo '@xcompmgr' >> ~/.config/lxsession/LXDE-pi/autostart`.
  The app self-diagnoses: when transparency can't work, the content area
  shows what is missing (compositor / ARGB visual), and `app.log` records
  `Window realized: visual depth=…, composited=…`.
- Build dependencies:

```
Debian/Ubuntu: sudo apt install build-essential libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev
Fedora:        sudo dnf install gcc make gtk3-devel json-glib-devel libX11-devel libXtst-devel
Arch:          sudo pacman -S base-devel gtk3 json-glib libx11 libxtst
```

- Go (for the shared core).


Run from a release zip
----------------------

Download the `Pob-<version>-linux-<arch>.zip` matching your CPU
(`uname -m`: `x86_64` → amd64, `aarch64` → arm64), then:

```
unzip Pob-<version>-linux-<arch>.zip
cd Pob
./pob
```

`pob` starts the bundled `pob-core` automatically. Runtime dependencies are
just the GTK 3 libraries, preinstalled on mainstream desktops
(`sudo apt install libgtk-3-0 libjson-glib-1.0-0 libxtst6` if missing).

On first run the working files (`settings.json`, `instruction.txt`,
`macro.txt`, `logs/`, `app.log`) are created in `~/.pob/` —
set `openai_api_key` in `settings.json` there.


Run from source
---------------

```
git clone <repo> && cd pob
./setup.sh              # select 2) Linux / X11 — records the choice in SYSTEM,
                        # checks deps and builds core + shell
vim settings.json       # set openai_api_key
./start.sh              # build and run in the foreground
./restart.sh            # rebuild and (re)start in the background (logs to app.log)
./stop.sh
```

After `./setup.sh`, the root scripts dispatch to this folder automatically —
the workflow is identical to macOS. The scripts here can also be run
directly:

```
./linux-x11/setup.sh         # check deps, build core + shell
./linux-x11/start.sh         # build and run in the foreground
./linux-x11/restart.sh       # rebuild and (re)start in the background
./linux-x11/stop.sh
./linux-x11/build.sh         # release build — native on Linux; on macOS
                             # (SYSTEM=macos) it delegates to build_docker.sh
./linux-x11/build_docker.sh  # release build from any Docker host (used by release.sh)
```

Both build scripts produce `linux-x11/dist/Pob/` (`pob` + `pob-core` side by
side) and `Pob-<version>-linux-<arch>.zip` at the project root.

Project files (`settings.json`, `instruction.txt`, `macro.txt`, `logs/`)
always live in `~/.pob` (same rule as macOS).


Source layout
-------------

Mirrors `macos/Sources`:

```
src/
  main.c                 window, headerbar toolbar, dialogs, click-through, lock
                         (≈ AppDelegate.swift + toolbar half of ContentView.swift)
  content_view.c         overlay: gray background, targeting, crop, virtual
                         cursor animation, screenshot flash (≈ ContentView.swift)
  core_bridge.c          pob-core process + JSON-RPC stdio (≈ CoreBridge.swift)
  mouse_service.c        virtual cursor + XTest mouse/keyboard (≈ MouseService.swift)
  screenshot_service.c   root-window capture + cursor compositing (≈ ScreenshotService.swift)
  settings_service.c     settings.json, editor launching, clears (≈ SettingsService.swift)
  app_logger.c           app.log (≈ AppLogger.swift)
```


Platform notes (differences from macOS)
---------------------------------------

These are forced by X11; behavior is otherwise identical:

- **Screenshots hide the window for one frame.** macOS captures "everything
  below the overlay window" directly; X11 has no equivalent, so the window
  turns fully transparent for ~80 ms while the root window is grabbed. You
  may notice a blink each time the agent looks at the screen.
- **The real pointer moves during actions.** macOS can post events at a
  position while freezing the visible cursor; XTest cannot, so the pointer
  jumps to the click position and is restored right afterwards.
- **Click-through during execution.** While the agent is executing, the
  content area always passes clicks through to the window below (the toolbar
  stays clickable) — synthesized clicks must reach the target app. macOS
  toggles this per-action; the effect is the same.
- **`cmd+<key>` maps to `Ctrl+<key>`**, the Unix equivalent.
- **Text is typed via XTest** with temporary keysym remapping for characters
  not on your layout (CJK etc.). The target app must accept synthetic key
  events; apps relying purely on input methods may differ from the macOS
  AX-insertion behavior.
- **Window lock** blocks resizing and header-bar dragging; a window manager
  that moves windows via Alt+drag can still move it.

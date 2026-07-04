
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
  overlay cannot be transparent.
- Build dependencies:

```
Debian/Ubuntu: sudo apt install build-essential libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev
Fedora:        sudo dnf install gcc make gtk3-devel json-glib-devel libX11-devel libXtst-devel
Arch:          sudo pacman -S base-devel gtk3 json-glib libx11 libxtst
```

- Go (for the shared core).


Build & run
-----------

```
./linux-x11/setup.sh         # check deps, build core + shell
./linux-x11/start.sh         # build and run in the foreground
./linux-x11/restart.sh       # rebuild and (re)start in the background
./linux-x11/stop.sh
./linux-x11/build.sh         # release build (native, on a Linux machine)
./linux-x11/build-docker.sh  # release build from any Docker host (used by release.sh)
```

Both build scripts produce `linux-x11/dist/Pob/` (`pob` + `pob-core` side by
side) and `Pob-<version>-linux-<arch>.zip` at the project root.

Run from the project root so the shell picks up `settings.json`,
`instruction.txt`, `macro.txt` and `logs/` there (same rule as macOS). When
launched elsewhere it falls back to `~/.local/share/Pob`.


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

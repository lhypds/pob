
Pob — Windows shell
===================

A feature-for-feature port of the macOS shell (`macos/`) built with C# and
WPF. It spawns the shared Go core (`core/`, `pob-core.exe`) and speaks the
same line-delimited JSON-RPC protocol over stdin/stdout; the core owns the
agent loop, LLM calls, logs, macros and the MCP server, while this shell
provides perception (screenshots) and operation (mouse/keyboard) plus the UI.

The UI mirrors macOS exactly — same toolbar buttons in the same order, same
colors (translucent gray overlay, #007AFF accent, #FF3B30 record), same
targeting / crop / click-through / lock / record / execute features — except
that icons come from the system Segoe MDL2 set and window controls
(minimize/maximize/close) sit on the right, the native Windows layout.

One Windows-specific detail: click-through (`WS_EX_TRANSPARENT`) is a
per-window flag, so the shell is a toolbar window with a content overlay
window glued below it — visually one window, but the toolbar stays clickable
while the content passes clicks through (the Stop button must remain usable
while the agent is executing).


Requirements
------------

- Windows 10 or later (per-monitor DPI aware, Segoe MDL2 Assets preinstalled).
- To run a release zip: nothing else — `Pob.exe` is self-contained.
- To build from source:
  - Go (for the shared core) — `winget install GoLang.Go`
  - .NET 8 SDK — `winget install Microsoft.DotNet.SDK.8`


Run from a release zip
----------------------

Download `Pob-<version>-windows-<arch>.zip` (`amd64` for Intel/AMD,
`arm64` for ARM), then unzip and run `Pob\Pob.exe`.

`Pob.exe` starts the bundled `pob-core.exe` automatically. On first run the
working files (`settings.json`, `instruction.txt`, `macro.txt`, `logs/`,
`app.log`) are created in `%USERPROFILE%\.pob\` — set `openai_api_key` in
`settings.json` there.


Run from source (on Windows)
----------------------------

```
git clone <repo> ; cd pob
powershell -ExecutionPolicy Bypass -File win\setup.ps1   # check deps, build core + shell
notepad settings.json                                    # set openai_api_key
powershell -ExecutionPolicy Bypass -File win\start.ps1   # build and run in the foreground
powershell -ExecutionPolicy Bypass -File win\restart.ps1 # rebuild and (re)start in the background
powershell -ExecutionPolicy Bypass -File win\stop.ps1    # stop the app and the core process
powershell -ExecutionPolicy Bypass -File win\build.ps1   # release build (dist\Pob + zip)
```

(The root `setup.sh` / `start.sh` dispatchers are bash scripts for
macOS/Linux; on Windows use the `win\*.ps1` scripts directly.)


Build the Windows release from macOS/Linux
------------------------------------------

The WPF shell can be *compiled* (not run) on Unix — the .NET SDK supports
`EnableWindowsTargeting`, and the Go core cross-compiles with
`GOOS=windows`. Two options:

```
./win/build.sh          # needs go + the .NET 8 SDK (brew install dotnet-sdk)
./win/build_docker.sh   # needs go + Docker (shell compiles in mcr.microsoft.com/dotnet/sdk:8.0)
```

Both produce `win/dist/Pob/` and `Pob-<version>-windows-<arch>.zip` in the
project root. Default target is `amd64`; use `WIN_ARCHS="amd64 arm64"` to
build both.


Source layout
-------------

```
win/
    Pob.csproj                     .NET project (net8.0-windows, WPF)
    app.manifest                   per-monitor-v2 DPI awareness
    src/
        App.xaml(.cs)              lifecycle, window glue, frame persistence
        AppState.cs                shared state + mode transitions
        AppLogger.cs               app.log writer
        Interop/NativeMethods.cs   Win32: SendInput, BitBlt, window styles
        Services/
            CoreBridge.cs          pob-core process + JSON-RPC dispatch
            MouseService.cs        virtual cursor + input synthesis (worker thread)
            ScreenshotService.cs   content-area capture, cursor compositing, PNG
            SettingsService.cs     project root, settings.json, open/clear files
        Views/
            ToolbarWindow.xaml(.cs) the compact toolbar (main window)
            OverlayWindow.xaml(.cs) translucent content overlay + edge resize
            ContentView.cs          targeting/crop/toast/virtual-cursor drawing
            CursorArrow.cs          the arrow pointer geometry
            Dialogs.cs              max-step / macro-choice / clear / about
```

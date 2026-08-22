
Development
===========

Requirements: Go, plus the platform shell's toolchain — Xcode Command Line
Tools (Swift) on macOS, GTK 3 development libraries on Linux (see
[linux-x11/README.md](../../linux-x11/README.md)), or the .NET 8 SDK on Windows
(see [win/README.md](../../win/README.md)).

```
./setup.sh      # select your OS (recorded in the SYSTEM file), then
                # check toolchains and build core + that OS shell
./start.sh      # build and run in the foreground
./restart.sh    # rebuild and relaunch in the background (logs to ~/.pob/app.log)
./stop.sh       # stop the app and the core process
./build.sh      # release build (the dist/Pob folder a release zip is made of)
./keyboard.sh   # build and run Pob Keyboard (its own Go module, not built
                # by the scripts above)
```

The root scripts are dispatchers: `setup.sh` writes your choice (`macos` or
`linux-x11`) to the `SYSTEM` file, and the others read it and forward to the
matching folder's script (`macos/*.sh` or `linux-x11/*.sh`), which can also
be run directly.

Windows has no bash to dispatch from, so it runs its own scripts directly —
`win\setup.ps1`, `win\start.ps1`, `win\restart.ps1`, `win\stop.ps1`,
`win\build.ps1` — one per root script, doing the same job.

On macOS a dev run and a built bundle are two different things to the
permission system. Posting mouse and keyboard events needs Accessibility, which
macOS grants per app identity: `./start.sh` runs `macos/.build/*/Pob` as a child
of your terminal, so the events are attributed to the terminal app and borrow
its grant, while `Pob.app` is its own identity and needs its own — added by
hand, since nothing prompts for Accessibility. `macos/build.sh` ad-hoc signs
the bundle whenever no Developer ID is in the keychain, and macOS pins an
ad-hoc grant to that exact binary, so every rebuild invalidates it: the toggle
stays on in the list while clicks stop working — and screenshots come back
black or empty the same way. After replacing an installed copy, reset both
services and grant them again:

```
tccutil reset Accessibility com.gcc3.pob
tccutil reset ScreenCapture com.gcc3.pob
tccutil reset All com.gcc3.pob            # or both at once, plus anything else
```

Reset with Pob quit, then reopen it: Screen Recording prompts again on the
first capture, Accessibility still has to be added by hand. Renaming the bundle
ID leaves the old identity's entries behind, pointing at an app that no longer
exists — `tccutil reset All com.gcc3.pob` clears them.


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

All three zips unzip to the same `Pob/` folder: the shell app, `pob-core`
beside it, the `pob` CLI in `Helpers/`, a `README.txt` telling the person who
downloaded it how to install and use the `pob` command, and — on Linux and
Windows — the installer that does it (`install.sh` / `install.ps1`; macOS has
the app-menu item instead). See [CLI](07_CLI.md).

The `README.txt` in each zip is `macos/README.txt`, `linux-x11/README.txt` or
`win/README.txt`, copied in at build time.

Every build compiles `VERSION` into what it produces — the App menu's About
box and the window title read that, not a file, because an installed copy has
no `VERSION` file at a path it can guess: macOS stamps `Info.plist`
(`CFBundleShortVersionString`), Linux compiles in `-DPOB_VERSION`, Windows
passes `-p:Version` to `dotnet publish`, and the `pob` CLI takes
`-ldflags -X main.version`. A build made without the stamp (a bare
`swift build`, `make`, or `dotnet build`) falls back to reading `VERSION` from
the checkout, so a dev run still shows the right number.


See also
--------

- [Architecture](01_Architecture.md) — what each folder builds and how they talk
- [Pob Keyboard](13_Keyboard.md) — the one component with its own toolchain needs
- [Windows VM](15_VMWare.md) — where to *run* the Windows build when the only machine
  you have is a Mac: a Fusion guest, deployed to with `win/vm_deploy.sh`

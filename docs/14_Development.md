
Development
===========

Requirements: Go, plus the platform shell's toolchain — Xcode Command Line
Tools (Swift) on macOS, GTK 3 development libraries on Linux (see
[linux-x11/README.md](../linux-x11/README.md)), or the .NET 8 SDK on Windows
(see [win/README.md](../win/README.md)).

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


See also
--------

- [Architecture](01_Architecture.md) — what each folder builds and how they talk
- [Pob Keyboard](13_Keyboard.md) — the one component with its own toolchain needs

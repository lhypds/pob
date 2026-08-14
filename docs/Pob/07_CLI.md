
CLI
===

The `pob` command controls and inspects Pob from the terminal.

Every release ships the CLI beside the app in a `Helpers` folder — the app's
own executable is called `pob` too on Linux and `Pob.exe` on Windows, and a
case-insensitive filesystem cannot hold both in one directory. How it gets on
the `PATH` differs by platform:

| Platform | Install | What it does |
|----------|---------|--------------|
| macOS | **Pob → Install 'pob' Command…** in the app menu | Symlinks `Pob.app/Contents/Helpers/pob` at `/usr/local/bin/pob` (asks for an admin password when needed; the same menu item uninstalls it again) |
| Linux | `./install.sh` in the unzipped folder | Copies the app to `~/.local/share/pob` and links the CLI at `~/.local/bin/pob` — or `/opt/pob` and `/usr/local/bin/pob` under `sudo`. `--prefix` / `--bin` override both, `--uninstall` reverses it |
| Windows | `powershell -ExecutionPolicy Bypass -File install.ps1` | Copies the app to `%LOCALAPPDATA%\Programs\Pob`, adds its `Helpers` folder to the user `PATH` and puts Pob in the Start menu. No administrator prompt; `-Uninstall` reverses it, `-InstallDir` moves it |

Neither installer touches `~/.pob`, so settings, instances and logs survive an
uninstall and a reinstall.

The dev scripts build the CLI to `core/bin/pob` next to `pob-core` instead
(add that folder to your `PATH`, or call it by path).

Everything lives in `~/.pob`, created on first use and shared by the app and
the CLI: `settings.json` is the machine's, `INSTANCE` names the instance
directory, and that directory holds its `src/` macros, `instance.json` and
`logs/`.

A running Pob serves a small control API on an ephemeral localhost port,
advertised in `~/.pob/<instance>/control.json`; the CLI reads that file
and talks to that API — see **Control API** below. Log and session inspection
reads the log tree directly, so it also works when the app is not running.

```
Usage: pob [flags] [command] [args]

Flags:
  -v, --version      Print the Pob version, the same as the version command
  --session <id>     Target session; with no command, shows its details
```

`launch` takes one option of its own, `--start`; everything else after it is the
instance, so `pob launch --start "Work laptop"` is that instance, started and
running.

| Command | Description |
|---------|-------------|
| *(none)* | Show the instance and its sessions; with `--session` show that session |
| `launch [instance]` | Start the app; fails if it is already running. With more than one instance and none named, it lists them and asks which to start — ↑/↓ (or `k`/`j`) to move, enter to start, a digit to pick a row outright, `q` to cancel; `<instance>` is a name or an id, which skips the list. The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the app beside `Helpers/` in a Linux or Windows install, the shell build outputs for `core/bin/pob` |
| `launch --start` | The same launch, and then the macro: as soon as the new instance's control API answers, the run `start` would have started is started on it. It is the one way to get from nothing running to a running macro in one command — `pob start` on its own has nothing to talk to until a launch has finished — which is what a cron entry or a login item needs. Combines with an instance: `pob launch --start "Work laptop"` |
| `new [name]` | Create an instance — its own `src/` and `logs/`, on the machine's existing settings — under the name given, asking for one when it isn't. The new instance becomes the one `pob launch` starts next |
| `status` | Live status (executing, recording, psl, MCP, server address) |
| `sessions` | List sessions with duration and token usage |
| `check` | Everything that has to be right before a run, in one report — see **Checking** below. `src/main.macro.psl` and the files it `call`s, line by line, and then this machine: psl, what psl fills a slot with, the app and the `pob-core` behind it, `settings.json`, `stop_hook`. It reads files and talks to no one, which makes it the command that answers with Pob closed; exits `1` when there is anything to fix |
| `start` | Execute [`src/main.macro.psl`](03_Macro%20PSL.md) on the running instance — the same run as the toolbar's Execute button, and what `stop` stops. With nothing running it says so and names `launch --start`, since starting the app is also called starting |
| `stop` | Stop the running session |
| `kill` | Quit the running instance. It is the shell app that is signalled — `pob-core` exits with the pipe to it, writing the instance's end time — and only when it does not go within 10s is anything killed outright. Nothing running is reported, not an error |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Register the MCP server in the user settings of installed agent CLIs (`claude`, `gemini`) and print its info. The server starts with the instance, so there is usually nothing to start; `[port]` moves it to that port first |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `update` | Install the latest release over this install — see **Updating** below. `--version VER` installs that release instead, which is how one is reinstalled over itself or an older one gone back to; `--prefix DIR` installs somewhere else, `--bin DIR` (Linux and macOS) moves the symlink. Pob has to be closed |
| `update --check` | Print what is installed and what the latest release is, and install nothing. Exits `1` when there is a newer one, so a script can ask without reading the text |
| `version` | Print the Pob version. `pob -v` and `pob --version` print the same thing — they are read before the flags are parsed, so they answer wherever a version is asked for. What is printed is stamped into the CLI at build time, so a build from a checkout says `dev` |

Examples:

```
pob                                      # what's running?
pob new "Work laptop"                    # create an instance and switch to it
pob launch                               # start the app (asks which, if there are several)
pob launch "Work laptop"                 # start that one
pob launch --start "Work laptop"         # start that one and run its macro
pob check                                # is the macro sound, and can this machine run it?
pob start                                # replay src/main.macro.psl; pob stop stops it
pob --session 1752712400                 # session detail: the macro, conditions, usage
pob mcp start                            # register MCP with the agent CLIs here
pob update --check                       # is there a newer release?
pob update                               # install it over this one
```


Checking
--------

`pob check` is the report to read before a run. It asks nothing of the app, so it
answers with Pob closed — the state a macro is written in, and the state a new
install is in.

Two groups come out of it, printed apart because they are fixed apart.

**The macro** — `src/main.macro.psl` and every file it `call`s, line by line.
This is the same reading Execute takes before the cursor moves (see
[When something is wrong](../Macro%20PSL/12_When%20something%20is%20wrong.md)),
so a macro this passes is a macro that will start: an unknown statement, a call
written with the wrong number of arguments, a `call()` naming a file that is not
there, a `/*` nobody closed, a `:: … ::` in a `.macro` that psl is never run for.

**This machine** — what a run needs of it besides the file:

| Checked | Why |
|---------|-----|
| `settings.json` parses | The app and the core read a file they cannot parse as an empty one, so one stray comma quietly puts every setting back to its default and nothing says so |
| psl is where `settings.json` says | A `:: … ::` is filled by running psl, and a macro with slots in it cannot start without one. Not checked for a macro that has none — psl is never started then, and the summary says as much |
| psl has something to fill with | A `.pslrc` in `~/.pob` or in your home directory, or a key in the environment. With neither, every slot fails at the moment the replay reaches it |
| The app, and `pob-core` behind it | The app is the window and core is what runs the macro. Both are looked for exactly where the CLI and the shells look for them, the checkout layouts included |
| The libraries the app is linked against | Linux only, by asking `ldd` — the same reading `get.sh` takes after an install, repeated because they also go missing later: an upgrade takes one away and the app stops starting with nothing on screen to say why |
| `stop_hook` names something that exists | It is started with a shell and never waited for, so a hook naming a command that is not installed is the quietest failure there is — the run ends, nothing announces it, and no log line says why |

```
$ pob check
Instance:   pb-b424 (Work laptop)
Macro:      /Users/you/.pob/pb-b424/src/main.macro.psl
psl:        /opt/homebrew/bin/psl
App:        /Applications/Pob.app
Core:       /Applications/Pob.app/Contents/MacOS/pob-core

main.macro.psl — 2 problems:
  line 1: move takes 2 arguments, and 1 was written
  line 4: call("sign-out.macro.psl") names /Users/you/.pob/pb-b424/src/sign-out.macro.psl, and there is no such file

2 problems to fix.
```

That summary is most of the answer when nothing is wrong: which psl and which
app were found is what `Nothing to fix.` is about.

The problems go to stderr and the summary to stdout, and the exit status is what
a script goes by — `0` when there is nothing to fix, `1` when there is — so it
can stand in front of a run:

```
pob check && pob launch --start
```


Updating
--------

`pob update` reinstalls Pob from its releases, over the install the `pob` that
ran it belongs to. No installer of its own is involved: on Linux and macOS it
fetches [`get.sh`](https://github.com/lhypds/pob/blob/master/get.sh) — the
script the one-line install pipes into `sh` — and runs it with `--version` and
`--prefix` filled in; on Windows, where there is no such script, it downloads
`Pob-<version>-windows-<arch>.zip` and runs the `install.ps1` inside it. Either
way what installs the release is the release's own installer, and the answer to
"what will this do to my machine" is the same one the README's install has.

Which release is the latest is read off the tag page GitHub redirects
`/releases/latest` to, so there is no API call and no token needed.

Three things the command adds to doing it by hand:

- **It installs over this copy.** The install is found from the CLI's own path
  — every install puts the CLI in a `Helpers` directory beside the app — so an
  app in `~/Applications` is replaced there rather than a second one appearing
  in `/Applications`. A `pob` that is *not* in such a directory is a build in a
  checkout, and the command says so instead of installing over it: there, `git
  pull` is the update. `--prefix DIR` overrides all of this.
- **It stops if Pob is running.** The app being replaced is the app running:
  Windows will not overwrite its files at all, and elsewhere it would be left
  as a live process with a different install underneath it. `pob kill` first.
- **It says when the install needs `sudo`** rather than failing halfway through
  a tree it cannot write — a root install (`/opt/pob`) is updated with `sudo pob
  update`.

On Unix the installer replaces the `pob` process rather than running as a child
of it, since that process lives inside the very bundle being taken apart; its
output and its exit status are the command's. On Windows nothing may delete a
running `.exe`, but anything may rename one, so the CLI moves itself to
`Helpers\pob.exe.old` before the installer writes the new one and the next
update sweeps the file up.

`~/.pob` is not touched by any of it — settings, instances, macros and logs
survive an update as they survive a reinstall. On macOS the Accessibility and
Screen Recording grants do not: they are tied to the exact copy of the app
macOS was shown, and Pob is not signed with a Developer ID, so a replaced app
keeps switches that look on while clicks are dropped in silence. The installer
prints the `tccutil reset All com.gcc3.pob` that clears them.


How it reaches the app
----------------------

The app owns `pob-core` as a child process and drives it over that process's
stdin and stdout, a pipe a `pob` typed into a terminal has no way to join. So
`pob-core` also serves the [Control API](11_Control%20API.md) on an ephemeral
`127.0.0.1` port, and advertises it by writing its pid and port into
`~/.pob/<instance>/instance.json`:

```json
{
  "id": "pb-b424",
  "name": "Work laptop",
  "start_time": 1752712400,
  "pid": 4711,
  "port": 57259
}
```

The port is whatever the OS hands out, so it is a different one on every
launch and the file is the only way to find it. The two fields are there only
while the instance runs — it stops advertising itself by clearing them, so a
file without a `port` is a stopped instance. `status`, `start`, `stop`,
`screenshot` and the `mcp` commands are each one call to that
API; the rest read the `logs/` tree directly, which is why they still work
with the app closed. `check` is in neither group: it reads `src/main.macro.psl`
and this machine itself rather than asking the instance anything, which is what
lets a macro be checked while it is being written and an install before it has
ever been started.

`kill` uses the API for one thing only — asking the instance which process it
is — and then works on the processes themselves. The pid it gets back is
`pob-core`'s, and core's parent is the shell that spawned it, so that is what
gets signalled; core follows it down when their pipe closes. The parent is
checked for the name the shell is built under (`Pob`, `pob`, `Pob.exe`) before
anything is sent to it, so a `pob-core` started by hand from a terminal takes
the signal itself rather than handing it to the terminal.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — what `pob start` runs and `pob check` reads
- [Control API](11_Control%20API.md) — the endpoints behind these commands
- [Logs](05_Logs.md) — the tree `pob` reads for session detail
- [MCP Server](08_MCP.md) — the server `pob mcp start` registers
- [Pob Server](09_Server.md) — the address `pob status` prints

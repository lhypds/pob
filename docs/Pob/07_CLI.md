
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
directory, and that directory holds its `macro.psl`, `instance.json` and
`logs/`.

A running Pob serves a small control API on an ephemeral localhost port,
advertised in `~/.pob/<instance>/control.json`; the CLI reads that file
and talks to that API — see **Control API** below. Log and session inspection
reads the log tree directly, so it also works when the app is not running.

```
Usage: pob [flags] [command] [args]

Flags:
  --session <id>     Target session; with no command, shows its details
```

| Command | Description |
|---------|-------------|
| *(none)* | Show the instance and its sessions; with `--session` show that session |
| `launch [instance]` | Start the app; fails if it is already running. With more than one instance and none named, it lists them and asks which to start — ↑/↓ (or `k`/`j`) to move, enter to start, a digit to pick a row outright, `q` to cancel; `<instance>` is a name or an id, which skips the list. The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the app beside `Helpers/` in a Linux or Windows install, the shell build outputs for `core/bin/pob` |
| `new [name]` | Create an instance — its own `macro.psl` and `logs/`, on the machine's existing settings — under the name given, asking for one when it isn't. The new instance becomes the one `pob launch` starts next |
| `status` | Live status (executing, recording, psl, MCP, server address) |
| `sessions` | List sessions with duration and token usage |
| `macro` | Execute [`macro.psl`](03_Macro%20PSL.md) (same as the toolbar Execute button) |
| `macro --check` | Read `macro.psl` and the files it `call`s, print what is wrong with them line by line, and run nothing. The same check Execute refuses a run over, so a macro this passes is one that will start. It reads the file and talks to no one, which makes it the one `macro` command that works with Pob closed; exits `1` when there is anything to fix |
| `stop` | Stop the running session |
| `kill` | Quit the running instance. It is the shell app that is signalled — `pob-core` exits with the pipe to it, writing the instance's end time — and only when it does not go within 10s is anything killed outright. Nothing running is reported, not an error |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Register the MCP server in the user settings of installed agent CLIs (`claude`, `gemini`) and print its info. The server starts with the instance, so there is usually nothing to start; `[port]` moves it to that port first |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `version` | Print the Pob version |

Examples:

```
pob                                      # what's running?
pob new "Work laptop"                    # create an instance and switch to it
pob launch                               # start the app (asks which, if there are several)
pob launch "Work laptop"                 # start that one
pob macro                                # replay macro.psl
pob macro --check                        # read it and say what is wrong with it
pob --session 1752712400                 # session detail: the macro, conditions, usage
pob mcp start                            # register MCP with the agent CLIs here
```


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
file without a `port` is a stopped instance. `status`, `macro`, `stop`,
`screenshot` and the `mcp` commands are each one call to that
API; the rest read the `logs/` tree directly, which is why they still work
with the app closed. `macro --check` is the odd one of that first group: it
reads `macro.psl` itself rather than asking the instance about it, so a macro
can be checked while it is being written and before anything is running.

`kill` uses the API for one thing only — asking the instance which process it
is — and then works on the processes themselves. The pid it gets back is
`pob-core`'s, and core's parent is the shell that spawned it, so that is what
gets signalled; core follows it down when their pipe closes. The parent is
checked for the name the shell is built under (`Pob`, `pob`, `Pob.exe`) before
anything is sent to it, so a `pob-core` started by hand from a terminal takes
the signal itself rather than handing it to the terminal.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — what `pob macro` runs
- [Control API](11_Control%20API.md) — the endpoints behind these commands
- [Logs](05_Logs.md) — the tree `pob` reads for session detail
- [MCP Server](08_MCP.md) — the server `pob mcp start` registers
- [Pob Server](09_Server.md) — the address `pob status` prints

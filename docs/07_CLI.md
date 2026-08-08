
CLI
===

The `pob` command controls and inspects Pob from the terminal.

On macOS the packaged app ships the CLI inside the bundle
(`Pob.app/Contents/Helpers/pob`) — use **Pob → Install 'pob' Command…** in the
app menu to symlink it at `/usr/local/bin/pob` (asks for an admin password
when needed; the same menu item uninstalls it again). The dev scripts also
build it to `core/bin/pob` next to `pob-core` (add that folder to your `PATH`,
or call it by path).

Everything lives in `~/.pob`, created on first use and shared by the app and
the CLI: `INSTANCE` names the instance directory, and that directory holds its
`settings.json`, `instruction.txt`, `macro.txt`, `instance.json` and `logs/`.

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
| `launch [instance]` | Start the app; fails if it is already running. With more than one instance and none named, it lists them and asks which to start — ↑/↓ (or `k`/`j`) to move, enter to start, a digit to pick a row outright, `q` to cancel; `<instance>` is a name or an id, which skips the list. The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the shell build outputs for `core/bin/pob` |
| `new [name]` | Create an instance — its own `settings.json`, `instruction.txt`, `macro.txt` and `logs/` — under the name given, asking for one when it isn't. The new instance becomes the one `pob launch` starts next |
| `status` | Live status (executing, recording, model, MCP, server address) |
| `sessions` | List sessions with duration and token usage |
| `start` | Execute `instruction.txt` (same as the toolbar Execute button) |
| `run <text...>` | Replace `instruction.txt` with `<text>`, then execute it |
| `macro` | Execute `macro.txt` |
| `stop` | Stop the running session |
| `kill` | Quit the running instance. It is the shell app that is signalled — `pob-core` exits with the pipe to it, writing the instance's end time — and only when it does not go within 10s is anything killed outright. Nothing running is reported, not an error |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Start the MCP server and print its info (port defaults to `8032`). Registers the server in the user settings of installed agent CLIs (`claude`, `gemini`) |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `version` | Print the Pob version |

Examples:

```
pob                                      # what's running?
pob new "Work laptop"                    # create an instance and switch to it
pob launch                               # start the app (asks which, if there are several)
pob launch "Work laptop"                 # start that one
pob run "click Save and close the dialog"
pob start                                # run instruction.txt
pob --session 1752712400                 # session detail: plans, steps, usage
pob mcp start                            # start MCP and print the connection info
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
file without a `port` is a stopped instance. `status`, `start`, `run`,
`macro`, `stop`, `screenshot` and the `mcp` commands are each one call to that
API; the rest read the `logs/` tree directly, which is why they still work
with the app closed.

`kill` uses the API for one thing only — asking the instance which process it
is — and then works on the processes themselves. The pid it gets back is
`pob-core`'s, and core's parent is the shell that spawned it, so that is what
gets signalled; core follows it down when their pipe closes. The parent is
checked for the name the shell is built under (`Pob`, `pob`, `Pob.exe`) before
anything is sent to it, so a `pob-core` started by hand from a terminal takes
the signal itself rather than handing it to the terminal.


See also
--------

- [Control API](11_Control%20API.md) — the endpoints behind these commands
- [Logs](05_Logs.md) — the tree `pob` reads for session detail
- [MCP Server](08_MCP.md) — what `pob mcp start` brings up
- [Pob Server](09_Server.md) — the address `pob status` prints

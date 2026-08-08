
CLI
===

The `pob` command controls and inspects Pob from the terminal.

On macOS the packaged app ships the CLI inside the bundle
(`Pob.app/Contents/Helpers/pob`) — use **Pob → Install 'pob' Command…** in the
app menu to symlink it at `/usr/local/bin/pob` (asks for an admin password
when needed; the same menu item uninstalls it again). The dev scripts also
build it to `core/bin/pob` next to `pob-core` (add that folder to your `PATH`,
or call it by path).

All project files (`settings.json`, `instruction.txt`, `macro.txt`, `logs/`)
live in `~/.pob`, created on first use and shared by the app and the CLI.

A running Pob serves a small control API on an ephemeral localhost port,
advertised in `~/.pob/logs/<instance>/control.json`; the CLI reads that file
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
| `launch` | Start the app (alias: `new`); fails if it is already running. The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the shell build outputs for `core/bin/pob` |
| `status` | Live status (executing, recording, model, MCP, server address) |
| `sessions` | List sessions with duration and token usage |
| `start` | Execute `instruction.txt` (same as the toolbar Execute button) |
| `run <text...>` | Replace `instruction.txt` with `<text>`, then execute it |
| `macro` | Execute `macro.txt` |
| `stop` | Stop the running session |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Start the MCP server and print its info (port defaults to `8032`). Registers the server in the user settings of installed agent CLIs (`claude`, `gemini`) |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `version` | Print the Pob version |

Examples:

```
pob                                      # what's running?
pob launch                               # start the app
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
`127.0.0.1` port, and advertises it by writing
`~/.pob/logs/<instance>/control.json`:

```json
{
  "pid": 4711,
  "port": 57259,
  "start_time": 1752712400
}
```

The port is whatever the OS hands out, so it is a different one on every
launch and the file is the only way to find it. `status`, `start`, `run`,
`macro`, `stop`, `screenshot` and the `mcp` commands are each one call to that
API; the rest read the `logs/` tree directly, which is why they still work
with the app closed.


See also
--------

- [Control API](11_Control%20API.md) — the endpoints behind these commands
- [Logs](05_Logs.md) — the tree `pob` reads for session detail
- [MCP Server](08_MCP.md) — what `pob mcp start` brings up
- [Pob Server](09_Server.md) — the address `pob status` prints

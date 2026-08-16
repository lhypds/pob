
Control API
===========

The API that *controls* the app — replay a macro, stop a session, start
the MCP server, ask what is going on. It is how the [`pob` CLI](07_CLI.md)
reaches a running instance, and it is meant for nothing else.

(The other one is the [Operation API](10_Operation%20API.md), which operates the
machine — pointer, keys, text — and is open to the network.)

The CLI cannot reach the app directly: the app owns `pob-core` as a child
process and drives it over that process's stdin and stdout, a pipe a `pob`
typed into a terminal has no way to join. So `pob-core` also serves this small
HTTP API on an ephemeral `127.0.0.1` port, alongside the bridge.


Finding it
----------

The port is advertised in `~/.pob/<instance>/instance.json`, written when the
instance starts and cleared when it stops:

```json
{
  "id": "pb-b424",
  "name": "Work laptop",
  "start_time": 1752712400,
  "pid": 4711,
  "port": 57259
}
```

The port is whatever the OS hands out, so it is a different one on every launch
and the file is the only way to find it. A file left behind by a crash is
harmless: an instance counts as running only when `GET /status` on that port
answers with the matching instance ID, so a stale entry — or a port some other
program has since been given — is ignored.


Endpoints
---------

Requests and responses are JSON, and a failure is a non-2xx status carrying
`{"error": "..."}`. The `POST` endpoints answer any other method with 405.

| Endpoint | CLI | Description |
|----------|-----|-------------|
| `GET /status` | `status` | Instance ID, pid, root, executing and recording state, current session, where the `psl` compiler was found, and the `mcp` and `server` blocks below |
| `GET /mcp` | `mcp status` | `running`, `host`, `port`, `url`, `urls` — one per network when `mcp_host` opens it to them, `url` being the first and the one the local agent CLIs are registered with — and `tools` |
| `GET /server` | — | The [Pob server](09_Server.md): `running`, `port`, `url`, `urls` — one per network the machine is on. `pob status` reads the same block out of `/status` |
| `POST /mcp/start` | `mcp start` | Body `{"port": 8032}` optional, defaulting to the `mcp_port` setting — which is the port it is already on, so this is a no-op unless another one is asked for, and then the server moves there. 409 when the port will not bind |
| `POST /mcp/stop` | `mcp stop` | Always succeeds |
| `POST /run/macro` | `macro` | Replays [`src/main.macro.psl`](03_Macro%20PSL.md). Body `{"file": "/abs/path.macro.psl"}` optional — `pob start --macropsl` sends it — and replays that file instead: 400 when the path is not absolute, since the caller and the core run in different directories and only the caller knows which one a relative path meant, and 404 when there is nothing there. 409 when a session is already running |
| `POST /run/stop` | `stop` | Idempotent |
| `POST /screenshot` | `screenshot` | Returns `{"path": "..."}`. 409 while a session is running — it owns the capture pipeline |

A stopped MCP or Pob server — one turned off in `settings.json`, or one whose
port would not bind — still reports the port it *would* take rather than `0`, so
`pob mcp status` prints the address either way.

```
$ curl -s http://127.0.0.1:57259/status
{"executing":false,"instance":"pb-a703","mcp":{...},"psl":"/usr/local/bin/psl", ...}

$ curl -s -X POST http://127.0.0.1:57259/run/macro
{"started":true}
```


Reach
-----

**This API binds `127.0.0.1` only, and carries no authentication** — the
loopback bind is what stands in for one. It is the CLI's private channel, not
a public interface: it can replay a macro and take screenshots, so putting
it on a network interface would hand the machine to that network.

That is also why it stays separate from the [Pob server](09_Server.md), which
listens on every interface by design.

Driving Pob from another machine goes through the [Operation
API](10_Operation%20API.md) or [MCP](08_MCP.md) instead. And the app's own toolbar
buttons never come this way either — they go down the stdin/stdout bridge, to
the same session runner these endpoints call.


See also
--------

- [CLI](07_CLI.md) — the one client of this API
- [Operation API](10_Operation%20API.md) — the other API: operating the machine
- [MCP Server](08_MCP.md) — the server `POST /mcp/start` registers and moves
- [Pob Server](09_Server.md) — what `GET /server` describes
- [Logs](05_Logs.md) — where `instance.json` lives

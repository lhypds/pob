
Control API
===========

The API that *controls* the app — run an instruction, stop a session, start
the MCP server, ask what is going on. It is how the [`pob` CLI](CLI.md)
reaches a running instance, and it is meant for nothing else.

(The other one is the [Operation API](Operation%20API.md), which operates the
machine — pointer, keys, text — and is open to the network.)

The CLI cannot reach the app directly: the app owns `pob-core` as a child
process and drives it over that process's stdin and stdout, a pipe a `pob`
typed into a terminal has no way to join. So `pob-core` also serves this small
HTTP API on an ephemeral `127.0.0.1` port, alongside the bridge.


Finding it
----------

The port is advertised in `~/.pob/logs/<instance>/control.json`, written when
the instance starts and removed when it stops:

```json
{
  "pid": 4711,
  "port": 57259,
  "start_time": 1752712400
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
| `GET /status` | `status` | Instance ID, pid, root, executing and recording state, current session, model, and the `mcp` and `server` blocks below |
| `GET /mcp` | `mcp status` | `running`, `port`, `url`, `tools` |
| `GET /server` | — | The [Pob server](Server.md): `running`, `port`, `url`, `urls` — one per network the machine is on. `pob status` reads the same block out of `/status` |
| `POST /mcp/start` | `mcp start` | Body `{"port": 8032}` optional. 409 when the port will not bind |
| `POST /mcp/stop` | `mcp stop` | Always succeeds |
| `POST /run/instruction` | `start`, `run` | Body `{"instruction": "..."}` optional; given, it replaces `instruction.txt` before running. 409 when a session is already running |
| `POST /run/macro` | `macro` | Same 409 |
| `POST /run/stop` | `stop` | Idempotent |
| `POST /screenshot` | `screenshot` | Returns `{"path": "..."}`. 409 while a session is running — it owns the capture pipeline |

A stopped MCP or Pob server still reports the port it *would* take rather than
`0`, so `pob mcp status` can print the address before anything is started.

```
$ curl -s http://127.0.0.1:57259/status
{"executing":false,"instance":"pb-a703","mcp":{...},"model":"...", ...}

$ curl -s -X POST http://127.0.0.1:57259/run/instruction \
       -d '{"instruction":"click Save and close the dialog"}'
{"started":true}
```


Reach
-----

**This API binds `127.0.0.1` only, and carries no authentication** — the
loopback bind is what stands in for one. It is the CLI's private channel, not
a public interface: it can run instructions and take screenshots, so putting
it on a network interface would hand the machine to that network.

That is also why it stays separate from the [Pob server](Server.md), which
listens on every interface by design.

Driving Pob from another machine goes through the [Operation
API](Operation%20API.md) or [MCP](MCP.md) instead. And the app's own toolbar
buttons never come this way either — they go down the stdin/stdout bridge, to
the same session runner these endpoints call.


See also
--------

- [CLI](CLI.md) — the one client of this API
- [Operation API](Operation%20API.md) — the other API: operating the machine
- [MCP Server](MCP.md) — what `POST /mcp/start` brings up
- [Pob Server](Server.md) — what `GET /server` describes
- [Logs](Logs.md) — where `control.json` lives

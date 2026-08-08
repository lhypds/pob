
Architecture
============

Pob is split into a platform-independent brain and a native shell:

```
core/    The brain (Go, zero dependencies). Agent loop (plan → execute →
         verify), OpenAI-compatible LLM client, session logs, macro engine,
         the MCP SSE server, and the localhost Control API the pob CLI drives
         the app with. Compiled to a single binary: pob-core.

macos/   The hands and eyes (Swift). Overlay window UI, screenshot capture,
         virtual cursor, mouse/keyboard event injection, and the permission
         surface (Screen Recording / Accessibility).

linux-x11/  The same hands and eyes for Linux/Xorg (C + GTK 3). Identical UI
            and features; screenshots via XGetImage, input via XTest.
            See linux-x11/README.md.

win/     The same hands and eyes for Windows (C# / WPF). Identical UI and
         features; screenshots via GDI, input via SendInput.
         See win/README.md.

server/  The Pob server (Go, zero dependencies), compiled into pob-core and
         started with the instance: an HTTP server serving the remote-control
         API and the machine's current frame, and hosting the three Web UI
         pages it keeps in server/webui/.

keyboard/  Pob Keyboard (Go + Fyne), a separate desktop app: a full-size
           on-screen keyboard and a trackpad in their own window, driving Pob
           through the same server API. Run it with ./keyboard.sh.
```

The shell spawns `pob-core` as a child process and the two talk over
stdin/stdout with line-delimited JSON-RPC:

- Shell → core: `run.instruction`, `run.macro`, `run.stop`, `recording.changed`
- Core → shell: `screenshot.capture`, `cursor.move`, `mouse.click`,
  `keyboard.type`, `ui.confirmMaxStep`, … and `session.state` notifications

Everything that drives the machine goes through those same calls — the agent
loop, the MCP server, and the Pob server — so a tap on a phone and a tool call
from a model take exactly the same path.

All coordinates crossing the boundary are screenshot pixels; the shell owns
the conversion to real screen positions. Porting to a new platform means
reimplementing only the shell — the brain is shared.


See also
--------

- [Pob Server](09_Server.md) — the HTTP server started with every instance
- [Control API](11_Control%20API.md) — the other server, loopback only, for the CLI
- [Pob Keyboard](13_Keyboard.md) — the separate desktop client in `keyboard/`
- [Development](14_Development.md) — building the core and the shells
- [Logs](05_Logs.md) — what the core writes as it runs

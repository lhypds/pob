
Architecture
============

Pob is split into a platform-independent brain and a native shell:

```
core/    The brain (Go, zero dependencies). The Prompt Script Language
         engine, session logs, the MCP SSE server — started with the instance,
         like the Pob server below — and the localhost Control API the pob CLI
         drives the app with. Compiled to a single binary: pob-core. It makes
         no model calls of its own: an AI slot is filled by running psl.

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
         pages it keeps in server/public/.

keyboard/  Pob Keyboard (Go + Fyne), a separate desktop app: a full-size
           on-screen keyboard and a trackpad in their own window, driving Pob
           through the same server API. Run it with ./keyboard.sh.
```

The shell spawns `pob-core` as a child process and the two talk over
stdin/stdout with line-delimited JSON-RPC:

- Shell → core: `run.macro`, `run.stop`, `recording.changed`
- Core → shell: `screenshot.capture`, `cursor.move`, `mouse.click`,
  `keyboard.type`, `ui.alert`, `ui.lock`, `ui.clickThrough`, `ui.record`, …
  and `session.state` notifications

Captured frames are the one thing that does not travel that way. They go down
a second, binary connection instead: the core listens on loopback and offers
the port and a token in a `frames.channel` notification, the shell connects
back and pushes frames as `POBF`-tagged, length-prefixed blocks. A frame on
the JSON-RPC line would be base64 — a third again as many bytes, parsed as one
enormous string — and worse, it would sit in front of every mouse move and
keystroke waiting behind it. At one frame a second that is invisible; at
thirty it is the difference between a view you can work in and one you can
only watch. A shell that does not connect keeps answering with base64 on the
JSON-RPC line, which is what every shell did before the channel existed.

Everything that drives the machine goes through those same calls — a macro
replay, the MCP server, and the Pob server — so a tap on a phone and a tool
call from a model take exactly the same path.

All coordinates crossing the boundary are screenshot pixels; the shell owns
the conversion to real screen positions. Porting to a new platform means
reimplementing only the shell — the brain is shared.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — `src/main.macro.psl` and the language the engine runs
- [Pob Server](09_Server.md) — the HTTP server started with every instance
- [MCP Server](08_MCP.md) — the other one started with it, for MCP clients
- [Control API](11_Control%20API.md) — the other server, loopback only, for the CLI
- [Pob Keyboard](13_Keyboard.md) — the separate desktop client in `keyboard/`
- [Development](14_Development.md) — building the core and the shells
- [Logs](05_Logs.md) — what the core writes as it runs



Pob
===


Perception & Operation Bridge.  


Purpose
-------

Pob is designed to connect AI with desktop applications.

It allows AI to:

- View the current desktop or application window
- Move and click the mouse
- Type text and press keys
- Record and replay operation macros
- Work with MCP-compatible AI clients

The same bridge works for people, not only for AI: every instance runs a
server, so there is a web page you can open on your phone, and a desktop app
with a full-size keyboard and a trackpad — see [Pob Server](docs/09_Server.md),
[Web UI](docs/12_Web UI.md) and [Pob Keyboard](docs/13_Keyboard.md).


Getting started
---------------

From a checkout:

```
./setup.sh      # select your OS, check toolchains, build core + shell
./start.sh      # run it
```

From a release zip, unzip it and install — the app goes somewhere permanent
and the `pob` command lands on your `PATH`:

```
./install.sh                                              # Linux
powershell -ExecutionPolicy Bypass -File install.ps1      # Windows
```

On macOS drag `Pob.app` to Applications and use **Pob → Install 'pob'
Command…** in the app menu. See [CLI](docs/07_CLI.md) for the details of all
three.

Put your API key and model in `~/.pob/settings.json`, write what you
want done in the instance's `instruction.txt` below it, and press Execute in the toolbar — or drive the
machine yourself from a phone, a terminal, or another computer.


Documentation
-------------

| Doc | What's in it |
|-----|--------------|
| [Architecture](docs/01_Architecture.md) | How the brain and the native shells are split, and how they talk |
| [UI](docs/02_UI.md) | The window and every toolbar button |
| [Macro](docs/03_Macro.md) | `macro.txt`, and the functions the AI and macros both call |
| [Key names](docs/04_Keys.md) | What `keyPress` / `key_press` accepts |
| [Logs](docs/05_Logs.md) | The `~/.pob/` tree: the instance directory, its sessions, plans and steps |
| [Settings](docs/06_Settings.md) | Every key in `settings.json` |
| [CLI](docs/07_CLI.md) | The `pob` command |
| [MCP Server](docs/08_MCP.md) | Driving Pob from Claude Code, Claude Desktop, Gemini CLI |
| [Pob Server](docs/09_Server.md) | The server every instance runs, and its address |
| [Operation API](docs/10_Operation%20API.md) | The HTTP command grammar for driving the machine |
| [Control API](docs/11_Control%20API.md) | The localhost API the `pob` CLI drives the app with |
| [Web UI](docs/12_Web%20UI.md) | The remote control page, for a phone |
| [Pob Keyboard](docs/13_Keyboard.md) | The desktop keyboard and trackpad client |
| [Development](docs/14_Development.md) | Building, the dev scripts, and cutting a release |
| [Windows VM](docs/VM.md) | Running and driving the Windows shell in a VM, from a Mac |

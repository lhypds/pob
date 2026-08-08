

![Pob Icon](https://github.com/user-attachments/assets/8c4be5c7-0b4a-4f86-abc1-d5f8a7e92314)


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
with a full-size keyboard and a trackpad — see [Pob Server](docs/Server.md),
[Web UI](docs/WebUI.md) and [Pob Keyboard](docs/Keyboard.md).


Getting started
---------------

```
./setup.sh      # select your OS, check toolchains, build core + shell
./start.sh      # run it
```

Put your API key and model in `~/.pob/settings.json`, write what you want done
in `~/.pob/instruction.txt`, and press Execute in the toolbar — or drive the
machine yourself from a phone, a terminal, or another computer.


Documentation
-------------

| Doc | What's in it |
|-----|--------------|
| [Architecture](docs/Architecture.md) | How the brain and the native shells are split, and how they talk |
| [UI](docs/UI.md) | The window and every toolbar button |
| [Macro](docs/Macro.md) | `macro.txt`, and the functions the AI and macros both call |
| [Key names](docs/Keys.md) | What `keyPress` / `key_press` accepts |
| [Logs](docs/Logs.md) | The `~/.pob/logs/` tree: instances, sessions, plans, steps |
| [Settings](docs/Settings.md) | Every key in `settings.json` |
| [CLI](docs/CLI.md) | The `pob` command |
| [MCP Server](docs/MCP.md) | Driving Pob from Claude Code, Claude Desktop, Gemini CLI |
| [Pob Server](docs/Server.md) | The server every instance runs, and its address |
| [Operation API](docs/Operation%20API.md) | The HTTP command grammar for driving the machine |
| [Control API](docs/Control%20API.md) | The localhost API the `pob` CLI drives the app with |
| [Web UI](docs/WebUI.md) | The remote control page, for a phone |
| [Pob Keyboard](docs/Keyboard.md) | The desktop keyboard and trackpad client |
| [Development](docs/Development.md) | Building, the dev scripts, and cutting a release |


Roadmap
-------

Phase 1. Make AI see its frontend development result.  
         To improve the frontend development automation. (DONE)  
Phase 2. Make the AI can operate the desktop application. (DONE)  
Phase 3. Make AI learn users operation and do it for the user with instructions, or repeat. (IN PROGRESS)  

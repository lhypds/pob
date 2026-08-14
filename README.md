

Pob
===


Pob (Perception & Operation Bridge) is designed to connect AI with desktop applications.  


Design
------

It perceives the application through the window that contains it and interacts with the application.

<img width="560" alt="image" src="https://github.com/user-attachments/assets/64db922a-4104-43c7-aa33-6511bbe484c9" />

Features:

- Macro PSL language, combines macros with AI prompts.  
- MCP server, enables AI to operate desktop applications directly.  
- Pob server, provides a web interface for remote control and monitoring.  
- Supports Windows, macOS, and Linux.


Getting started
---------------

1. One command (Linux and macOS)  

Downloads the release for this machine and installs it — the app goes
somewhere permanent and the `pob` command lands on your `PATH`:  

```
curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sh
```

sudo:  

```
curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sudo sh
```

On macOS this puts `Pob.app` in `/Applications` — an admin account needs no
`sudo` for that — and links the bundled `pob` command. You still have to grant
Accessibility and Screen Recording by hand; see the macOS note below.  

Anything after `sh -s --` is passed on: `--prefix DIR`, `--bin DIR`,
`--version VER`, or `--uninstall` to take it back off again.  

2. From the repository  

Clone it and `cd` into the repo.  

```
./setup.sh      # select your OS, check toolchains, build core + shell
./start.sh      # run it
```

3. From a [release](https://github.com/lhypds/pob/releases) zip

Unzip it and install — the app goes somewhere permanent
and the `pob` command lands on your `PATH`:

```
./install.sh                                              # Linux
powershell -ExecutionPolicy Bypass -File install.ps1      # Windows
```

On macOS drag `Pob.app` to Applications and use Pob → Install 'pob'
Command, in the app menu.  

macOS Only: allow the blocked first open in System Settings ▸ Privacy & Security, add
`Pob.app` to Accessibility by hand — nothing prompts for it, and clicks are
dropped in silence until you do — and allow Screen Recording.


Macro PSL
---------

To operate the application, Pob uses an AI native language called PSL (Prompt Script Language).  

How a macro runs:  
Pob executes it line by line -> reaches an AI slot -> psl fills the slot in -> execution continues

How it looks like (`src/main.macro.psl`):

```
// Reply to every unread message, then sign out.
move(398, 915)
click()
if (:: a chat window is open ::) {
    loop (:: another unread message in the list ::, 10) {
        move(:: the x offset to the message box ::, 738)
        click()
        typeText(:: a short reply to the message on screen ::)
        keyPress("return")
    }
}
call("sign-out.macro.psl")
```

Refer: [PSL](https://github.com/lhypds/psl) and [Macro PSL](docs/Macro%20PSL/01_Macro%20PSL.md)  


Documentation
-------------

| Doc | What's in it |
|-----|--------------|
| [Architecture](docs/Pob/01_Architecture.md) | How the brain and the native shells are split, and how they talk |
| [UI](docs/Pob/02_UI.md) | The window and every toolbar button |
| [Macro PSL](docs/Pob/03_Macro%20PSL.md) | `src/main.macro.psl` and the Prompt Script Language it is written in: recording and replaying, every statement, and the `:: … ::` slots psl fills in as it runs |
| [Key names](docs/Pob/04_Keys.md) | What `keyPress` / `key_press` accepts |
| [Logs](docs/Pob/05_Logs.md) | The `~/.pob/` tree: the instance directory and its sessions |
| [Settings](docs/Pob/06_Settings.md) | Every key in `settings.json` |
| [CLI](docs/Pob/07_CLI.md) | The `pob` command |
| [MCP Server](docs/Pob/08_MCP.md) | Driving Pob from Claude Code, Claude Desktop, Gemini CLI |
| [Pob Server](docs/Pob/09_Server.md) | The server every instance runs, and its address |
| [Operation API](docs/Pob/10_Operation%20API.md) | The HTTP command grammar for driving the machine |
| [Control API](docs/Pob/11_Control%20API.md) | The localhost API the `pob` CLI drives the app with |
| [Web UI](docs/Pob/12_Web%20UI.md) | The remote control page, for a phone |
| [Pob Keyboard](docs/Pob/13_Keyboard.md) | The desktop keyboard and trackpad client |
| [Development](docs/Pob/14_Development.md) | Building, the dev scripts, and cutting a release |
| [Windows VM](docs/Pob/15_VM.md) | Running and driving the Windows shell in a VM, from a Mac |


License
-------

[Pob License](LICENSE) © 2026 Heyang Liu

You can fork this code and run it on your own machine for non-commercial use.
Commercial use of the software, or any part of it, is not permitted — that
includes use inside a company or in the course of paid work. Selling it,
renamed or not, and offering any product that competes with it, free of charge
or not, are not permitted either.

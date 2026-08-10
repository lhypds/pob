

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
- Record and replay operation macros, with the AI reading the screen where a step varies
- Work with MCP-compatible AI clients

The same bridge works for people, not only for AI: every instance runs a
server, so there is a web page you can open on your phone, and a desktop app
with a full-size keyboard and a trackpad — see [Pob Server](docs/09_Server.md),
[Web UI](docs/12_Web%20UI.md) and [Pob Keyboard](docs/13_Keyboard.md).


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

Press Record in the toolbar, do the thing once, and press Execute to have it
done again — the actions land in the instance's `macro.psl`, a
[macro](docs/03_Macro%20PSL.md) you can then open and edit. Anywhere the macro
should read the screen instead of repeating a value, write a `:: … ::` in its
place and the AI fills it in as the macro runs — a coordinate, a piece of text,
a true or false. Those are filled by [psl](https://github.com/lhypds/psl), the
Prompt Script Language compiler, so install that and give it an API key first.
Or drive the machine yourself from a phone, a terminal, or another computer.


Macro PSL
---------

A macro is a sequence of actions Pob plays back, and each instance keeps one —
`macro.psl`, in its `~/.pob/<instance>/` directory. It is written in **Prompt
Script Language**, PSL: one statement per line, run top to bottom, small enough
that a recording is readable and readable enough that the recording is worth
editing afterwards.

There are three kinds of statement. A **call** does something to the machine —
`move`, `click`, `drag`, `scroll`, `typeText`, `keyPress`, `sleep` — or something
to the run itself: `stop` ends it where it stands, and `call` replays another PSL
file before carrying on. An **if block** guards the statements inside it with a
condition. A **loop block** runs them again and again, up to a count. Any of the
three can hold an **AI slot**: a prompt written where a value would go,
`:: … ::`, filled in from a screenshot as the replay reaches it.

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
call("../sign-out.psl")
```

Comments are C's — `//` to the end of the line, `/* … */` across as many as it
takes — and are taken out of the line rather than out of the file, so a
statement is still found at the line number it was written on.

That is what separates PSL from a scripting language that only ever does what it
is told: a call is a macro repeating what it was given, and a slot is a macro
asking about a screen nobody could describe to it in advance, at the moment it is
looking at that screen.

The slots are filled by [psl](https://github.com/lhypds/psl), the Prompt Script
Language compiler — its own project, and where the models and the API keys live.
Pob holds none of its own. A macro with no slot in it never runs psl and needs
nothing installed; a macro that has one puts up **psl needed** before the cursor
moves if psl cannot be found.

A macro is read before it is played. Pob checks the whole file, and every file it
`call`s, when Execute is pressed, and a line it cannot read puts up **Macro
problems** with the line numbers instead of moving the cursor — `move(1)` is the
sort of thing that otherwise leaves the cursor where it was and the click under
it somewhere nobody chose. `pob macro --check` is the same reading from the
terminal, and works with Pob closed.

See [Macro PSL](docs/03_Macro%20PSL.md) for every statement, what a slot has to
come back as, and what happens to a line that cannot be read.


Documentation
-------------

| Doc | What's in it |
|-----|--------------|
| [Architecture](docs/01_Architecture.md) | How the brain and the native shells are split, and how they talk |
| [UI](docs/02_UI.md) | The window and every toolbar button |
| [Macro PSL](docs/03_Macro%20PSL.md) | `macro.psl` and the Prompt Script Language it is written in: recording and replaying, every statement, and the `:: … ::` slots psl fills in as it runs |
| [Key names](docs/04_Keys.md) | What `keyPress` / `key_press` accepts |
| [Logs](docs/05_Logs.md) | The `~/.pob/` tree: the instance directory and its sessions |
| [Settings](docs/06_Settings.md) | Every key in `settings.json` |
| [CLI](docs/07_CLI.md) | The `pob` command |
| [MCP Server](docs/08_MCP.md) | Driving Pob from Claude Code, Claude Desktop, Gemini CLI |
| [Pob Server](docs/09_Server.md) | The server every instance runs, and its address |
| [Operation API](docs/10_Operation%20API.md) | The HTTP command grammar for driving the machine |
| [Control API](docs/11_Control%20API.md) | The localhost API the `pob` CLI drives the app with |
| [Web UI](docs/12_Web%20UI.md) | The remote control page, for a phone |
| [Pob Keyboard](docs/13_Keyboard.md) | The desktop keyboard and trackpad client |
| [Development](docs/14_Development.md) | Building, the dev scripts, and cutting a release |
| [Windows VM](docs/15_VM.md) | Running and driving the Windows shell in a VM, from a Mac |

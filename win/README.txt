
Pob — Perception and Operation Bridge
=====================================


Pob lets an AI see your screen and work your mouse and keyboard, and lets you
do the same from a phone, a terminal, or another computer on your network.

This folder holds:

    Pob.exe             the app (self-contained — no .NET install needed)
    pob-core.exe        the agent core, started by the app
    Helpers\pob.exe     the `pob` command-line tool
    install.ps1         puts both somewhere permanent, with `pob` on your PATH
    VERSION             which release this is

Windows 10 or later. Try it here first if you like — Pob.exe starts the app
from this folder. If SmartScreen stops it, choose More info ▸ Run anyway.


1. Install
----------

Open PowerShell in this folder and run:

    powershell -ExecutionPolicy Bypass -File install.ps1

That copies the app to %LOCALAPPDATA%\Programs\Pob, adds Pob to the Start
menu, and puts its Helpers folder on your PATH. Everything is per-user, so
there is no administrator prompt. Use -InstallDir to choose another folder.

Open a NEW terminal afterwards — a PATH change only reaches terminals started
after it — and check:

    pob version


2. Install psl, and give it a key
---------------------------------

A macro that asks the AI anything — a ":: ... ::" slot — is filled by running
psl, the Prompt Script Language compiler. Pob makes no model calls of its own
and holds no API key. Install psl from https://github.com/pob/psl, then either
set a key:

    setx ANTHROPIC_API_KEY ...          REM or OPENAI_API_KEY

or write %USERPROFILE%\.pob\.pslrc naming the models and their keys — see
psl's README. A macro with no AI slot in it needs none of this.

Pob's own options live in settings.json there; the app's toolbar has a button
that opens it, or:

    notepad %USERPROFILE%\.pob\settings.json


3. Use the `pob` command
------------------------

    pob                     What is running: the instance and its sessions
    pob launch              Start the app
    pob status              Live status — executing, model, MCP, server address
    pob macro               Replay macro.psl
    pob stop                Stop the running session
    pob kill                Quit the running instance
    pob screenshot          Capture the screen; prints the file it saved
    pob sessions            List past sessions with duration and token usage
    pob mcp start           Start the MCP server for Claude Code, Gemini CLI…
    pob version             Print the version

The command on your PATH is the CLI, not the app: `pob` on its own reports
what is running, and `pob launch` is what opens the window. Both Pob.exe and
pob.exe are called "pob", which is why the CLI sits in Helpers\ — Windows
cannot keep two files of the same name in one folder.


Where your files live
---------------------

Everything is under %USERPROFILE%\.pob:

    settings.json           this machine's options (no API key: psl holds those)
    INSTANCE                which instance directory is the current one
    app.log                 what the app did
    <instance>\
        instance.log        lifecycle, macro steps, psl requests and responses
        macro.psl           recorded or hand-written actions
        screenshots\        captures from the Screenshot button
        logs\               sessions, AI slots, screenshots

Uninstalling never touches it.


Uninstall
---------

    powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall

Run it from this folder, or from %LOCALAPPDATA%\Programs\Pob. Delete
%USERPROFILE%\.pob too if you want your settings and history gone.


More
----

Documentation and source: https://github.com/lhypds/pob

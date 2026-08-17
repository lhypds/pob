
Pob — Perception and Operation Bridge
=====================================


Pob lets an AI see your screen and work your mouse and keyboard, and lets you
do the same from a phone, a terminal, or another computer on your network.

This folder holds:

    Pob.exe             the app (self-contained — no .NET install needed)
    pob-core.exe        the agent core, started by the app
    Helpers\pob.exe     the `pob` command-line tool
    install.ps1         puts both somewhere permanent, with `pob` on your PATH
    LICENSE             the terms this software is released under
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
    pob launch --start      Start the app and run its macro as soon as it is up
    pob --fullscreen        Start it over the whole screen, with no toolbar —
                            nothing on screen to click, so these commands drive it
    pob status              Live status — executing, model, MCP, server address
    pob check               Read the macro and this machine, and print what is wrong
    pob start               Replay the macro — what pob stop stops
    pob stop                Stop the running session
    pob kill                Quit the running instance
    pob screenshot          Capture the screen; prints the file it saved
    pob sessions            List past sessions with duration and token usage
    pob mcp start           Start the MCP server for Claude Code, Gemini CLI…
    pob update              Install the latest release over this one
    pob version             Print the version (pob -v too)

The command on your PATH is the CLI, not the app: `pob` on its own reports
what is running, and `pob launch` is what opens the window. Both Pob.exe and
pob.exe are called "pob", which is why the CLI sits in Helpers\ — Windows
cannot keep two files of the same name in one folder.


Where your files live
---------------------

Everything is under %USERPROFILE%\.pob:

    settings.json           this machine's options (no API key: psl holds those)
    INSTANCE                which instance directory is the current one
    app.log                 the app and its instances starting, stopping and failing
    <instance>\
        instance.log        lifecycle, macro steps, psl request source and results
        macro.psl           recorded or hand-written actions
        screenshots\        captures from the Screenshot button
        logs\               sessions, AI slots, screenshots

Uninstalling never touches it.


Update
------

    pob update              Install the latest release over this one
    pob update --check      Say whether there is a newer one, and change nothing

Quit Pob first: Windows will not let a running app be replaced. It downloads
the release, hands it to the install.ps1 that came with it, and installs over
the copy this pob came from — %USERPROFILE%\.pob is left as it stands.


Uninstall
---------

    powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall

Run it from this folder, or from %LOCALAPPDATA%\Programs\Pob. Delete
%USERPROFILE%\.pob too if you want your settings and history gone.


License
-------

Pob License — Copyright (C) 2026 Heyang Liu. Use and redistribution are
permitted for non-commercial use only. Commercial use of this software, or any
part of it, is not permitted — that includes use inside a company or in the
course of paid work. Selling it, renamed or not, and offering any product that
competes with it, free of charge or not, are not permitted either. See the
LICENSE file beside this one for the full terms.


More
----

Documentation and source: https://github.com/lhypds/pob

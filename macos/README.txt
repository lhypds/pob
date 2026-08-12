
Pob — Perception and Operation Bridge
=====================================


Pob lets an AI see your screen and work your mouse and keyboard, and lets you
do the same from a phone, a terminal, or another computer on your network.

This folder holds:

    Pob.app     the app, with the agent core (pob-core) and the `pob`
                command-line tool inside it
    LICENSE     the terms this software is released under
    VERSION     which release this is


1. Install the app
------------------

Drag Pob.app to your Applications folder, then open it.

macOS will ask for two permissions the first time, in System Settings ▸
Privacy & Security:

  - Accessibility     — to move the mouse and type
  - Screen Recording  — to see the screen

Grant both and reopen Pob. Nothing works without them.

The app is not notarized, so the first open may need a right-click ▸ Open
(or System Settings ▸ Privacy & Security ▸ Open Anyway).


2. Install the `pob` command
----------------------------

With Pob running, use the menu:

    Pob ▸ Install 'pob' Command…

That links the tool shipped inside the app at /usr/local/bin/pob, and asks
for your password if that folder needs it. The same menu item removes it
again later ("Uninstall 'pob' Command").

Open a new terminal and check it:

    pob version


3. Install psl, and give it a key
---------------------------------

A macro that asks the AI anything — a `:: … ::` slot — is filled by running
psl, the Prompt Script Language compiler. Pob makes no model calls of its own
and holds no API key. Install psl from https://github.com/pob/psl, then either
export a key:

    export ANTHROPIC_API_KEY=...        # or OPENAI_API_KEY

or write ~/.pob/.pslrc naming the models and their keys — see psl's README.
A macro with no AI slot in it needs none of this.

Pob's own options live in ~/.pob/settings.json; the app's toolbar has a button
that opens it, or:

    open ~/.pob/settings.json


4. Use the `pob` command
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

`pob` works whether you started Pob from the Dock or from the terminal — it
finds the running instance through ~/.pob and talks to it.


Where your files live
---------------------

Everything is under ~/.pob:

    settings.json           this machine's options (no API key: psl holds those)
    INSTANCE                which instance directory is the current one
    app.log                 the app and its instances starting, stopping and failing
    <instance>/
        instance.log        lifecycle, macro steps, psl request source and results
        macro.psl           recorded or hand-written actions
        screenshots/        captures from the Screenshot button
        logs/               sessions, AI slots, screenshots

Uninstalling never touches ~/.pob.


Uninstall
---------

Use Pob ▸ Uninstall 'pob' Command, quit Pob, and move Pob.app to the Trash.
Delete ~/.pob too if you want your settings and history gone.


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

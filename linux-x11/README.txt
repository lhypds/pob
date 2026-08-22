
Pob — Perception and Operation Bridge
=====================================


Pob lets an AI see your screen and work your mouse and keyboard, and lets you
do the same from a phone, a terminal, or another computer on your network.

This folder holds:

    pob             the app
    pob-core        the agent core, started by the app
    Helpers/pob     the `pob` command-line tool
    install.sh      puts both somewhere permanent, with `pob` on your PATH
    LICENSE         the terms this software is released under
    VERSION         which release this is


1. What it needs
----------------

  - An X11 session (Xorg). Under Wayland the app runs through XWayland but
    cannot see or drive native Wayland windows — log in to an Xorg session.

  - A running compositor, or the overlay cannot be transparent. Most desktops
    have one; on Raspberry Pi OS: sudo apt install xcompmgr && xcompmgr &
    
  - GTK 3 at runtime, preinstalled on mainstream desktops:
    sudo apt install libgtk-3-0 libjson-glib-1.0-0 libxtst6
    
  - A text editor and a file manager, for the toolbar buttons that open your
    settings and the logs folder. Most desktops already have both; a bare X
    session or a container may have neither, and the buttons then say so on
    screen. Any one of each will do — the lightest pair is:
    sudo apt install mousepad pcmanfm

Try it here first if you like — ./pob starts the app from this folder.


2. Install
----------

    ./install.sh                 just you      ~/.local/share/pob
                                               ~/.local/bin/pob
    sudo ./install.sh            everyone      /opt/pob
                                               /usr/local/bin/pob

Use --prefix DIR and --bin DIR to put them somewhere else.

If the installer says your bin directory is not on the PATH, add the line it
prints to ~/.profile (or ~/.bashrc, ~/.zshrc) and open a new terminal.

Then check it:

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

    $EDITOR ~/.pob/settings.json


4. Use the `pob` command
------------------------

    pob                     What is running: the instance and its sessions
    pob launch              Start the app
    pob launch --start      Start the app and run its macro as soon as it is up
    pob launch --fullscreen Start it over the whole screen, with no toolbar —
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
what is running, and `pob launch` is what opens the window.


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


Update
------

    pob update              Install the latest release over this one
    pob update --check      Say whether there is a newer one, and change nothing

Quit Pob first. It replaces the install this pob came from, wherever that is,
and keeps ~/.pob as it stands — so an install made with sudo (/opt/pob) wants
sudo pob update, and it says so if you forget.


Uninstall
---------

    ./install.sh --uninstall          (add sudo if you installed with sudo)

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

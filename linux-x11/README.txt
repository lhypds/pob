
Pob — Perception and Operation Bridge
=====================================


Pob lets an AI see your screen and work your mouse and keyboard, and lets you
do the same from a phone, a terminal, or another computer on your network.

This folder holds:

    pob             the app
    pob-core        the agent core, started by the app
    Helpers/pob     the `pob` command-line tool
    install.sh      puts both somewhere permanent, with `pob` on your PATH
    VERSION         which release this is


1. What it needs
----------------

  - An X11 session (Xorg). Under Wayland the app runs through XWayland but
    cannot see or drive native Wayland windows — log in to an Xorg session.
  - A running compositor, or the overlay cannot be transparent. Most desktops
    have one; on Raspberry Pi OS: sudo apt install xcompmgr && xcompmgr &
  - GTK 3 at runtime, preinstalled on mainstream desktops:
    sudo apt install libgtk-3-0 libjson-glib-1.0-0 libxtst6

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


3. Set your API key
-------------------

The first run creates ~/.pob/. Put your key and model in
~/.pob/settings.json — the app's toolbar has a button that opens it, or:

    $EDITOR ~/.pob/settings.json


4. Use the `pob` command
------------------------

    pob                     What is running: the instance and its sessions
    pob launch              Start the app
    pob status              Live status — executing, model, MCP, server address
    pob run "click Save"    Write that instruction and execute it
    pob start               Execute what is already in instruction.txt
    pob macro               Replay macro.txt
    pob stop                Stop the running session
    pob kill                Quit the running instance
    pob screenshot          Capture the screen; prints the file it saved
    pob sessions            List past sessions with duration and token usage
    pob mcp start           Start the MCP server for Claude Code, Gemini CLI…
    pob version             Print the version

The command on your PATH is the CLI, not the app: `pob` on its own reports
what is running, and `pob launch` is what opens the window.


Where your files live
---------------------

Everything is under ~/.pob:

    settings.json           your API key, model and options (this machine's)
    INSTANCE                which instance directory is the current one
    <instance>/
        instruction.txt     what you want done
        macro.txt           recorded or hand-written actions
        logs/               sessions, plans, steps, screenshots

Uninstalling never touches ~/.pob.


Uninstall
---------

    ./install.sh --uninstall          (add sudo if you installed with sudo)

Delete ~/.pob too if you want your settings and history gone.


More
----

Documentation and source: https://github.com/lhypds/pob

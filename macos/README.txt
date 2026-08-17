
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

(There is no need to download anything next time: one command does all of this
— the app, the `pob` command, and the quarantine flag below — and again for
every later version. Add --uninstall to take it back off.

    curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sh

The permissions in this section still have to be granted by hand.)

The app is not notarized, so that first open is blocked. Try it anyway, then
go to System Settings ▸ Privacy & Security — the Security section, under
"Allow applications from", then offers Open Anyway for Pob. On macOS 15 and
later that is the only route; control-click ▸ Open no longer works for an
unnotarized app. If macOS calls the app damaged instead, the download
quarantine flag is still on it:

    xattr -dr com.apple.quarantine /Applications/Pob.app

Then two permissions, both in System Settings ▸ Privacy & Security:

  - Accessibility     — to move the mouse and type
  - Screen Recording  — to see the screen

Screen Recording prompts for itself, the first time Pob captures anything.
Accessibility never prompts: open it, press +, and add Pob.app by hand. Until
you do, Pob looks like it is working — it draws its own cursor and walks it
around the screen — while every click it makes is dropped in silence.

Grant both and reopen Pob. Nothing works without them. Pob checks at every
launch and says so if either is missing, with a button that opens the right
pane — so you are told, rather than left with an app that looks fine.

macOS ties these grants to the exact app it was shown, and Pob is not signed
with a Developer ID, so replacing Pob.app with a newer version invalidates
them: both switches stay on in the list while clicking does nothing and
screenshots come back empty. That is what the launch message's "Reset
Permissions and Quit" button is for — it clears both grants so the copy you
reopen can be given them. By hand it is:

    tccutil reset All com.gcc3.pob            # both at once

    tccutil reset Accessibility com.gcc3.pob  # or one at a time
    tccutil reset ScreenCapture com.gcc3.pob


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


Update
------

    pob update              Install the latest release over this one
    pob update --check      Say whether there is a newer one, and change nothing

Quit Pob first: the app cannot be replaced while it is running. It replaces the
copy this pob came from, wherever that is, and keeps ~/.pob as it stands.

macOS ties the two permissions to the exact copy it was shown, and Pob is not
signed with a Developer ID — so after an update the switches stay on while
clicks are dropped and screenshots come back empty. Clear them and grant them
again:

    tccutil reset All com.gcc3.pob


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

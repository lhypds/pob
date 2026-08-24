
CLI
===

The `pob` command controls and inspects Pob from the terminal.

Every release ships the CLI beside the app in a `Helpers` folder — the app's
own executable is called `pob` too on Linux and `Pob.exe` on Windows, and a
case-insensitive filesystem cannot hold both in one directory. How it gets on
the `PATH` differs by platform:

| Platform | Install | What it does |
|----------|---------|--------------|
| macOS | **Pob → Install 'pob' Command…** in the app menu | Symlinks `Pob.app/Contents/Helpers/pob` at `/usr/local/bin/pob` (asks for an admin password when needed; the same menu item uninstalls it again) |
| Linux | `./install.sh` in the unzipped folder | Copies the app to `~/.local/share/pob` and links the CLI at `~/.local/bin/pob` — or `/opt/pob` and `/usr/local/bin/pob` under `sudo`. `--prefix` / `--bin` override both, `--uninstall` reverses it |
| Windows | `powershell -ExecutionPolicy Bypass -File install.ps1` | Copies the app to `%LOCALAPPDATA%\Programs\Pob`, adds its `Helpers` folder to the user `PATH` and puts Pob in the Start menu. No administrator prompt; `-Uninstall` reverses it, `-InstallDir` moves it |

Neither installer touches `~/.pob`, so settings, instances and logs survive an
uninstall and a reinstall.

The dev scripts build the CLI to `core/bin/pob` next to `pob-core` instead
(add that folder to your `PATH`, or call it by path).

Everything lives in `~/.pob`, created on first use and shared by the app and
the CLI: `settings.json` is the machine's, `INSTANCE` names the instance
directory, and that directory holds its `src/` macros, `instance.json` and
`logs/`.

A running Pob serves a small control API on an ephemeral localhost port,
advertised in `~/.pob/<instance>/control.json`; the CLI reads that file
and talks to that API — see **Control API** below. Log and session inspection
reads the log tree directly, so it also works when the app is not running.

```
Usage: pob [flags] [command] [args]

Flags:
  -v, --version      Print the Pob version, the same as the version command
  --session <id>     Target session; with no command, shows its details

Macro options (on start, check, and launch --start):
  --macropsl <file>  Work on that PSL file instead of the instance's own
                     src/main.macro.psl. A relative path is from the current
                     directory; a bare name not found there is looked for in
                     the instance's src/
```

`launch` takes four options of its own, `--start`, `--fullscreen`, `--msb` and
`--vncviewer`; everything else after it is the instance, so
`pob launch --start "Work laptop"` is that instance, started and running.

`--macropsl` says which file to work on. See **Another macro** below.

`--fullscreen` starts Pob over the whole screen with none of its own chrome on
it. See **Fullscreen** below.

`--msb` starts it on a Linux machine of its own instead of on this desktop,
`--count` says how many of those machines to start, and `--vncviewer` opens a
window on that machine's screen. See [Microsandbox](16_Microsandbox.md).

| Command | Description |
|---------|-------------|
| *(none)* | Show what is on this machine, and nothing about what it is doing. Three lines of it first — the version, `~/.pob`, and the `psl` [`settings.json`](06_Settings.md) names with where it was found or that it wasn't — and then the lists: every instance under `~/.pob` with its state and last run, the one `pob launch` would start marked `*`, and every [microVM](16_Microsandbox.md) microsandbox has, each with the instance running inside it and the `vnc://` address to watch its screen at, since that address is the only way to look at one. What the running instance is *doing* — executing, recording, the lock, the server addresses — is `pob status`, which is live; the sessions are `pob sessions`, since an instance a week old answers with hundreds of them. With `--session <id>`, that session instead — from whichever instance has it |
| `launch [instance]` | Start the app; fails if it is already running. With more than one instance and none named, it lists them and asks which to start — ↑/↓ (or `k`/`j`) to move, enter to start, a digit to pick a row outright, `q` to cancel; `<instance>` is a name or an id, which skips the list. The app is found next to the CLI — the surrounding bundle for `Pob.app/Contents/Helpers/pob`, the app beside `Helpers/` in a Linux or Windows install, the shell build outputs for `core/bin/pob` |
| `launch --start` | The same launch, and then the macro: as soon as the new instance's control API answers, the run `start` would have started is started on it. It is the one way to get from nothing running to a running macro in one command — `pob start` on its own has nothing to talk to until a launch has finished — which is what a cron entry or a login item needs. Combines with an instance: `pob launch --start "Work laptop"`, and with `--macropsl <file>` to run that file rather than the instance's own |
| `launch --fullscreen` | The same launch, over the whole screen: no toolbar, no window buttons, nothing on screen that is Pob's to click — see **Fullscreen** below. Both it and `--msb` are options of the launch and are read only after that word: `pob --fullscreen` on its own says where the flag goes rather than starting anything. Combines with everything else a launch takes: `pob launch --start --fullscreen "Work laptop"` |
| `launch --msb` | Start it in a microVM instead of on this desktop: a Linux machine with a screen nobody is looking at, Firefox in it, and a copy of this machine's `~/.pob` inside it, put there at boot. Nothing opens here and the pointer stays yours, which is what makes it the way to run a macro while the machine is being used — and every launch is a machine of its own, named `msb-xxxx` the way an instance is named `pb-xxxx`, so several runs go on side by side and a run that leaves a mess leaves it somewhere you can stop and forget. `POB_MSB_NAME=<name>` takes one machine over at every launch instead, for a script that needs the address to stay put. The launch prints the addresses to reach it at: a VNC view of the guest's screen, the [web UI](12_Web%20UI.md) and the [MCP server](08_MCP.md), all on `127.0.0.1`. Combines with `--start` and `--fullscreen`. Needs [microsandbox](https://microsandbox.dev) and Docker; the scripts it runs ship beside the app, and the Linux app it puts in the guest is built from a checkout or fetched from the release — see [Microsandbox](16_Microsandbox.md) |
| `launch --msb --count N` | How many of those machines this launch is. A `--msb` launch asks after the instance list — `How many VMs? [1]:`, where enter is the one machine almost always wanted — and `--count` is that answer given in advance, which is also how a script or a cron entry says it, since a launch with nobody at the keyboard starts one rather than asking. They come up one after another and not at once: each picks the host ports it publishes by looking for free ones, so each has to see the one before it. Every machine is its own — its own `msb-xxxx` name, its own ports, its own copy of the instance — so ten is ten Pobs running the same instance side by side, and `--start` starts the macro on each. `20` is the most one launch takes: every machine holds its own memory and disk (4G and 12G by default), so a `100` typed for a `10` would take the host down while looking like a launch — run the command again for more. If one of them will not start, the launch stops there and says which ones are up. Only with `--msb`: this desktop runs the one Pob, whatever the number says. Not with `POB_MSB_NAME`, which is one named machine replaced at every launch — ten of those would be that machine built ten times |
| `launch --msb --vncviewer` | The same launch, with a VNC viewer opened on the guest's screen as soon as Pob answers in it — so a `--msb` run can be watched rather than only reached at the address it prints. It looks for TigerVNC (`vncviewer` on the `PATH`, or the binary inside `TigerVNC.app`, where Homebrew's cask puts it and nothing on the `PATH`) and falls back to macOS's own Screen Sharing; `POB_MSB_VIEWER=<command>` names another, and is an opt-in on its own for a machine where every `--msb` launch should open one. The guest's screen has no password on it, so the window comes straight up — with the exception of Screen Sharing, which will not open a server that asks for nothing and wants a `POB_MSB_VNC_PASSWORD=<word>` launch instead. It is detached, so the launch finishes with the window still up. Only with `--msb`: the screen it opens on is that machine's. Combines with `--start`, and the window is up before the macro begins |
| `new [name]` | Create an instance — its own `src/` and `logs/`, on the machine's existing settings — under the name given, asking for one when it isn't. The new instance becomes the one `pob launch` starts next |
| `del`, `delete` `<instance>` | Delete an instance — the directory, its `src/` macros and its whole `logs/` tree. `~/.pob` is where those live and there is no other copy of them, so it says what is about to go (the sessions in it, what it comes to on disk, where it is) and asks; `--yes` answers that in advance, and a `del` with nobody at the keyboard refuses rather than assuming one. It will not delete an instance anything is running — here or in a [VM](16_Microsandbox.md) — since that leaves a Pob writing into a directory that is gone: `pob kill` first. Deleting the current instance moves `INSTANCE` to the most recently run of what is left, or clears it when there is nothing left, so the pointer never names a directory that isn't there. The two words are one command, and whichever one is typed is the one it answers with. The machine's `settings.json` is not an instance's and stays |
| `purge` | Everything Pob has on this machine, gone: every [microVM](16_Microsandbox.md) microsandbox is holding for it — running or stopped — removed with its disk, and every instance with its `src/` macros and its whole `logs/` tree. It is `del` for all of them at once, and it does the one thing `del` refuses to: what is running is stopped on the way rather than reported, since "all of it" is not an instruction that can be finished around whatever happens to be up. That makes the question before it the whole safeguard, so it is asked with the list of what is about to go — the VMs with their state and the instance in each, the instances with their sessions and what they come to on disk — and `--yes` answers it in advance; with nobody at the keyboard it refuses rather than assuming one. Nothing stops at the first thing that will not go: a sandbox microsandbox will not remove is said and counted, everything else still goes, and the exit status is `1` when anything was left behind. `INSTANCE` is cleared when the last instance goes, so the next `pob launch` starts a fresh one. It takes no name — one instance is what `del` takes — and what stays is the machine's own: `settings.json`, and the guest's image and app cached under `~/.pob/msb`, which are a download rather than anything of this machine's to lose. Sandboxes that are not Pob's are left where they are |
| `status` | Live status (executing, recording, the window's lock and click-through, psl, MCP, server address) |
| `sessions` | List sessions with their duration and how many slots each filled — every instance's, under a heading naming each, so the question "which run left this" is answered without pointing `INSTANCE` at one instance after another. Instances with no sessions are left out |
| `check` | Everything that has to be right before a run, in one report — see **Checking** below. `src/main.macro.psl` and the files it `call`s, line by line, and then this machine: psl, what psl fills a slot with, the app and the `pob-core` behind it, `settings.json`, `stop_hook`. It reads files and talks to no one, which makes it the command that answers with Pob closed; exits `1` when there is anything to fix. `--macropsl <file>` reads that file in its place |
| `start` | Execute [`src/main.macro.psl`](03_Macro%20PSL.md) on the running instance — the same run as the toolbar's Execute button, and what `stop` stops. With nothing running it says so and names `launch --start`, since starting the app is also called starting. `--macropsl <file>` runs that file in its place |
| `stop` | Stop the running session |
| `restart` | `stop` and `start` again, with the wait between them that makes the pair work: a stop is a cancel rather than a halt — the session ends when the step it is on comes back — and a `pob start` typed straight after a `pob stop` is refused by the run that has not finished going. `--macropsl <file>` runs that file in its place. The file the stopped run was of is not remembered, so this starts the run `pob start` would |
| `reset` | The same stop, and then the virtual cursor put back in the corner every replay starts from — the toolbar's Reset Mouse Position button. The two belong together: a stopped run leaves the cursor wherever it had got to, and resetting it under a live session is refused, since the run is what is driving it |
| `record start` / `record stop` | Record what you do at the machine into [`src/main.macro.psl`](03_Macro%20PSL.md) — the toolbar's Record button, and everything that follows a press of it: the window is locked, clicks are let through to the app underneath, and the state shows on the toolbar. Starting **appends and never clears**. The button asks which when there is something in the file already, and here there is nobody at the window to answer, so the answer is the one that cannot lose work |
| `lock on` / `lock off` | Hold the window to its size, or let go. Locked, a drag carries the windows underneath along with the frame, so a macro's coordinates keep landing where they were recorded; only the resize is taken away. The state is written to `instance.json`, so the next launch comes back the way this one was left, and `pob status` prints it |
| `clickthrough on` / `clickthrough off` | Whether a click on the overlay reaches the window underneath. On is the resting state — the overlay sits over the app it drives — and off is for working in Pob's own window. Written to `instance.json` like the lock |
| `kill`, `shutdown` | Quit the running instance. It is the shell app that is signalled — `pob-core` exits with the pipe to it, writing the instance's end time — and only when it does not go within 10s is anything killed outright. Nothing running is reported, not an error. The two words are one command: `shutdown` is what quitting an app is called, `kill` what is done to a process |
| `kill <name>` | Stop what that name is running. The names are the ones a bare `pob` lists: an **instance** — id or name — is stopped wherever it is running, this machine or a [microVM](16_Microsandbox.md) or both, and a **sandbox** name is that VM in particular. Stopping a VM is the machine and the Pob on it together, since the guest's workload *is* that Pob. Running in more than one place is said before it happens rather than found out after, with a line for each as it goes: naming an instance asks for that instance stopped, and where it happens to be running is something this can find rather than a question to put back. `--all` is every VM that is up — the Pob here is `pob kill` with nothing after it. Nothing running under that name is reported, not an error |
| `relaunch` | Quit the running instance and start it again, on the same instance and the same settings — the way back from a shell that is up but no longer doing what it should, a window left somewhere unreachable, or a permission granted since it started. One command rather than two because the wait between them has to be right: a launch started before the old instance has let go of its port would be refused as one already running. Nothing running is not an error; it launches. It is also how fullscreen is entered and left on a Pob that is already up, since neither is anything the running app can be talked into: `pob relaunch --fullscreen` brings it back over the whole screen, and a plain `pob relaunch` brings a fullscreen one back as an ordinary window |
| `screenshot` | Capture a screenshot; prints the saved file path |
| `mcp status` | Show MCP server info (URL, tools, client config snippet) |
| `mcp start [port]` | Register the MCP server in the user settings of installed agent CLIs (`claude`, `gemini`) and print its info. The server starts with the instance, so there is usually nothing to start; `[port]` moves it to that port first |
| `mcp stop` | Stop the MCP server and remove those registrations |
| `update` | Install the latest release over this install — see **Updating** below. `--version VER` installs that release instead, which is how one is reinstalled over itself or an older one gone back to; `--prefix DIR` installs somewhere else, `--bin DIR` (Linux and macOS) moves the symlink. Pob has to be closed |
| `update --check` | Print what is installed and what the latest release is, and install nothing. Exits `1` when there is a newer one, so a script can ask without reading the text |
| `version` | Print the Pob version. `pob -v` and `pob --version` print the same thing — they are read before the flags are parsed, so they answer wherever a version is asked for. What is printed is stamped into the CLI at build time, so a build from a checkout says `dev` |

Examples:

```
pob                                      # what is on this machine?
pob new "Work laptop"                    # create an instance and switch to it
pob launch                               # start the app (asks which, if there are several)
pob launch "Work laptop"                 # start that one
pob launch --start "Work laptop"         # start that one and run its macro
pob launch --fullscreen                  # start it over the whole screen, no toolbar
pob launch --msb                         # start it in a Linux microVM of its own
pob launch --msb --count 10              # …ten machines of it, without being asked
pob launch --msb --start                 # …and run the macro in there
pob launch --msb --vncviewer             # …and open a VNC window on its screen
pob kill msb-4f2a                        # shut that machine down again
pob kill pb-d7df                         # …or by the instance it is running
pob kill --all                            # …or every VM that is up
pob purge                                # every VM and every instance, gone
pob check                                # is the macro sound, and can this machine run it?
pob start                                # replay src/main.macro.psl; pob stop stops it
pob start --macropsl login.macro.psl     # replay that file instead
pob restart                              # stop that run and start it again
pob reset                                # stop it, and put the cursor back in its corner
pob record start                         # record what you do into src/main.macro.psl
pob record stop                          # and stop recording
pob lock on                              # hold the window to its size
pob clickthrough off                     # let the overlay take clicks again
pob relaunch                             # quit the app and start it again
pob --session 1752712400                 # session detail: the macro, conditions, usage
pob mcp start                            # register MCP with the agent CLIs here
pob update --check                       # is there a newer release?
pob update                               # install it over this one
```


Fullscreen
----------

`--fullscreen` starts Pob over the whole screen, and takes everything of its own
off it:

```
pob launch --fullscreen
```

No titlebar, no toolbar, no window buttons, no instance badge — nothing on
screen that is Pob's to click. What is left is the overlay itself: the
translucent wash, the virtual cursor, and the desktop underneath it, which is
now the whole of what Pob frames. Every click, drag and keystroke passes
straight through to the applications below, as it does under the ordinary
window, so the machine goes on working normally with Pob over it.

That leaves the terminal as the only way in, which is the point of the mode:

```
pob status                    # what it is doing
pob start                     # replay the macro
pob stop                      # stop that run
pob screenshot                # capture what it frames — now the whole screen
pob record start              # record what you do into the macro
pob kill                      # quit it
```

The [web UI](12_Web%20UI.md) and the [MCP server](08_MCP.md) reach it as they
always did — a fullscreen instance is an ordinary instance in every way except
what it draws.

What it is for is what the frame means. Every coordinate Pob works in is
relative to the content area, so an ordinary window says "drive this much of the
screen" and fullscreen says "drive the machine": screenshots come back as the
whole display, and the macro's coordinates are screen coordinates.

Some details worth knowing:

- **It covers the menu bar, the Dock, and the taskbar.** The window sits above
  them rather than beside them, on the display it came up on. Other displays are
  left alone.
- **It is a property of the run, not of the instance.** Nothing on disk
  remembers it: the next `pob launch` is an ordinary window again. The frame the
  window was left at, its lock and its click-through are not written to
  `instance.json` during a fullscreen run either, so an instance set up for a
  macro is still set up for it afterwards.
- **`clickthrough off` does not take hold.** A fullscreen window that took
  clicks would be one nothing could be done about — there is no button on it to
  hand the desktop back — so it goes on passing them through.
- **Getting out is a relaunch.** `pob relaunch` brings it back as an ordinary
  window; `pob relaunch --fullscreen` takes a running one the other way. Neither
  is something the app can be talked into while it is up: the mode is settled at
  launch, which is why it is a launch that changes it.


Another macro
-------------

An instance has one entry point, `src/main.macro.psl`, and that is what the
toolbar's Execute button runs. `--macropsl <file>` is how the terminal says
another one — `start` runs it, `check` reads it, `launch --start` starts the app
on it:

```
pob check --macropsl login.macro.psl && pob start --macropsl login.macro.psl
```

It exists because a macro is written one piece at a time, and the piece being
written is rarely the one `main` calls first. It is also how a macro gets to live
outside the instance: a file kept beside the thing it automates, or in a repo
next to the rest of a project, is run from where it is kept.

Which file it names:

| Written | Read as |
|---------|---------|
| `login.macro.psl` | That file in the current directory. Not there, and a bare name is looked for in the instance's `src/` — so the file beside `main.macro.psl` runs from wherever the command is typed. The current directory wins when both have one |
| `./flows/login.macro.psl`, `../login.macro.psl` | A path, from the current directory, and looked for nowhere else |
| `~/work/login.macro.psl` | Under the home directory, as `~` is read everywhere else it is written down |
| `/abs/login.macro.psl` | Itself |

A file that is not there ends the command before anything is started: it is the
whole of what was asked for, and a run without it would be a run of something
else. So is a flag misspelled — `pob start --macropslx f` is an error, not a
`pob start`, since a mistyped flag passed over would replay the instance's own
macro in answer to a command that named another file.

Everything downstream of the name follows the file rather than the instance. A
`call()` inside it names its own neighbours (see
[call](../Macro%20PSL/11_call.md)); its extension decides whether psl is started
for it at all, so a `.macro` named this way is replayed without the compiler
exactly as one in `src/` is; and the session keeps its copy of the macro under
that file's own name, so `pob --session <id>` says which file the run was of.

The flag names one file, not a directory or a set: what a run of several files is
remains a `call()`, written in the file that is started.


Checking
--------

`pob check` is the report to read before a run. It asks nothing of the app, so it
answers with Pob closed — the state a macro is written in, and the state a new
install is in.

Two groups come out of it, printed apart because they are fixed apart.

**The macro** — `src/main.macro.psl`, or whichever file `--macropsl` named, and
every file it `call`s, line by line.
This is the same reading Execute takes before the cursor moves (see
[When something is wrong](../Macro%20PSL/12_When%20something%20is%20wrong.md)),
so a macro this passes is a macro that will start: an unknown statement, a call
written with the wrong number of arguments, a `call()` naming a file that is not
there, a `/*` nobody closed, a `:: … ::` in a `.macro` that psl is never run for.

**This machine** — what a run needs of it besides the file:

| Checked | Why |
|---------|-----|
| `settings.json` parses | The app and the core read a file they cannot parse as an empty one, so one stray comma quietly puts every setting back to its default and nothing says so |
| psl is where `settings.json` says | A `:: … ::` is filled by running psl, and a macro with slots in it cannot start without one. Not checked for a macro that has none — psl is never started then, and the summary says as much |
| psl has something to fill with | A `.pslrc` in `~/.pob` or in your home directory, or a key in the environment. With neither, every slot fails at the moment the replay reaches it |
| The app, and `pob-core` behind it | The app is the window and core is what runs the macro. Both are looked for exactly where the CLI and the shells look for them, the checkout layouts included |
| The libraries the app is linked against | Linux only, by asking `ldd` — the same reading `get.sh` takes after an install, repeated because they also go missing later: an upgrade takes one away and the app stops starting with nothing on screen to say why |
| `stop_hook` names something that exists | It is started with a shell and never waited for, so a hook naming a command that is not installed is the quietest failure there is — the run ends, nothing announces it, and no log line says why |

```
$ pob check
Instance:   pb-b424 (Work laptop)
Macro:      /Users/you/.pob/pb-b424/src/main.macro.psl
psl:        /opt/homebrew/bin/psl
App:        /Applications/Pob.app
Core:       /Applications/Pob.app/Contents/MacOS/pob-core

main.macro.psl — 2 problems:
  line 1: move takes 2 arguments, and 1 was written
  line 4: call("sign-out.macro.psl") names /Users/you/.pob/pb-b424/src/sign-out.macro.psl, and there is no such file

2 problems to fix.
```

That summary is most of the answer when nothing is wrong: which psl and which
app were found is what `Nothing to fix.` is about.

The problems go to stderr and the summary to stdout, and the exit status is what
a script goes by — `0` when there is nothing to fix, `1` when there is — so it
can stand in front of a run:

```
pob check && pob launch --start
```


Updating
--------

`pob update` reinstalls Pob from its releases, over the install the `pob` that
ran it belongs to. No installer of its own is involved: on Linux and macOS it
fetches [`get.sh`](https://github.com/lhypds/pob/blob/master/get.sh) — the
script the one-line install pipes into `sh` — and runs it with `--version` and
`--prefix` filled in; on Windows, where there is no such script, it downloads
`Pob-<version>-windows-<arch>.zip` and runs the `install.ps1` inside it. Either
way what installs the release is the release's own installer, and the answer to
"what will this do to my machine" is the same one the README's install has.

Which release is the latest is read off the tag page GitHub redirects
`/releases/latest` to, so there is no API call and no token needed.

Three things the command adds to doing it by hand:

- **It installs over this copy.** The install is found from the CLI's own path
  — every install puts the CLI in a `Helpers` directory beside the app — so an
  app in `~/Applications` is replaced there rather than a second one appearing
  in `/Applications`. A `pob` that is *not* in such a directory is a build in a
  checkout, and the command says so instead of installing over it: there, `git
  pull` is the update. `--prefix DIR` overrides all of this.
- **It stops if Pob is running.** The app being replaced is the app running:
  Windows will not overwrite its files at all, and elsewhere it would be left
  as a live process with a different install underneath it. `pob kill` first.
- **It says when the install needs `sudo`** rather than failing halfway through
  a tree it cannot write — a root install (`/opt/pob`) is updated with `sudo pob
  update`.

On Unix the installer replaces the `pob` process rather than running as a child
of it, since that process lives inside the very bundle being taken apart; its
output and its exit status are the command's. On Windows nothing may delete a
running `.exe`, but anything may rename one, so the CLI moves itself to
`Helpers\pob.exe.old` before the installer writes the new one and the next
update sweeps the file up.

`~/.pob` is not touched by any of it — settings, instances, macros and logs
survive an update as they survive a reinstall. On macOS the Accessibility and
Screen Recording grants do not: they are tied to the exact copy of the app
macOS was shown, and Pob is not signed with a Developer ID, so a replaced app
keeps switches that look on while clicks are dropped in silence. The installer
prints the `tccutil reset All com.gcc3.pob` that clears them.


How it reaches the app
----------------------

The app owns `pob-core` as a child process and drives it over that process's
stdin and stdout, a pipe a `pob` typed into a terminal has no way to join. So
`pob-core` also serves the [Control API](11_Control%20API.md) on an ephemeral
`127.0.0.1` port, and advertises it by writing its pid and port into
`~/.pob/<instance>/instance.json`:

```json
{
  "id": "pb-b424",
  "name": "Work laptop",
  "start_time": 1752712400,
  "pid": 4711,
  "port": 57259
}
```

The port is whatever the OS hands out, so it is a different one on every
launch and the file is the only way to find it. The two fields are there only
while the instance runs — it stops advertising itself by clearing them, so a
file without a `port` is a stopped instance. `status`, `start`, `stop`,
`screenshot`, `lock`, `clickthrough`, `record` and the `mcp` commands are each
one call to that API; `restart` and `reset` are two, with a wait in between for
the run to actually stop; the rest read the `logs/` tree directly, which is why
they still work with the app closed. `check` is in neither group: it reads the
macro and this machine itself rather than asking the instance anything, which
is what lets a macro be checked while it is being written and an install before
it has ever been started.

The three that set the window — `lock`, `clickthrough` and `record` — do not
end at the core either. Those states belong to the window, so each is passed
on to the shell and does exactly what pressing the toolbar button does, icon
and `instance.json` included. Which is also how `pob status` answers for two of
them with no question asked of anybody: the shell writes the lock and the
click-through into `instance.json` as they change, so the file is where they
are read from — and they are still there with the app closed.

`kill` uses the API for one thing only — asking the instance which process it
is — and then works on the processes themselves. The pid it gets back is
`pob-core`'s, and core's parent is the shell that spawned it, so that is what
gets signalled; core follows it down when their pipe closes. The parent is
checked for the name the shell is built under (`Pob`, `pob`, `Pob.exe`) before
anything is sent to it, so a `pob-core` started by hand from a terminal takes
the signal itself rather than handing it to the terminal. `relaunch` is that
same stop, waited out until the port stops answering, and then the launch.


See also
--------

- [Macro PSL](03_Macro%20PSL.md) — what `pob start` runs and `pob check` reads
- [Control API](11_Control%20API.md) — the endpoints behind these commands
- [Logs](05_Logs.md) — the tree `pob` reads for session detail
- [MCP Server](08_MCP.md) — the server `pob mcp start` registers
- [Pob Server](09_Server.md) — the address `pob status` prints
- [Microsandbox](16_Microsandbox.md) — what `launch --msb` starts, and how to
  drive it from here

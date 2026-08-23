
Pob in a microVM
================

```
pob launch --msb
```

One command, and Pob is running on a Linux machine that is not this one: its own
kernel, its own screen, its own Firefox, and a copy of this machine's `~/.pob`
inside it. It takes a couple of minutes the first time — a Debian desktop is
built and handed over — and about five seconds every time after that. Nothing of
it is on your desktop: no window opens here, and the mouse stays yours.

That is the point of it. Pob drives a desktop by moving the pointer across it
and typing into whatever has focus, which is the same desktop you are working
on: a macro replayed while you use the machine is a fight over the pointer, and
a macro replayed at four in the morning needs the machine left logged in with
nothing on top. A guest solves both. It also makes a run *disposable* — the
sandbox is thrown away and made again at every launch, so a macro that opens the
wrong thing, installs something, or leaves a browser in a state leaves it in a
machine that will not exist in an hour.

This is the container answer to the same question
[Pob in a Windows VM](15_VMWare.md) answers for Windows. There, a full virtual
machine is unavoidable — a Windows container has no display driver and no
interactive desktop, so capture comes back black. On Linux, X11 is a socket:
`Xvfb` is a real X server with a real root window to capture and a real XTest to
inject into, so the whole desktop fits in a machine that boots in a second.
[microsandbox](https://microsandbox.dev) is what runs it — a microVM, so it is a
kernel of its own rather than a namespace of this one.


What you need
-------------

- **microsandbox**, which is one line and a check:

  ```
  curl -sSL https://get.microsandbox.dev | sh
  msb doctor
  ```

- **Docker**, which builds the guest's image — the one thing here that no
  release can ship ready-made, and which a macOS host already needs to build the
  Linux app at all (see [Development](14_Development.md)).

- **Nothing else.** Every install ships `vm/msb/` beside the app —
  `Pob.app/Contents/Resources/vm/msb` on macOS, `<install>/vm/msb` on Linux —
  and the CLI runs it from there. What differs is where the *Linux* app in the
  guest comes from, and the launch prints which of these it was:

  | | |
  |-|-|
  | A checkout | `linux-x11/dist/Pob`, built (`linux-x11/build_docker.sh`) if it isn't there or is the other architecture |
  | An installed `pob`, run from inside a checkout | that checkout's `linux-x11/dist/Pob`, when it is already built for this architecture — the app you are working on beats the released one, and nothing is built for a launch that did not ask |
  | An installed `pob`, anywhere else | `Pob-<version>-linux-<arch>.zip` from the release it is one of, unpacked under `~/.pob/msb/app/` once and kept |

  `POB_MSB_APP` names a `dist/Pob` outright and skips all of it — which is also
  the answer for a version that was never released.

- Disk for two copies of the image — about 1 GB in Docker and 270 MB in
  microsandbox's own store — a 12 GB writable disk for the guest, which is
  sparse and stays near empty, and 4 GB of memory while it runs.

macOS and Linux hosts, on Apple silicon or x86-64. A microVM runs the host's own
architecture, so that is what both the app and the image are built for; the
launch reads the architecture out of the Linux app it is about to hand the guest
and refuses one that cannot run there.


What the launch does
--------------------

```
$ pob launch --msb
📦 App:      /Users/you/code/pob/linux-x11/dist/Pob (arm64)
📦 Image:    pob-msb:latest
🚀 Starting the sandbox msb-4f2a (2 vCPU, 4G, 12G, 1024x768x24)…
⏳ Waiting for Pob to answer inside the VM…

Instance:   pb-b424 (pid 254)
Root:       /root/.pob
Executing:  no
Recording:  no
Locked:     yes
Clickthru:  on
psl:        /usr/local/bin/psl (not found)
MCP:        running — http://127.0.0.1:8032/sse
Server:     http://172.16.0.30:8033/

🖥  Screen:   vnc://127.0.0.1:5901   (no password)
🌐 Web UI:   http://127.0.0.1:8033/
🔌 MCP:      http://127.0.0.1:8032/mcp
```

In order: the Linux app for the guest's architecture, built if this is a checkout
and fetched from the release if it is an install; the guest's image, built (Docker's
cache makes that nothing when the `Dockerfile` has not changed) and handed to
microsandbox when it is one microsandbox does not have yet; the sandbox, started
with this machine's `~/.pob` mounted into it; and then the wait that makes this a
launch rather than a boot — `pob status` inside the guest, asked until it
answers, which is the same thing `pob launch` waits for on a desktop.

The status printed is the guest's own. From there it is an ordinary Pob that
happens to be somewhere else.


What is inside
--------------

A Debian machine with exactly enough desktop to be one:

| | |
|-|-|
| **Xvfb** | The screen. A real X server with no monitor on the end of it — `XGetImage` captures it and XTest injects into it, which is all Pob asks of a display |
| **openbox** | The window manager. Not decoration: [`launch()`](../Macro%20PSL/15_launch.md) asks the window manager to put an application's window in the frame, and a screen without one has nobody to answer that |
| **xcompmgr** | The compositor. Pob's overlay is a translucent window, and on X11 translucency is the compositor's doing — without one the overlay paints opaque over what it is meant to be a wash across |
| **x11vnc** | The way to look at that screen from here |
| **Firefox** | Something for a macro to drive. Debian ships it as `firefox-esr`; the image links it as `firefox` too, so `launch("firefox")` is what a macro writes |
| **xterm, mousepad, pcmanfm** | A terminal, a text editor and a file manager — the ones Pob's own toolbar reaches for when it opens a macro, `settings.json` or the instance folder, so those buttons work in here too. `vim` and `nano` are in the terminal. A guest's `settings.json` naming an editor it hasn't got (`iterm2`, `vscode` — it is a copy of this machine's) falls down the same chain and lands on these |
| **Fonts** | DejaVu and Liberation for the faces the web asks for by name, Noto CJK for Chinese, Japanese and Korean, and Noto's colour emoji. A base image ships almost none of this, and a page whose characters have no font draws them as empty boxes — which a macro can read no better than you can |

There is no panel and no desktop icon in there, so **right-click the background**
for the menu: Terminal, Text editor, Firefox, and the instance folder. It is
written by `run.sh` at every boot, which is where to add to it.

Three read-only mounts carry everything of Pob's in, and nothing is baked into
the image:

| Host | Guest | |
|------|-------|-|
| `~/.pob` | `/mnt/pob-home` | Copied to `/root/.pob` at boot — the settings, the instances and the macros this machine has right now |
| `linux-x11/dist/Pob` | `/mnt/pob-app` | The app, run from where it is mounted |
| `vm/msb` | `/mnt/pob-vm` | `run.sh`, which is the sandbox's whole workload |

So a rebuilt app or an edited `run.sh` takes effect on the next launch, and the
image is rebuilt only when the desktop under it changes. `~/.pob` is *copied*
rather than linked, and copied *in* only: what the guest writes — its logs, its
sessions, its screenshots — stays in the guest and goes when the guest does.


The screen it comes up on
-------------------------

The guest's screen is **1024x768**. It is small enough that the viewer opens a
window rather than most of a display, and it is the size to write a macro
against unless there is a reason not to.

`POB_MSB_GEOMETRY=1920x1200x24` says it yourself, and is what to do when the
macro expects a particular display.

The frame has to fit on it. Every coordinate in a macro is measured from inside
Pob's frame, so the frame has to come up the size it was recorded at — and a
frame taller than the guest's screen is clipped by it, which is clicks past the
bottom edge landing on nothing and screenshots coming back short. The launch
reads the window the instance was left at out of its `instance.json` and says
when it will not fit, with both ways out: a window of **904x608** or smaller is
what 1024x768 holds once the title bar, the toolbar above it and a margin are
taken off, and `POB_MSB_GEOMETRY` is the other direction. It does not resize
anything itself — growing the screen would be the launch not doing what it was
asked, and shrinking the window would move every coordinate in the macro.


Watching it, and driving it
---------------------------

Three ways in, all of them on `127.0.0.1` and none of them on the network the
guest is on:

```
vncviewer 127.0.0.1::5901          # the guest's screen, live
open http://127.0.0.1:8033/        # the web UI (Web UI)
msb exec msb-4f2a -- pob status     # the CLI, inside the guest
```

The launch prints those addresses, and `pob` — the bare command — lists them
again afterwards, which is what to run when the terminal that printed them is
long gone:

```
VMs:
   SANDBOX        INSTANCE   STATE     SCREEN
   msb-4f2a       pb-d7df    running   vnc://127.0.0.1:5901
   msb-91c7       pb-91ab    running   vnc://127.0.0.1:5902
```

The port comes from microsandbox itself — `msb inspect <name>
--format json`, which is the authority on what a sandbox was actually started
with, mappings moved by a port already in use included. The instance column is
the one thing it cannot answer: the guest reads that from the copy of
`INSTANCE` that went in with it, so each launch leaves a note of what it sent
in `~/.pob/msb/vms/<name>.json`. A VM started by hand has none, and lists with
a dash there.

`--vncviewer` does the first of those as part of the launch, for a run that is
meant to be watched:

```
pob launch --msb --vncviewer          # a machine, a Pob on it, and a window on it
pob launch --msb --start --vncviewer  # …with the window up before the macro begins
```

It opens **TigerVNC** where there is one — `vncviewer` on the `PATH`, or the
binary inside `TigerVNC.app`, which is where Homebrew's cask puts it and it puts
nothing on the `PATH` — and falls back to macOS's Screen Sharing, which is the
one viewer the guest's screen is not open to (see below). There is nothing to
sign in with, so the window comes straight up on the screen.
The window is detached from the launch, so the command finishes and the window
stays; what a viewer that did not connect had to say is in
`~/.pob/msb/viewer.log`. `POB_MSB_VIEWER=<command>` names a different viewer —
`remmina`, a `vncviewer` of your own — and set on its own it is the opt-in
without the flag, for a machine where every `--msb` launch should open one.

```
brew install --cask tigervnc     # macOS
apt install tigervnc-viewer      # Debian, Ubuntu
```

The viewer is looked for before the image is built, so a machine without one
hears about it in the first second of the launch rather than after the boot.

**There is no VNC password**, and none is needed to keep anyone out — the port
is published to `127.0.0.1` and the guest's network is not on the LAN, so
whoever can reach it is already at this machine. What one would buy is a dialog
between a launch and the screen it just made. TigerVNC, RealVNC and Remmina all
take a server offering no authentication and open straight onto it:

```
vncviewer 127.0.0.1::5901        # TigerVNC — the address the launch printed
```

On macOS, run TigerVNC by its path inside the bundle —
`/Applications/TigerVNC.app/Contents/MacOS/vncviewer` — rather than through the
`vncviewer` Homebrew links onto the `PATH`: started through the symlink it has
no bundle to read its `Info.plist` from, and paints the guest's screen into a
quarter of its own window (see [When something is
wrong](#when-something-is-wrong)). `--vncviewer` does this for you.

The exception is macOS's **Screen Sharing, which will not open a server that
offers no authentication**: it answers with *"Screen Sharing requires a password
to sign in to 127.0.0.1:5901"* and there is no password to type. A server
offering VNC authentication is one it signs into, so that is the one viewer worth
putting a password on for:

```
POB_MSB_VNC_PASSWORD=pob pob launch --msb
open vnc://:pob@127.0.0.1:5901
```

With a password set, `--vncviewer` writes it where the viewer can read it —
`~/.pob/msb/vnc-passwd`, in the format every VNC viewer has read since AT&T's —
and signs the window in itself.

The `pob` command *inside* the guest is the one that can drive it: the
[Control API](11_Control%20API.md) is loopback-only, and the guest's loopback is
not this machine's. So it goes through `msb exec`, which is the guest's terminal:

```
msb exec msb-4f2a -- pob start                  # replay the macro
msb exec msb-4f2a -- pob start --macropsl f     # replay that one
msb exec msb-4f2a -- pob screenshot             # capture the guest's screen
msb exec msb-4f2a -- pob stop
msb exec -t msb-4f2a -- bash                    # a shell in the VM
msb logs msb-4f2a                               # what the desktop printed
pob kill msb-4f2a                               # shut the machine down
pob kill --all                                 # …every one that is up
```

The last two are Pob's own words for `msb stop`, which still says it too. What
`pob kill` takes is a name out of the listing a bare `pob` prints, and either
column of it will do: the sandbox — `pob kill msb-4f2a` — or the instance
running inside it, `pob kill pb-d7df`, which stops that instance wherever it is,
this machine included. Bare, with no name at all, `pob kill` is the Pob on this
desktop and never a VM.

`pob launch --msb --start` does the first of those as part of the launch, which
is the one command a cron entry or a CI step wants: a machine, a Pob on it, and
the macro running, from nothing.

The [Operation API](10_Operation%20API.md), the [web UI](12_Web%20UI.md) and the
[MCP server](08_MCP.md) all reach the guest on the forwarded ports, exactly as
they reach any other machine running Pob — which is the whole point of them
being ports. The host side of each is the number the guest uses when that number
is free here, and the next one up when it is not; the launch prints what it
picked. Nothing published leaves this machine, and neither the VNC nor the Pob
server asks for a password: they are as open as the machine they are reachable
from, and that machine is this one.


What is not in there
--------------------

**psl, and a key for it.** The guest has no psl, so a macro with `:: … ::` slots
in it fails at the slot — `pob check` inside the guest says so, and so does the
`psl: (not found)` line the launch prints. Install it in the guest and give it a
key if a macro needs one:

```
msb exec -t msb-4f2a -- bash        # then install psl as you would anywhere
```

That lasts as long as the sandbox, which is until the next launch. A macro that
needs psl every time wants it in `vm/msb/Dockerfile`, next to Firefox.

**Anything else a macro opens.** Firefox is in the image because a browser is
what most macros drive. `launch("libreoffice")` in the guest needs
`libreoffice` in the image.


Its lifetime
------------

**Every launch is a machine of its own**, named the way an instance is: a launch
draws `msb-<4 hex>` — `msb-4f2a`, `msb-91c7` — beside the `pb-<4 hex>` in
`~/.pob`, from the same two bytes of randomness core draws an instance id from.
So a second `pob launch --msb` stands beside the first instead of taking its
place, and several Pobs can be driven at once from one checkout. The launch
prints the name it drew and every command for that machine, and says how many
were already up before it: each one holds its own memory and disk, and a host
runs out of both without ever pointing here.

Drawn rather than counted, because a number would have to mean something. "The
second machine up" stops being true the moment the first one is stopped, and a
name that is only ever itself is one you can read off a launch from an hour ago
and still type. Nothing takes a name back, either: a stopped machine keeps its
own until `msb rm <name>` is asked for it, so `msb list` is a history of the
machines that were and `pob` is the list of the ones that are.

`POB_MSB_NAME=<name>` asks for one machine instead of a new one, and **that one
is replaced at every launch** — `msb run --replace` — because its state is a
copy of this machine's and is made again from it in seconds. It is the shape to
use from a script: the address stays the same across launches, which is what
lets the next line be an `msb exec` against it. What survives a launch is on
the host either way, which is where you were editing it anyway.

The VM is up for exactly as long as Pob is: `run.sh` waits on the app, so
`msb exec msb-4f2a -- pob kill` ends the machine rather than leaving a desktop
with nobody on it. That one answers
`error: runtime error: exec session ended without exit event`, which is not a
failure — it is what killing the machine you are talking *through* looks like
from here; `msb ls` says `stopped` afterwards. `pob kill <name>` is the same end
said from this side, and the one to reach for: it names the machine out of the
same listing `pob` prints, and it does not have to talk through the guest to
end it.

Nothing about this touches the Pob on your own desktop. Launch one here and one
in a VM if you like — they share the `~/.pob` the guest copied at boot and
nothing after that.


When something is wrong
-----------------------

- **Black frames.** Almost always the compositor: Pob writes
  `No compositor — transparency unavailable` across its own window when there
  isn't one, and the frame under an opaque overlay is black. `msb logs msb-4f2a`
  should show `starting xcompmgr`; a guest where it died is one to look at with
  `msb exec -t msb-4f2a -- bash`.
- **The guest is not running the app you just built.** The `📦 App:` line the
  launch prints says which app went in and where it came from: `this checkout`,
  `the checkout you are in`, or `release <version>, not a build of yours` — that
  last one is an installed `pob` fetching the release, which is the right answer
  everywhere except when you are testing a change to the shell. Run `--msb` from
  inside the checkout (any directory under it will do), or name the build:
  `POB_MSB_APP=~/code/pob/linux-x11/dist/Pob pob launch --msb`.

  Which directory you are in is the whole of it: an installed `pob` looks for a
  `linux-x11/dist/Pob` above the *current* directory, so the same command run
  from `~` fetches the release and run from the checkout hands over your build.
  A change made and built and still not in the guest is almost always this.

  The release side of that is unpacked into `~/.pob/msb/app/<version>-<arch>`
  once and kept, and a version number cannot tell a published 0.2.14 from a
  0.2.14 you have built since — so once that directory exists, every launch from
  outside a checkout keeps handing the guest the published one. The launch says
  so when the unpack is older than the Pob asking for it. `export
  POB_MSB_APP=~/code/pob/linux-x11/dist/Pob` in your shell is the way to stop
  thinking about it while you are working on the shell.
- **Screen Sharing will not connect — "requires a password to sign in".** The
  guest's screen asks for nothing, and that is the one thing Screen Sharing
  cannot open. Use TigerVNC (`vncviewer 127.0.0.1::5901`), or launch with a
  password for it: `POB_MSB_VNC_PASSWORD=pob pob launch --msb`, then
  `open vnc://:pob@127.0.0.1:5901`.
- **The guest's screen fills a quarter of the TigerVNC window, in the bottom-left
  corner, with black around it.** On macOS, TigerVNC started *through a symlink*
  — `brew install --cask tigervnc` links the binary into `/opt/homebrew/bin`, so
  plain `vncviewer` is usually one — runs with no bundle around it, and the
  `NSHighResolutionCapable=false` in the app's `Info.plist` is what nothing
  reads. The viewer then lays its window out in points and paints the screen one
  framebuffer pixel to one device pixel, which on a Retina display is half the
  size in each direction. Start it by its real path instead:

  ```
  /Applications/TigerVNC.app/Contents/MacOS/vncviewer 127.0.0.1::5901
  ```

  `--vncviewer` resolves the symlink itself, so a launch that opens the window
  is unaffected; this is the by-hand command. A window already up cannot be
  talked round — resizing it does not repaint it at the right size — so it is
  the next one that comes up right.

  To keep typing `vncviewer`, put a wrapper that does the same thing earlier on
  the `PATH` than Homebrew's link — `~/.local/bin/vncviewer`, two lines:

  ```sh
  #!/bin/sh
  exec /Applications/TigerVNC.app/Contents/MacOS/vncviewer "$@"
  ```

  The real path can come up in a quarter too, and then it is the bundle that
  macOS has lost track of rather than the path. Registering it again is the
  whole fix, and it lasts:

  ```
  /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f /Applications/TigerVNC.app
  ```
- **`--vncviewer` says there is no VNC viewer to open.** It found neither TigerVNC
  nor, off macOS, anything to fall back on — install one (`brew install --cask
  tigervnc`, `apt install tigervnc-viewer`) or name the one you have with
  `POB_MSB_VIEWER`. The launch itself is unaffected: without the flag it prints
  the address and opens nothing.
- **`--vncviewer` opened a window that asks for a password.** Only a launch given
  a `POB_MSB_VNC_PASSWORD` can: the password file could not be written and the
  launch said so — type the password, or drop the variable and the screen asks
  for nothing.
- **Clicks landing high, screenshots coming back short.** The frame is taller
  than the guest's 1024x768 screen and is being clipped by it — the launch says
  so when it starts. Shrink the window to 904x608 or smaller, or raise the
  screen with `POB_MSB_GEOMETRY`.
- **Empty boxes where the text should be.** A font is missing for those
  characters. The image carries CJK and emoji; anything past that — Arabic,
  Devanagari, Thai — is a `fonts-noto-*` package in `vm/msb/Dockerfile` away.
- **`launch()` opened it but did not place it.** openbox is not running; the log
  says whether it started.
- **The launch waits and then gives up.** It prints the last of `msb logs` when
  it does, and leaves the machine up on purpose — `msb exec -t msb-4f2a -- bash`
  is the way in.
- **`msb doctor` is not happy.** Nothing here will boot until it is. On macOS it
  wants a recent host and its own `libkrunfw`; the installer above puts that
  there.


The knobs
---------

All of them environment variables on the launch, because all of them are
answers to "not on this machine" rather than settings of Pob's:

| | |
|-|-|
| `POB_MSB_NAME` | The one machine to be, replacing whatever is under that name. Unset — the default — is a new machine each launch, under a name drawn as `msb-<4 hex>` |
| `POB_MSB_CPUS`, `POB_MSB_MEMORY`, `POB_MSB_DISK` | `2`, `4G`, `12G` |
| `POB_MSB_GEOMETRY` | The screen, `WIDTHxHEIGHTxDEPTH`. Default: `1024x768x24` |
| `POB_MSB_VNC_PORT`, `POB_MSB_WEB_PORT`, `POB_MSB_MCP_PORT` | The host side of each mapping. Default: the guest's own number, or the next free one |
| `POB_MSB_VNC_PASSWORD` | What a viewer signs in with. Unset, the default, is no password at all; set one for macOS's Screen Sharing, the one viewer that will not open a server that asks for nothing |
| `POB_MSB_VIEWER` | `1` to open a viewer on the guest's screen when Pob answers — what `--vncviewer` sets — or the command to open it with. `0`, the default, opens nothing and prints the address |
| `POB_MSB_IMAGE` | The image tag, `pob-msb:latest` |
| `POB_MSB_APP` | A Linux `dist/Pob` directory to run in the guest, instead of building or fetching one |
| `POB_MSB_REBUILD` | `1` to build the image with Docker's layer cache thrown away, which is how it picks up newer packages. An edited `Dockerfile` needs nothing: the build runs at every launch and is the cache doing nothing when there is nothing to do |
| `POB_MSB_SKIP_BUILD` | `1` to use `linux-x11/dist/Pob` exactly as it is |
| `POB_MSB_WAIT` | Seconds to wait for Pob to answer, `240` |

`vm/msb/launch.sh` is the script all of this is, and it runs on its own if the
`pob` command is not to hand — from a checkout, or from where the install keeps
it. Run that way it needs `POB_MSB_VERSION` to know which release to fetch the
guest's app from, since that is what the `pob` command tells it.


See also
--------

- [Pob in a Windows VM](15_VMWare.md) — the same idea where a container cannot
  go, and the desktop problems a Windows guest has that this one does not
- [CLI](07_CLI.md) — `launch` and everything else the guest's `pob` answers
- [launch](../Macro%20PSL/15_launch.md) — the statement that opens Firefox in
  there and puts it in the frame
- [Development](14_Development.md) — the Docker builds this shares
- [Operation API](10_Operation%20API.md) — what the forwarded port serves

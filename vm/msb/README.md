
Pob in a microVM
================

`pob launch --msb` — Pob on a Linux machine of its own, with this machine's
`~/.pob` copied into it and Firefox installed for a macro to drive. Nothing
opens on your desktop and the pointer stays yours.

The written-out version of all of this, including what to do when it goes wrong,
is [docs/Pob/16_Microsandbox.md](../../docs/Pob/16_Microsandbox.md). This is the
directory it talks about.

```
pob launch --msb              # a machine, a Pob on it
pob launch --msb --start      # …and the macro running
pob launch --msb --vncviewer  # …and a VNC window open on its screen
bash vm/msb/launch.sh         # the same thing without the pob command
```

| File | |
|------|-|
| `launch.sh` | The host's half: the Linux app for the guest's architecture, the image, the ports, the sandbox, and the wait for Pob to answer inside it. Everything `pob launch --msb` does, it does by running this |
| `Dockerfile` | The guest's desktop — Xvfb, openbox, xcompmgr, x11vnc, Firefox, and the libraries the GTK shell is linked against. Nothing of Pob's own |
| `run.sh` | The sandbox's workload: `~/.pob` brought over, the screen made, Pob started on it. The VM is up for exactly as long as this script is |

State lives in `~/.pob/msb/`: `loaded-image` is the Docker image id last handed
to microsandbox, so a launch with nothing to load does not load a gigabyte, and
`app/<version>-<arch>/` is the guest's Linux app when it was fetched from a
release rather than built here. `--vncviewer` leaves `viewer.log` there too,
what the viewer it opened had to say (and `vnc-passwd`, the sign-in file a
viewer wants, only on the launch that was given a `POB_MSB_VNC_PASSWORD`).

Pob itself is not in the image. The app, this directory and `~/.pob` go in as
read-only mounts at boot, so a rebuilt app (`linux-x11/build_docker.sh`) or an
edited `run.sh` takes effect on the next launch and the image is rebuilt only
when the desktop under it changes.

Needs [microsandbox](https://microsandbox.dev) (`curl -sSL
https://get.microsandbox.dev | sh`, then `msb doctor`) and Docker — a macOS host
needs Docker for the Linux app anyway.

These four files ship with every install too — `Pob.app/Contents/Resources/vm/msb`
on macOS, `<install>/vm/msb` on Linux — and the CLI runs whichever copy it
belongs to. From an install there is nothing to build the guest's Linux app
with, so it is fetched from the release that install is one of and kept in
`~/.pob/msb/app/`; `POB_MSB_APP=DIR` names a `dist/Pob` of your own instead.

Once it is up:

```
vncviewer 127.0.0.1::5901        # watch the guest's screen (the launch prints the port)
open http://127.0.0.1:8033/      # the web UI
msb exec msb-4f2a -- pob start    # replay the macro
msb exec -t msb-4f2a -- bash      # a shell in the VM
msb logs msb-4f2a                 # what the desktop printed
pob kill msb-4f2a                 # shut the machine down (msb stop msb-4f2a too)
pob                              # every instance and VM, with each VM's screen
```

Each launch is its own machine: unnamed, it takes the first of `msb-4f2a`,
`msb-4f2a-2`, … that no running sandbox holds, so a second `pob launch --msb`
comes up beside the first with its own screen and ports (the launch prints
both, and the name to put in the commands above). `POB_MSB_NAME=<name>` asks
for one machine instead, replacing it at every launch — the shape to use from a
script, where the address has to stay put. `msb list --running` is the roll
call; each machine holds its own 4G and 12G until it is stopped.

#!/bin/bash
# What the microVM runs, from boot to a Pob you can look at: this machine's
# ~/.pob brought over, a screen made for it, a window manager put on that
# screen, a way in for a viewer, and then Pob itself.
#
# It is the image's CMD, so it is the sandbox's workload: the sandbox is up for
# exactly as long as this script is, and everything it prints is what
# `msb logs pob-msb` shows. It is started by launch.sh on the host and is not
# meant to be run anywhere else — three read-only mounts are the whole of what
# it needs, and all three come from there.
#
# Environment, all set by launch.sh:
#   POB_MSB_GEOMETRY    Xvfb screen, WIDTHxHEIGHTxDEPTH  (default 1440x900x24)
#   POB_MSB_FULLSCREEN  1 to start Pob over the whole screen
#   POB_MSB_VNC_PORT    port x11vnc serves that screen on (default 5900)
#   POB_MSB_VNC_PASSWORD  what a viewer signs in with; empty for no password
#   POB_MSB_APP_DIR     the Pob app mount                (default /mnt/pob-app)
#   POB_MSB_HOME_MOUNT  the host's ~/.pob, read-only      (default /mnt/pob-home)

set -u

GEOMETRY="${POB_MSB_GEOMETRY:-1440x900x24}"
FULLSCREEN="${POB_MSB_FULLSCREEN:-0}"
VNC_PORT="${POB_MSB_VNC_PORT:-5900}"
VNC_PASSWORD="${POB_MSB_VNC_PASSWORD-pob}"
APP_DIR="${POB_MSB_APP_DIR:-/mnt/pob-app}"
HOME_MOUNT="${POB_MSB_HOME_MOUNT:-/mnt/pob-home}"

export DISPLAY="${DISPLAY:-:99}"
export HOME="${HOME:-/root}"
POB_HOME="$HOME/.pob"

log() { echo "[pob-msb] $*"; }
die() { log "$*"; exit 1; }

# The display number Xvfb is told is the one in DISPLAY, so the two cannot
# drift apart: everything below — the window manager, the viewer, Pob — finds
# the screen through that one variable.
SCREEN="$DISPLAY"

# Everything started here is a child of this script, and the sandbox stops when
# this script does, so the cleanup is about the other order: a Pob that exits on
# its own should not leave a VM up with a desktop and nobody on it.
SERVICE_PIDS=()
cleanup() {
    trap - TERM INT EXIT
    for pid in "${SERVICE_PIDS[@]:-}"; do
        [ -n "$pid" ] && kill "$pid" 2>/dev/null
    done
}
trap cleanup TERM INT EXIT

# ── the instance, brought over from the host ─────────────────────────────────
# The mount is read-only and Pob writes to ~/.pob constantly, so this is a copy
# and not a link. It runs at every boot, which is what makes the guest's macros
# and settings the ones on the host right now — and it is one-way: nothing the
# guest writes goes back.
if [ -d "$HOME_MOUNT" ]; then
    mkdir -p "$POB_HOME"
    if cp -a "$HOME_MOUNT/." "$POB_HOME/" 2>/dev/null; then
        log "copied the host's ~/.pob ($(du -sh "$POB_HOME" | cut -f1))"
    else
        die "could not copy $HOME_MOUNT into $POB_HOME"
    fi
    # The app log came over with it, and it is the host's account of the host's
    # runs. What this Pob writes belongs under its own name, so the two are
    # never read as one log with a gap in the middle.
    if [ -f "$POB_HOME/app.log" ]; then
        mv -f "$POB_HOME/app.log" "$POB_HOME/app.host.log"
    fi
else
    log "no ~/.pob mounted at $HOME_MOUNT — starting on a fresh instance"
    mkdir -p "$POB_HOME"
fi

[ -x "$APP_DIR/pob" ] || die "no Pob app at $APP_DIR — is $APP_DIR mounted?"

# GTK wants a runtime directory and complains about every missing thing in it.
# The image sets the variable; this is for the run that has it from somewhere
# else, or from nowhere.
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/0}"
mkdir -p "$XDG_RUNTIME_DIR" 2>/dev/null && chmod 700 "$XDG_RUNTIME_DIR" 2>/dev/null

# ── the screen ───────────────────────────────────────────────────────────────
# -s 0 and -dpms are the point of a headless screen: a blanked X server hands
# back black frames, which would look exactly like Pob capturing nothing.
log "starting Xvfb on $SCREEN at $GEOMETRY"
Xvfb "$SCREEN" -screen 0 "$GEOMETRY" -nolisten tcp -s 0 -dpms &
SERVICE_PIDS+=($!)

for _ in $(seq 50); do
    xdpyinfo -display "$SCREEN" >/dev/null 2>&1 && break
    sleep 0.2
done
xdpyinfo -display "$SCREEN" >/dev/null 2>&1 || die "Xvfb did not come up on $SCREEN"

# ── the window manager ───────────────────────────────────────────────────────
# Not decoration: `launch()` asks the window manager to place the application's
# window in the frame, and a screen without one is a screen where that request
# has nobody to answer it.
#
# The menu is the other half of that. There is no panel, no dock and no desktop
# icon in here, so a right-click on the background is the only way to open
# anything at all from a VNC session — and openbox's own default menu is a
# Debian one that this image has no file for.
#
# Debian's rc.xml names two menu files: this one, which is found here because a
# user's copy comes first, and a generated /var/lib/openbox/debian-menu.xml that
# an image with no Debian menu system never has. An empty stand-in for the second
# is worth writing: without it every start prints a line about a missing file
# into a log that is otherwise six lines about starting Pob.
mkdir -p /var/lib/openbox
[ -f /var/lib/openbox/debian-menu.xml ] || cat > /var/lib/openbox/debian-menu.xml <<'DEBIANMENU'
<?xml version="1.0" encoding="UTF-8"?>
<openbox_menu xmlns="http://openbox.org/3.4/menu"/>
DEBIANMENU

mkdir -p "$HOME/.config/openbox"
cat > "$HOME/.config/openbox/menu.xml" <<'MENU'
<?xml version="1.0" encoding="UTF-8"?>
<openbox_menu xmlns="http://openbox.org/3.4/menu">
  <menu id="root-menu" label="Pob">
    <item label="Terminal">
      <action name="Execute"><execute>xterm</execute></action>
    </item>
    <item label="Text editor">
      <action name="Execute"><execute>mousepad</execute></action>
    </item>
    <item label="Firefox">
      <action name="Execute"><execute>firefox</execute></action>
    </item>
    <separator/>
    <item label="Files — ~/.pob">
      <action name="Execute"><execute>pcmanfm /root/.pob</execute></action>
    </item>
    <separator/>
    <item label="Reconfigure openbox">
      <action name="Reconfigure"/>
    </item>
  </menu>
</openbox_menu>
MENU

log "starting openbox"
openbox &
SERVICE_PIDS+=($!)

# ── the compositor ───────────────────────────────────────────────────────────
# Pob's overlay is a translucent window, and on X11 translucency is the
# compositor's doing. Without one the overlay paints opaque over everything
# under it — so the frame comes back black, the app being driven is invisible
# under it, and the shell says so across the middle of its own window.
log "starting xcompmgr"
xcompmgr &
SERVICE_PIDS+=($!)

# What it paints an empty desktop is a plain grey, and that is worth having as
# it is: a screenshot of nothing looks like a screenshot of something, where a
# black one is exactly what a capture that failed looks like. (Setting a colour
# with xsetroot does nothing here — the compositor paints the root itself, and
# xsetroot leaves no root pixmap for it to paint.)

# ── the way in ───────────────────────────────────────────────────────────────
# There is a password, and it is not there to keep anyone out: the port is
# published to 127.0.0.1 on the host and nowhere else (see launch.sh), so
# whoever can reach it is already at the machine. It is there because macOS's
# Screen Sharing will not open a server that offers no authentication at all —
# it asks for a password that does not exist and the connection ends in that
# dialog. A server offering VNC authentication is one it signs in to.
#
# POB_MSB_VNC_PASSWORD='' takes it back off, for a viewer that would rather
# have no auth (TigerVNC, Remmina, RealVNC all connect either way).
VNC_AUTH=(-nopw)
if [ -n "$VNC_PASSWORD" ]; then
    mkdir -p "$HOME/.vnc"
    if x11vnc -storepasswd "$VNC_PASSWORD" "$HOME/.vnc/passwd" >/dev/null 2>&1; then
        VNC_AUTH=(-rfbauth "$HOME/.vnc/passwd")
    else
        log "could not store the VNC password — serving without one"
    fi
fi

# Its own log file rather than this script's output: x11vnc has a lot to say at
# startup, and `msb logs` is for the few lines that are about starting Pob.
log "serving that screen over VNC on :$VNC_PORT"
x11vnc -display "$SCREEN" -rfbport "$VNC_PORT" -forever -shared \
    "${VNC_AUTH[@]}" -o /var/log/x11vnc.log &
SERVICE_PIDS+=($!)

# A session bus keeps Firefox from starting its own for every window, and is
# what several GTK dialogs expect to find.
if command -v dbus-launch >/dev/null 2>&1; then
    eval "$(dbus-launch --sh-syntax)" 2>/dev/null
fi

# ── Pob ──────────────────────────────────────────────────────────────────────
# The shell starts pob-core beside itself, and core is what serves the control
# API the `pob` command talks to — inside the guest, over `msb exec`.
APP_ARGS=()
[ "$FULLSCREEN" = "1" ] && APP_ARGS+=(--fullscreen)

log "starting Pob${APP_ARGS[0]:+ ${APP_ARGS[*]}}"
"$APP_DIR/pob" "${APP_ARGS[@]}" &
APP_PID=$!

# This wait is the sandbox's lifetime. Pob quitting — `pob kill` inside the
# guest, or a crash — ends the workload, and the VM stops with it rather than
# idling on with a desktop nobody is driving.
wait "$APP_PID"
STATUS=$?
log "Pob exited ($STATUS) — stopping the desktop"
exit "$STATUS"

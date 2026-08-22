#!/bin/bash
# `pob launch --msb`: Pob in a microVM on this machine, with this machine's
# ~/.pob inside it and Firefox installed for a macro to drive.
#
# The pieces, and why each one is where it is:
#
#   Dockerfile   the guest's desktop — Xvfb, openbox, x11vnc, Firefox and the
#                libraries the GTK shell is linked against. Built with Docker
#                (which a macOS host already needs for the Linux app) and
#                handed to microsandbox with `docker save | msb load`.
#   run.sh       the sandbox's workload: brings ~/.pob over, makes the screen,
#                and starts Pob on it.
#   this script  the host's half — the Linux app for the guest's architecture,
#                the image, the ports, and the wait for the app to answer.
#
# The app, this directory and ~/.pob go in as read-only mounts rather than
# image layers, so a rebuilt app (linux-x11/build_docker.sh) or an edited
# run.sh takes effect on the next launch with no image work at all. The
# sandbox itself is replaced at every launch: what survives it is on the host.
#
# It runs from either end of an install: from a checkout, where the Linux app is
# built here, and from beside an installed app — Pob.app/Contents/Resources/vm/msb
# or <install>/vm/msb — where there is nothing to build with and the app is
# fetched from the release this Pob is instead.
#
# Usage: vm/msb/launch.sh            (or: pob launch --msb)
#
# Environment:
#   POB_MSB_NAME        sandbox name                     (default pob-msb)
#   POB_MSB_IMAGE       image tag                        (default pob-msb:latest)
#   POB_MSB_CPUS        vCPUs                            (default 2)
#   POB_MSB_MEMORY      guest memory                     (default 4G)
#   POB_MSB_DISK        writable root disk               (default 12G)
#   POB_MSB_GEOMETRY    the screen Pob comes up on       (default 1440x900x24)
#   POB_MSB_VNC_PORT    host port for the VNC view       (default 5900)
#   POB_MSB_VNC_PASSWORD  what a viewer signs in with    (default pob; empty
#                       for none, which macOS's Screen Sharing will not open)
#   POB_MSB_WEB_PORT    host port for the Pob server     (default: its own)
#   POB_MSB_MCP_PORT    host port for the MCP server     (default: its own)
#   POB_MSB_FULLSCREEN  1 to start Pob over the whole guest screen
#   POB_MSB_START       1 to run the macro once it is up
#   POB_MSB_MACROPSL    macro to run instead of the instance's own
#   POB_MSB_REBUILD     1 to rebuild the guest image even if one is cached
#   POB_MSB_SKIP_BUILD  1 to use linux-x11/dist/Pob as it is, whatever it is
#   POB_MSB_APP         a Linux dist/Pob directory to run in the guest, instead
#                       of building or fetching one
#   POB_MSB_VERSION     the release the guest's app is fetched from; the pob
#                       command sets it to its own version
#   POB_MSB_WAIT        seconds to wait for Pob to answer (default 240)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

NAME="${POB_MSB_NAME:-pob-msb}"
IMAGE="${POB_MSB_IMAGE:-pob-msb:latest}"
CPUS="${POB_MSB_CPUS:-2}"
MEMORY="${POB_MSB_MEMORY:-4G}"
DISK="${POB_MSB_DISK:-12G}"
GEOMETRY="${POB_MSB_GEOMETRY:-1440x900x24}"
FULLSCREEN="${POB_MSB_FULLSCREEN:-0}"
# Unset means the default, and empty means no password — so the - form, not :-.
VNC_PASSWORD="${POB_MSB_VNC_PASSWORD-pob}"
START="${POB_MSB_START:-0}"
MACROPSL="${POB_MSB_MACROPSL:-}"
REBUILD="${POB_MSB_REBUILD:-0}"
SKIP_BUILD="${POB_MSB_SKIP_BUILD:-0}"
WAIT_SECONDS="${POB_MSB_WAIT:-240}"
VERSION="${POB_MSB_VERSION:-}"

# Where the releases come from, the same repository `pob update` installs from.
REPO="lhypds/pob"

POB_HOME="$HOME/.pob"
# State, not build output: the guest's app and the record of what has been
# handed to microsandbox live under ~/.pob, which is writable wherever this
# script is run from — inside a signed app bundle it is not.
STATE_DIR="$POB_HOME/msb"
STAMP_FILE="$STATE_DIR/loaded-image"

die() { echo "❌ $*" >&2; exit 1; }

# ── what this needs of the host ──────────────────────────────────────────────
command -v msb >/dev/null 2>&1 || die "microsandbox is not installed — get it with:
   curl -sSL https://get.microsandbox.dev | sh
   (then \`msb doctor\` should say the host is ready)"
command -v docker >/dev/null 2>&1 || die "Docker is required to build the guest image."
docker info >/dev/null 2>&1 || die "The Docker daemon is not running — start Docker first."
[ -d "$POB_HOME" ] || die "there is no $POB_HOME to copy — run \`pob\` once first."

# A microVM runs the host's architecture and nothing else, so this decides both
# what the Linux app is built for and what the image is built for.
case "$(uname -m)" in
    arm64|aarch64) ARCH=arm64 ;;
    x86_64|amd64)  ARCH=amd64 ;;
    *) die "unsupported architecture $(uname -m) — microsandbox runs arm64 and amd64 guests" ;;
esac

# ── the Linux app, for the guest's architecture ──────────────────────────────
# e_machine, two bytes at offset 18 of the ELF header: 0xB7 is aarch64 and
# 0x3E is x86-64. Asked of the binary itself because the answer is what decides
# whether a dist built earlier can be used at all — an amd64 build of the shell
# in an arm64 guest fails to exec, with nothing on the screen to say why.
elf_arch() {
    local machine
    machine="$(od -An -tx1 -j18 -N1 "$1" 2>/dev/null | tr -d ' \n')"
    case "$machine" in
        b7) echo arm64 ;;
        3e) echo amd64 ;;
        *)  echo unknown ;;
    esac
}

# The app for the guest, from the release this Pob is one of: unzipped into
# ~/.pob/msb once and kept, so the first --msb launch fetches it and no later
# one does. This is the install's answer to the checkout's build — there is no
# compiler, no Linux toolchain and no repository to run one in, and the app in
# the VM should be the app out here anyway.
release_app() {
    local dir="$STATE_DIR/app/$VERSION-$ARCH" archive url
    if [ -x "$dir/Pob/pob" ]; then
        echo "$dir/Pob"
        return
    fi
    [ -n "$VERSION" ] || die "which release to fetch the guest's app from is not known here.
   Run --msb through the pob command, which says, or set POB_MSB_VERSION."
    case "$VERSION" in
        *[!0-9.]*) die "this Pob calls itself \"$VERSION\", which is a build rather than a release,
   so there is no release to fetch the guest's app from. Run --msb from a
   checkout, or point POB_MSB_APP at a linux-x11 dist/Pob you have." ;;
    esac
    command -v curl >/dev/null 2>&1 || die "curl is needed to fetch the guest's app."
    command -v unzip >/dev/null 2>&1 || die "unzip is needed to unpack the guest's app."

    url="https://github.com/$REPO/releases/download/v$VERSION/Pob-$VERSION-linux-$ARCH.zip"
    echo "⬇️  Fetching the app for the guest — Pob $VERSION for linux/${ARCH}…" >&2
    mkdir -p "$dir"
    archive="$dir/.app.zip"
    curl -fsSL "$url" -o "$archive" || die "could not download $url
   Check the network. A Linux dist/Pob of your own works too: POB_MSB_APP=DIR"
    (cd "$dir" && unzip -oq "$archive") || die "could not unpack $archive"
    rm -f "$archive"
    [ -x "$dir/Pob/pob" ] || die "$url did not hold a Pob/pob"
    echo "$dir/Pob"
}

# Three ways to have the app, in the order they answer: named outright, built in
# the checkout this script is part of, or fetched from the release. What tells a
# checkout from an install is the build script — an install ships neither it nor
# anything to run it on.
if [ -n "${POB_MSB_APP:-}" ]; then
    DIST_DIR="${POB_MSB_APP%/}"
    [ -x "$DIST_DIR/pob" ] || die "POB_MSB_APP is $DIST_DIR, and there is no pob in it."
elif [ -x "$ROOT_DIR/linux-x11/build_docker.sh" ]; then
    DIST_DIR="$ROOT_DIR/linux-x11/dist/Pob"
    if [ ! -x "$DIST_DIR/pob" ]; then
        [ "$SKIP_BUILD" = "1" ] && die "no Linux app at $DIST_DIR, and POB_MSB_SKIP_BUILD is set."
        echo "🔨 No Linux app yet — building it for linux/${ARCH}…"
        LINUX_ARCH="$ARCH" "$ROOT_DIR/linux-x11/build_docker.sh"
    elif [ "$(elf_arch "$DIST_DIR/pob")" != "$ARCH" ] && [ "$SKIP_BUILD" != "1" ]; then
        echo "🔨 The Linux app in dist is $(elf_arch "$DIST_DIR/pob") — rebuilding it for linux/${ARCH}…"
        LINUX_ARCH="$ARCH" "$ROOT_DIR/linux-x11/build_docker.sh"
    fi
    [ -x "$DIST_DIR/pob" ] || die "no Linux app at $DIST_DIR after building."
else
    DIST_DIR="$(release_app)"
fi

# Said here rather than found out in the guest: an app of the other architecture
# comes up as a VM that boots, fails to exec anything, and looks like a Pob that
# will not start.
APP_ARCH="$(elf_arch "$DIST_DIR/pob")"
[ "$APP_ARCH" = "$ARCH" ] || die "the Linux app in $DIST_DIR is $APP_ARCH and the guest is ${ARCH} —
   it cannot run there. A checkout rebuilds it (unset POB_MSB_SKIP_BUILD);
   POB_MSB_APP points at another."
echo "📦 App:      $DIST_DIR ($APP_ARCH)"

# ── the guest image ──────────────────────────────────────────────────────────
# Built by Docker, then handed to microsandbox, which keeps its own store of
# images.
#
# The build runs at every launch and is Docker's layer cache doing nothing most
# times: it is the only thing that notices an edited Dockerfile, and an image
# left behind by the Dockerfile before it is a guest missing whatever was added.
# POB_MSB_REBUILD is the cache thrown away — for a rebuild that picks up new
# packages from the same Dockerfile.
echo "🔨 Building the guest image $IMAGE for linux/${ARCH}…"
BUILD_ARGS=(--platform "linux/$ARCH" -t "$IMAGE")
[ "$REBUILD" = "1" ] && BUILD_ARGS+=(--no-cache)
docker build "${BUILD_ARGS[@]}" "$SCRIPT_DIR"

# The stamp is what was last handed over. The load copies a gigabyte or so, so
# doing it again for an image microsandbox already has would be most of a launch
# that had nothing else to do.
#
# The layers, not the image id: `docker build --platform` stamps out a new id at
# every build, cached or not, so an id would say "new image" every time and the
# gigabyte would move every time. The layers are the content, and they are the
# same until the image really is different.
IMAGE_LAYERS="$(docker image inspect -f '{{.RootFS.Layers}}' "$IMAGE")"
if [ "$(cat "$STAMP_FILE" 2>/dev/null || true)" != "$IMAGE_LAYERS" ] ||
   ! msb image list -q 2>/dev/null | grep -qx "$IMAGE"; then
    echo "📥 Handing $IMAGE to microsandbox…"
    docker save "$IMAGE" | msb load -t "$IMAGE" -q
    mkdir -p "$STATE_DIR"
    printf '%s\n' "$IMAGE_LAYERS" > "$STAMP_FILE"
fi
echo "📦 Image:    $IMAGE"

# ── the ports ────────────────────────────────────────────────────────────────
# The guest serves whatever this machine's settings.json says, because that
# file is the one the guest gets a copy of. The host side of each mapping is
# that same number when it is free, since a port people know is worth keeping.
SETTINGS="$POB_HOME/settings.json"

# The one number wanted out of a small hand-written JSON file, with a default
# for "no such key" and for "no such file" alike. Not a JSON parser, and not
# trying to be one: what these two files hold is one flat object each.
json_number() {
    local found
    found="$(sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p" "$1" 2>/dev/null | head -1)"
    echo "${found:-$3}"
}
setting_off() {
    grep -q "\"$1\"[[:space:]]*:[[:space:]]*false" "$SETTINGS" 2>/dev/null
}

WEB_GUEST="$(json_number "$SETTINGS" server_port 8033)"
MCP_GUEST="$(json_number "$SETTINGS" mcp_port 8032)"
VNC_GUEST=5900

# ── the screen the frame has to fit on ───────────────────────────────────────
# Every coordinate in a macro is measured from inside Pob's frame, so the frame
# has to come up the size it was recorded at — and a frame that does not fit on
# the guest's screen is clipped by it: the clicks below the bottom edge land on
# nothing, and the screenshots come back short.
#
# The window the instance was left at is in its instance.json, so the screen is
# made from that: the frame, plus room for the title bar and the shell's own
# toolbar above it, and a margin. Naming a geometry says this differently and
# is taken as said.
if [ -z "${POB_MSB_GEOMETRY:-}" ]; then
    INSTANCE_ID="$(tr -d ' \t\r\n' < "$POB_HOME/INSTANCE" 2>/dev/null || true)"
    INSTANCE_JSON="$POB_HOME/$INSTANCE_ID/instance.json"
    FRAME_W="$(json_number "$INSTANCE_JSON" window_width 0)"
    FRAME_H="$(json_number "$INSTANCE_JSON" window_height 0)"
    SCREEN_W=$((FRAME_W + 120))
    SCREEN_H=$((FRAME_H + 160))
    if [ "$SCREEN_W" -lt 1440 ]; then SCREEN_W=1440; fi
    if [ "$SCREEN_H" -lt 900 ]; then SCREEN_H=900; fi
    GEOMETRY="${SCREEN_W}x${SCREEN_H}x24"
fi

# /dev/tcp rather than lsof or nc: it is bash itself, so it is there on both
# hosts microsandbox runs on. Nothing is listening on the host side of a mapping
# that has not been made yet, so the three are also kept apart by hand — two
# scans that started at different numbers could otherwise agree on one.
CLAIMED_PORTS=()
port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
port_taken() {
    local claimed
    for claimed in "${CLAIMED_PORTS[@]:-}"; do
        [ "$claimed" = "$1" ] && return 0
    done
    port_busy "$1"
}
# Sets PORT, and cannot hand it back any other way: the claim it records has to
# outlive the call, which a $(…) subshell would not.
free_port() {
    local tries=0
    PORT="$1"
    while [ "$tries" -lt 40 ] && port_taken "$PORT"; do
        PORT=$((PORT + 1))
        tries=$((tries + 1))
    done
    CLAIMED_PORTS+=("$PORT")
}

WANT_VNC="${POB_MSB_VNC_PORT:-$VNC_GUEST}"
WANT_WEB="${POB_MSB_WEB_PORT:-$WEB_GUEST}"
WANT_MCP="${POB_MSB_MCP_PORT:-$MCP_GUEST}"

# The sandbox from an earlier launch is stopped here rather than replaced below,
# and the difference is the ports: a running VM holds the ones it published, and
# a launch that stepped around its own last one would move every address it
# prints one number along, at every launch, forever.
if msb list --running -q 2>/dev/null | grep -qx "$NAME"; then
    echo "🧹 Stopping the $NAME left from an earlier launch…"
    msb stop "$NAME" >/dev/null 2>&1 || true
    # Letting go of them takes a moment longer than stopping, and a scan run
    # inside that moment steps around ports that are already on their way back.
    for _ in $(seq 20); do
        if ! port_busy "$WANT_VNC" && ! port_busy "$WANT_WEB" && ! port_busy "$WANT_MCP"; then
            break
        fi
        sleep 0.5
    done
fi

free_port "$WANT_VNC"; VNC_HOST="$PORT"
free_port "$WANT_WEB"; WEB_HOST="$PORT"
free_port "$WANT_MCP"; MCP_HOST="$PORT"

# ── the sandbox ──────────────────────────────────────────────────────────────
# --replace, so a launch is a launch: the guest's own state is a copy of this
# machine's and is made again from it in a few seconds, and a VM left over from
# an earlier run is the one thing that could answer in its place.
#
# Published on 127.0.0.1 only. The guest's Pob server has no authentication and
# its VNC password is printed a few lines below — both are as open as the
# machine they are reachable from, and that machine is this one.
echo "🚀 Starting the sandbox $NAME (${CPUS} vCPU, $MEMORY, $DISK, $GEOMETRY)…"
msb run -d --replace --name "$NAME" "$IMAGE" --pull never \
    --cpus "$CPUS" --memory "$MEMORY" --root-disk "$DISK" \
    --port "127.0.0.1:$VNC_HOST:$VNC_GUEST" \
    --port "127.0.0.1:$WEB_HOST:$WEB_GUEST" \
    --port "127.0.0.1:$MCP_HOST:$MCP_GUEST" \
    --mount-dir "$POB_HOME:/mnt/pob-home:ro" \
    --mount-dir "$DIST_DIR:/mnt/pob-app:ro" \
    --mount-dir "$SCRIPT_DIR:/mnt/pob-vm:ro" \
    --env "POB_MSB_GEOMETRY=$GEOMETRY" \
    --env "POB_MSB_FULLSCREEN=$FULLSCREEN" \
    --env "POB_MSB_VNC_PORT=$VNC_GUEST" \
    --env "POB_MSB_VNC_PASSWORD=$VNC_PASSWORD" \
    --quiet >/dev/null ||
    die "microsandbox could not start the VM — \`msb doctor\` says whether this host can."

# ── waiting for Pob ──────────────────────────────────────────────────────────
# `pob status` inside the guest answers only once the app is up and pob-core is
# serving its control API, which is the same thing `pob launch` waits for on a
# desktop — so this is the launch finishing rather than the VM booting.
#
# Counted in seconds rather than in attempts: an attempt on a machine that is
# still booting takes a moment of its own, so a loop of a hundred tries is a
# wait of anything at all.
echo "⏳ Waiting for Pob to answer inside the VM…"
STATUS_OUT=""
DEADLINE=$((SECONDS + WAIT_SECONDS))
while [ "$SECONDS" -lt "$DEADLINE" ]; do
    if STATUS_OUT="$(msb exec "$NAME" -- pob status 2>/dev/null)"; then
        break
    fi
    STATUS_OUT=""
    sleep 1
done
if [ -z "$STATUS_OUT" ]; then
    echo "❌ Pob did not answer within ${WAIT_SECONDS}s. What the VM printed:" >&2
    msb logs "$NAME" --tail 40 >&2 || true
    echo "" >&2
    die "the sandbox is still up — look around with: msb exec -t $NAME -- bash"
fi

echo ""
echo "$STATUS_OUT"
echo ""
if [ -n "$VNC_PASSWORD" ]; then
    echo "🖥  Screen:   vnc://127.0.0.1:$VNC_HOST   (password: $VNC_PASSWORD)"
else
    echo "🖥  Screen:   vnc://127.0.0.1:$VNC_HOST   (no password)"
fi
setting_off server || echo "🌐 Web UI:   http://127.0.0.1:$WEB_HOST/"
setting_off mcp    || echo "🔌 MCP:      http://127.0.0.1:$MCP_HOST/mcp"
echo ""
echo "Drive it with the CLI inside the guest, or stop the whole machine:"
echo "  msb exec $NAME -- pob start        # replay the macro"
echo "  msb exec $NAME -- pob screenshot   # capture the guest's screen"
echo "  msb exec -t $NAME -- bash          # a shell in the VM"
echo "  msb logs $NAME                     # what the desktop printed"
echo "  msb stop $NAME                     # shut the VM down"

# ── and, if it was asked for, the run ────────────────────────────────────────
if [ "$START" = "1" ]; then
    echo ""
    echo "▶️  Starting the macro…"
    if [ -n "$MACROPSL" ]; then
        msb exec "$NAME" -- pob start --macropsl "$MACROPSL"
    else
        msb exec "$NAME" -- pob start
    fi
fi

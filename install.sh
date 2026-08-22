#!/bin/bash
# Installs what *this* folder builds onto this machine: the app goes somewhere
# it can stay and the `pob` command lands on the PATH. The local counterpart of
# get.sh — the same destinations, but from this working tree rather than from a
# downloaded release, so a build you just made is the one you run.
#
# Like the other root scripts it dispatches on the SYSTEM file (see ./setup.sh);
# --os installs a specific shell without changing SYSTEM.
#
# Usage:
#   ./install.sh                 # install what SYSTEM says (building if needed)
#   sudo ./install.sh            # everyone (Linux: /opt/pob + /usr/local/bin)
#   ./install.sh --build         # rebuild first, then install
#   ./install.sh --uninstall     # take it back off again
#
# Options:
#   --os OS        macos | linux-x11    (default: the SYSTEM file)
#   --prefix DIR   where the app goes   (macOS: the folder Pob.app goes in)
#   --bin DIR      where the `pob` symlink goes (must be on the PATH)
#   --build        rebuild before installing, even if a build is already there
#   --uninstall    remove an install this script made

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SYSTEM_FILE="$SCRIPT_DIR/SYSTEM"
BUNDLE_ID="com.gcc3.pob"

normalize() {
    case "$1" in
        macos|macOS|mac) echo "macos" ;;
        linux|linux-x11|x11) echo "linux-x11" ;;
        win|windows) echo "win" ;;
        *) echo "" ;;
    esac
}

usage() {
    cat <<EOF
Installs what this folder builds and puts the \`pob\` command on the PATH.

Usage:
  ./install.sh                 # install what SYSTEM says (building if needed)
  sudo ./install.sh            # everyone (Linux: /opt/pob + /usr/local/bin)
  ./install.sh --build         # rebuild first, then install
  ./install.sh --uninstall     # take it back off again

Options:
  --os OS        macos | linux-x11    (default: the SYSTEM file)
  --prefix DIR   where the app goes   (macOS: the folder Pob.app goes in)
  --bin DIR      where the pob symlink goes (must be on the PATH)
  --build        rebuild before installing, even if a build is already there
  --uninstall    remove an install this script made
EOF
}

# ── options ──────────────────────────────────────────────────────────────────

SYSTEM=""
PREFIX=""
BIN_DIR=""
BUILD=0
UNINSTALL=0

while [ $# -gt 0 ]; do
    case "$1" in
        --os)
            [ $# -ge 2 ] || { echo "❌ --os requires a value. Use: --os [macos|linux-x11]"; exit 1; }
            SYSTEM="$(normalize "$2")"
            [ -n "$SYSTEM" ] || { echo "❌ Unknown OS '$2'. Use: --os [macos|linux-x11]"; exit 1; }
            shift 2 ;;
        --os=*)
            SYSTEM="$(normalize "${1#--os=}")"
            [ -n "$SYSTEM" ] || { echo "❌ Unknown OS '${1#--os=}'. Use: --os [macos|linux-x11]"; exit 1; }
            shift ;;
        --prefix)
            [ $# -ge 2 ] || { echo "❌ --prefix needs a directory"; exit 1; }
            PREFIX="$2"; shift 2 ;;
        --bin)
            [ $# -ge 2 ] || { echo "❌ --bin needs a directory"; exit 1; }
            BIN_DIR="$2"; shift 2 ;;
        --build) BUILD=1; shift ;;
        --uninstall) UNINSTALL=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "❌ Unknown option: $1 (try --help)"; exit 1 ;;
    esac
done

if [ -z "$SYSTEM" ]; then
    if [ ! -f "$SYSTEM_FILE" ]; then
        echo "❌ No SYSTEM file found — run ./setup.sh first, or pass --os [macos|linux-x11]."
        exit 1
    fi
    SYSTEM="$(tr -d '[:space:]' < "$SYSTEM_FILE")"
    SYSTEM="$(normalize "$SYSTEM")"
    if [ -z "$SYSTEM" ]; then
        echo "❌ Unknown SYSTEM in $SYSTEM_FILE — run ./setup.sh again."
        exit 1
    fi
fi

if [ "$SYSTEM" = "win" ]; then
    echo "❌ Windows has no bash to dispatch from — run its installer directly:"
    echo "   powershell -ExecutionPolicy Bypass -File win\\install.ps1"
    exit 1
fi

# ── Linux: hand it to the shell's own installer ───────────────────────────────

# linux-x11/install.sh is the installer that ships inside the release zip, so a
# repo install and a downloaded one are the same install — it already builds
# dist/Pob when it is missing.
if [ "$SYSTEM" = "linux-x11" ]; then
    if [ "$(uname -s)" != "Linux" ] && [ "$UNINSTALL" -eq 0 ]; then
        echo "❌ SYSTEM is linux-x11 but this host is $(uname -s) — nothing to install here."
        echo "   Build the Linux shell with ./build.sh --os linux-x11 and install it there."
        exit 1
    fi

    ARGS=()
    [ -n "$PREFIX" ] && ARGS+=(--prefix "$PREFIX")
    [ -n "$BIN_DIR" ] && ARGS+=(--bin "$BIN_DIR")
    [ "$UNINSTALL" -eq 1 ] && ARGS+=(--uninstall)

    if [ "$BUILD" -eq 1 ] && [ "$UNINSTALL" -eq 0 ]; then
        echo "🔨 Rebuilding the Linux/X11 shell…"
        "$SCRIPT_DIR/linux-x11/build.sh"
    fi

    exec "$SCRIPT_DIR/linux-x11/install.sh" ${ARGS[@]+"${ARGS[@]}"}
fi

# ── macOS ────────────────────────────────────────────────────────────────────

if [ "$(uname -s)" != "Darwin" ]; then
    echo "❌ SYSTEM is macos but this host is $(uname -s)."
    exit 1
fi

# An admin account can write /Applications without a password, so what decides
# the destination is not who you are but what you can write — a plain
# ./install.sh still lands the app where a drag would have put it.
if [ -z "$PREFIX" ]; then
    if [ "$(id -u)" -eq 0 ] || [ -w "/Applications" ]; then
        PREFIX="/Applications"
    else
        PREFIX="$HOME/Applications"
    fi
fi

# /usr/local/bin is where the app's own "Install 'pob' Command…" menu item links
# to, so putting the link there keeps that menu telling the truth. It is
# root-owned — and on Apple Silicon often not there at all — so fall back to the
# same ~/.local/bin the Linux install uses.
if [ -z "$BIN_DIR" ]; then
    if [ "$(id -u)" -eq 0 ] || [ -w "/usr/local/bin" ]; then
        BIN_DIR="/usr/local/bin"
    else
        BIN_DIR="$HOME/.local/bin"
    fi
fi

APP="$PREFIX/Pob.app"
CLI_IN_APP="$APP/Contents/Helpers/pob"

# A running Pob holds the app it was launched from; replacing that underneath it
# leaves a half-live process and an install nobody can trust.
if pgrep -x Pob >/dev/null 2>&1; then
    echo "⚠️  Pob is running — quit it first (or \`pob kill\`) so the app can be replaced."
    exit 1
fi

# ── uninstall ────────────────────────────────────────────────────────────────

if [ "$UNINSTALL" -eq 1 ]; then
    REMOVED=0
    # Only ever remove a link that points into the app being removed — someone
    # else's pob on the PATH is not ours to delete. /usr/local/bin is checked
    # even when the link went elsewhere, because the app's own menu item makes
    # one there and it would be left dangling.
    for DIR in "$BIN_DIR" "/usr/local/bin" "$HOME/.local/bin"; do
        LINK="$DIR/pob"
        if [ -L "$LINK" ] && [ "$(readlink "$LINK")" = "$CLI_IN_APP" ]; then
            rm -f "$LINK"
            echo "✅ Removed $LINK"
            REMOVED=1
        fi
    done

    if [ -d "$APP" ]; then
        rm -rf "$APP"
        echo "✅ Removed $APP"
        REMOVED=1
    fi

    if [ "$REMOVED" -eq 0 ]; then
        echo "❌ No install found at $APP."
        echo "   If Pob is somewhere else, pass --prefix DIR (and --bin DIR)."
        exit 1
    fi

    echo ""
    echo "Done. ~/.pob is untouched — settings, instances and logs are still there."
    echo ""
    echo "macOS keeps the Accessibility and Screen Recording grants after the app"
    echo "is gone. To clear them too:"
    echo ""
    echo "   tccutil reset All $BUNDLE_ID"
    exit 0
fi

# ── what to install from ─────────────────────────────────────────────────────

# macos/build.sh writes the bundle here and dittos a copy into dist/Pob for the
# release zip; either is the same app, so take whichever is present.
SRC_APP=""
for CANDIDATE in \
    "$SCRIPT_DIR/macos/macos_app/Pob.app" \
    "$SCRIPT_DIR/macos/macos_app/dist/Pob/Pob.app"
do
    if [ -x "$CANDIDATE/Contents/Helpers/pob" ]; then
        SRC_APP="$CANDIDATE"
        break
    fi
done

if [ "$BUILD" -eq 1 ] || [ -z "$SRC_APP" ]; then
    # Building as root would leave root-owned objects all over the worktree —
    # and the sudo is for writing /Applications, not for the compiler.
    if [ "$(id -u)" -eq 0 ]; then
        echo "❌ Nothing built yet, and building as root would leave root-owned files in the repo."
        echo "   Build it as yourself first, then install:"
        echo ""
        echo "   ./build.sh && sudo ./install.sh"
        exit 1
    fi
    if [ -z "$SRC_APP" ]; then
        echo "🔨 No Pob.app built yet — building it…"
    else
        echo "🔨 Rebuilding Pob.app…"
    fi
    "$SCRIPT_DIR/macos/build.sh"
    SRC_APP="$SCRIPT_DIR/macos/macos_app/Pob.app"
fi

if [ ! -x "$SRC_APP/Contents/Helpers/pob" ]; then
    echo "❌ $SRC_APP is incomplete — the build did not finish."
    exit 1
fi

# Installing on top of the source would delete it in the swap below.
if [ "$SRC_APP" = "$APP" ]; then
    echo "❌ $SRC_APP is already the install target — nothing to do."
    exit 1
fi

# ── install ──────────────────────────────────────────────────────────────────

mkdir -p "$PREFIX" "$BIN_DIR"

VERSION="$(tr -d '[:space:]' < "$SCRIPT_DIR/VERSION" 2>/dev/null || true)"
echo "📦 Installing${VERSION:+ Pob $VERSION} to ${PREFIX}…"

# Copy in under a dotted name first: Launchpad and Spotlight skip it, so a copy
# that fails halfway is never a second Pob in the launcher, and the app already
# installed stays usable until the rename swaps it out in one step.
STAGE="$PREFIX/.Pob.app.incoming"
cleanup() {
    [ -n "${STAGE:-}" ] && rm -rf "$STAGE"
    return 0
}
trap cleanup EXIT INT TERM HUP

rm -rf "$STAGE"
# ditto, not cp: it puts an app bundle down the way it went in — modes, symlinks
# and the extended attributes a signed bundle travels with.
ditto "$SRC_APP" "$STAGE"

rm -rf "$APP"
mv "$STAGE" "$APP"
STAGE=""

# The command on the PATH is the CLI, not the app: `pob` typed in a terminal
# inspects and drives the instance, and `pob launch` is what starts the window.
# It stays a symlink — the same one the app's menu item makes — so the next
# install in place is picked up without relinking.
ln -sfn "$CLI_IN_APP" "$BIN_DIR/pob"

echo "✅ Installed:"
echo "   app  $APP"
echo "   cli  $BIN_DIR/pob → $CLI_IN_APP"

# ── is it actually reachable? ────────────────────────────────────────────────

case ":$PATH:" in
    *":$BIN_DIR:"*)
        echo ""
        echo "Done — try \`pob\` (or \`pob launch\` to start the app)."
        ;;
    *)
        echo ""
        echo "⚠️  $BIN_DIR is not on your PATH. Add this to ~/.zshrc (or ~/.bash_profile):"
        echo ""
        echo "   export PATH=\"$BIN_DIR:\$PATH\""
        echo ""
        echo "then open a new terminal and try \`pob\`."
        ;;
esac

# macOS pins an ad-hoc grant to the exact binary it was given to, so a build
# from this folder replacing an installed copy comes with the toggles still on
# and the clicks silently dropped. See docs/Pob/14_Development.md.
echo ""
echo "This is a local build: macOS ties Accessibility and Screen Recording to the"
echo "exact binary, so replacing an installed copy invalidates the old grants."
echo "With Pob quit, reset them and grant them again:"
echo ""
echo "   tccutil reset All $BUNDLE_ID"

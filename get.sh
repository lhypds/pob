#!/bin/sh
# Installs Pob on Linux or macOS in one command: works out which release fits
# this machine, downloads it, and puts it where it belongs — on Linux by handing
# the unzipped folder to its own install.sh, the same install a user gets by
# downloading the zip and running it by hand; on macOS by putting Pob.app in
# Applications, where dragging it would have put it.
#
# Written for POSIX sh so it can be piped straight into whatever /bin/sh is:
#
#   curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sudo sh
#
# Anything after `-s --` reaches the install:
#
#   curl -fsSL .../get.sh | sh -s -- --prefix /opt/pob --bin /usr/local/bin
#   curl -fsSL .../get.sh | sh -s -- --uninstall
#
# `pob update` fetches this script and runs it the same way, with --version and
# --prefix already filled in for the install it is replacing — so an update is
# this install, and there is one installer rather than two.
#
# Env:
#   POB_VERSION=0.2.3      install this version instead of the latest release

set -eu

REPO="lhypds/pob"
BUNDLE_ID="com.gcc3.pob"
VERSION="${POB_VERSION:-}"
PREFIX=""
BIN_DIR=""
UNINSTALL=0

usage() {
    cat <<EOF
Installs Pob (Linux and macOS) and puts the \`pob\` command on the PATH.

Usage:
  curl -fsSL https://raw.githubusercontent.com/$REPO/master/get.sh | sh
  curl -fsSL https://raw.githubusercontent.com/$REPO/master/get.sh | sudo sh

On Linux, without sudo it installs for this user (~/.local); with sudo, for
everyone (/opt/pob + /usr/local/bin).

On macOS, Pob.app goes to /Applications when that is writable — on an admin
account it is, without a password — and to ~/Applications otherwise.

Options (after \`sh -s --\`):
  --version VER  install a specific version   (default: the latest release)
  --prefix DIR   where the app tree goes; on macOS, the folder Pob.app goes in
  --bin DIR      where the pob symlink goes   (must be on the PATH)
  --uninstall    remove an install this script made
EOF
}

# ── options ──────────────────────────────────────────────────────────────────

while [ $# -gt 0 ]; do
    case "$1" in
        --version) [ $# -ge 2 ] || { echo "❌ --version needs a version"; exit 1; }; VERSION="$2"; shift 2 ;;
        --prefix)  [ $# -ge 2 ] || { echo "❌ --prefix needs a directory"; exit 1; }; PREFIX="$2"; shift 2 ;;
        --bin)     [ $# -ge 2 ] || { echo "❌ --bin needs a directory"; exit 1; }; BIN_DIR="$2"; shift 2 ;;
        --uninstall) UNINSTALL=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "❌ Unknown option: $1 (try --help)"; exit 1 ;;
    esac
done

# A tag typed as v0.2.3 and a version typed as 0.2.3 mean the same release.
VERSION="${VERSION#v}"

# ── is this machine one of ours? ─────────────────────────────────────────────

OS="$(uname -s)"
case "$OS" in
    Linux)  PLATFORM="linux" ;;
    Darwin) PLATFORM="macos" ;;
    *)
        echo "❌ This installer is for Linux and macOS — this is $OS."
        echo "   For Windows download Pob-<version>-windows-<arch>.zip from"
        echo "   https://github.com/$REPO/releases and follow the README."
        exit 1
        ;;
esac

if [ "$PLATFORM" = "linux" ]; then
    case "$(uname -m)" in
        x86_64|amd64) ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)
            echo "❌ No Pob release for $(uname -m) — only amd64 and arm64 are built."
            echo "   You can build it from source: https://github.com/$REPO"
            exit 1
            ;;
    esac
    PLATFORM_LABEL="linux/$ARCH"
    ZIP_SUFFIX="linux-$ARCH"
else
    # `uname -m` says x86_64 under Rosetta as well, so it cannot tell an Intel
    # Mac from a translated shell on Apple Silicon; this sysctl is about the
    # machine rather than the process, and is absent on Intel.
    if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" != "1" ]; then
        echo "❌ The macOS release is built for Apple Silicon — this Mac is Intel."
        echo "   You can build it from source: https://github.com/$REPO"
        exit 1
    fi
    PLATFORM_LABEL="macOS (Apple Silicon)"
    ZIP_SUFFIX="macos"
fi

# ── where things go ──────────────────────────────────────────────────────────

if [ "$PLATFORM" = "linux" ]; then
    # What install.sh itself defaults to, repeated here only so --uninstall
    # knows where to look and the messages below can name the real directory.
    if [ -z "$PREFIX" ]; then
        if [ "$(id -u)" -eq 0 ]; then
            PREFIX="/opt/pob"
        else
            PREFIX="${XDG_DATA_HOME:-$HOME/.local/share}/pob"
        fi
    fi

    FORWARD_ARGS="--prefix $PREFIX"
    if [ -n "$BIN_DIR" ]; then
        FORWARD_ARGS="$FORWARD_ARGS --bin $BIN_DIR"
    fi
else
    # An admin account can write /Applications on macOS without a password, so
    # what matters is not who you are but what you can write: a plain `| sh`
    # still lands the app where a drag would have put it.
    if [ -z "$PREFIX" ]; then
        if [ "$(id -u)" -eq 0 ] || [ -w "/Applications" ]; then
            PREFIX="/Applications"
        else
            PREFIX="$HOME/Applications"
        fi
    fi
    if [ -z "$BIN_DIR" ]; then
        # /usr/local/bin is where the app's own "Install 'pob' Command…" menu
        # item links to, so putting the link there keeps that menu telling the
        # truth. It is root-owned — and on Apple Silicon often not there at
        # all — so a user install falls back to the same ~/.local/bin Linux uses.
        if [ "$(id -u)" -eq 0 ] || [ -w "/usr/local/bin" ]; then
            BIN_DIR="/usr/local/bin"
        else
            BIN_DIR="$HOME/.local/bin"
        fi
    fi
    APP="$PREFIX/Pob.app"
    CLI_IN_APP="$APP/Contents/Helpers/pob"
fi

# ── uninstall ────────────────────────────────────────────────────────────────

if [ "$UNINSTALL" -eq 1 ] && [ "$PLATFORM" = "linux" ]; then
    # Every install leaves its own installer behind in $PREFIX, so taking Pob
    # back off again never means finding the zip — or this script's release — a
    # second time.
    if [ ! -x "$PREFIX/install.sh" ]; then
        echo "❌ No install found at $PREFIX."
        echo "   If Pob is somewhere else, pass --prefix DIR (and --bin DIR)."
        echo "   A root install lives in /opt/pob — try again with sudo."
        exit 1
    fi
    # shellcheck disable=SC2086
    exec "$PREFIX/install.sh" --uninstall $FORWARD_ARGS
fi

if [ "$UNINSTALL" -eq 1 ]; then
    # Nothing is left behind on macOS to delegate to: an install here is the app
    # bundle and a symlink, and removing both is the whole of it.
    if pgrep -x Pob > /dev/null 2>&1; then
        echo "⚠️  Pob is running — quit it first (or \`pob kill\`)."
        exit 1
    fi

    REMOVED=0
    # Only ever remove a link that points into the app being removed — someone
    # else's pob on the PATH is not ours to delete. /usr/local/bin is checked
    # too even when the link went elsewhere, because the app's own menu item
    # makes one there and it would be left dangling.
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

# ── what this needs to work with ─────────────────────────────────────────────

if command -v curl > /dev/null 2>&1; then
    FETCH="curl"
elif command -v wget > /dev/null 2>&1; then
    FETCH="wget"
else
    echo "❌ Neither curl nor wget is installed — install one first."
    exit 1
fi

# macOS unpacks with ditto, which is always there; Linux needs unzip.
if [ "$PLATFORM" = "linux" ] && ! command -v unzip > /dev/null 2>&1; then
    echo "❌ unzip not found — install it first:"
    echo "   Debian/Ubuntu/Raspberry Pi OS: sudo apt install unzip"
    echo "   Fedora: sudo dnf install unzip"
    exit 1
fi

# ── which version ────────────────────────────────────────────────────────────

# GitHub redirects /releases/latest to the tag page, so where it lands is the
# version — no API call, and nothing to parse out of JSON.
if [ -z "$VERSION" ]; then
    echo "🔎 Looking up the latest release…"
    LATEST_URL=""
    if [ "$FETCH" = "curl" ]; then
        LATEST_URL="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
            "https://github.com/$REPO/releases/latest" 2>/dev/null || true)"
    else
        # wget prints the redirect chain to stderr with -S; the last tag URL in
        # it is the same place curl would have ended up.
        LATEST_URL="$(wget -qS --spider "https://github.com/$REPO/releases/latest" 2>&1 \
            | sed -n 's/^[[:space:]]*Location:[[:space:]]*\([^[:space:]]*\).*/\1/p' \
            | tail -1 || true)"
    fi
    case "$LATEST_URL" in
        */releases/tag/*) VERSION="${LATEST_URL##*/tag/}"; VERSION="${VERSION#v}" ;;
        *) VERSION="" ;;
    esac
    if [ -z "$VERSION" ]; then
        echo "❌ Could not work out the latest version."
        echo "   Check the network, or pick one yourself:"
        echo "   curl -fsSL https://raw.githubusercontent.com/$REPO/master/get.sh | sh -s -- --version 0.2.3"
        exit 1
    fi
fi

ZIP_NAME="Pob-${VERSION}-${ZIP_SUFFIX}.zip"
ZIP_URL="https://github.com/$REPO/releases/download/v${VERSION}/${ZIP_NAME}"

# ── download ─────────────────────────────────────────────────────────────────

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/pob-install.XXXXXX")"
# The macOS install stages the new bundle beside the old one before swapping, so
# a download or a copy that dies halfway leaves the working app where it was
# rather than nothing at all; STAGE names that half-built copy while it exists.
STAGE=""
cleanup() {
    rm -rf "$TMP_DIR"
    [ -n "$STAGE" ] && rm -rf "$STAGE"
    return 0
}
trap cleanup EXIT INT TERM HUP

echo "⬇️  Downloading Pob $VERSION for ${PLATFORM_LABEL}…"
if [ "$FETCH" = "curl" ]; then
    # --fail so a 404 page never lands on disk looking like a zip.
    curl -fL --progress-bar -o "$TMP_DIR/$ZIP_NAME" "$ZIP_URL" || DOWNLOAD_FAILED=1
else
    wget -q --show-progress -O "$TMP_DIR/$ZIP_NAME" "$ZIP_URL" || DOWNLOAD_FAILED=1
fi

if [ "${DOWNLOAD_FAILED:-0}" -eq 1 ] || [ ! -s "$TMP_DIR/$ZIP_NAME" ]; then
    echo "❌ Could not download $ZIP_NAME"
    echo "   $ZIP_URL"
    echo "   Check that version $VERSION exists: https://github.com/$REPO/releases"
    exit 1
fi

echo "📂 Unpacking…"
if [ "$PLATFORM" = "macos" ]; then
    # ditto, not unzip: the zip was written with ditto, and ditto is what puts an
    # app bundle back the way it went in — modes, symlinks and the extended
    # attributes a signed bundle travels with.
    ditto -x -k "$TMP_DIR/$ZIP_NAME" "$TMP_DIR"
else
    unzip -q "$TMP_DIR/$ZIP_NAME" -d "$TMP_DIR"
fi

SRC="$TMP_DIR/Pob"

# ── install ──────────────────────────────────────────────────────────────────

if [ "$PLATFORM" = "linux" ]; then
    if [ ! -x "$SRC/install.sh" ] && [ ! -f "$SRC/install.sh" ]; then
        echo "❌ $ZIP_NAME did not contain what was expected — nothing installed."
        exit 1
    fi

    # unzip honours the modes in the archive, but not every build of it does when
    # the zip travels through a mirror or a proxy that repacks — and install.sh
    # rejects a tree whose binaries are not executable.
    chmod +x "$SRC/install.sh" "$SRC/pob" "$SRC/pob-core" "$SRC/Helpers/pob" 2>/dev/null || true

    # What the app needs at runtime beyond libc. Checked here rather than left for
    # the first launch to discover, because a one-command install is exactly the
    # path where nobody reads the README that lists them.
    MISSING_LIBS=""
    if command -v ldd > /dev/null 2>&1; then
        MISSING_LIBS="$(ldd "$SRC/pob" 2>/dev/null | sed -n 's/^[[:space:]]*\([^[:space:]]*\) => not found.*/\1/p' || true)"
    fi

    # install.sh does the real work: it is the copy from the release being
    # installed, so it always matches what came down the wire.
    # shellcheck disable=SC2086
    "$SRC/install.sh" $FORWARD_ARGS

    if [ -n "$MISSING_LIBS" ]; then
        echo ""
        echo "⚠️  Installed, but these libraries are missing — Pob will not start until they are there:"
        echo "$MISSING_LIBS" | sed 's/^/   /'
        echo ""
        echo "   Debian/Ubuntu/Raspberry Pi OS: sudo apt install libgtk-3-0 libjson-glib-1.0-0 libxtst6"
        echo "   Fedora: sudo dnf install gtk3 json-glib libXtst"
    fi

    echo ""
    echo "Pob $VERSION — the app needs an X11 session (Xorg) and a running compositor."
    echo "Guide: $PREFIX/README.txt"
    exit 0
fi

# The macOS release ships no install.sh — a .app is installed by being put in
# Applications, and that is what happens here.
if [ ! -d "$SRC/Pob.app" ] || [ ! -x "$SRC/Pob.app/Contents/Helpers/pob" ]; then
    echo "❌ $ZIP_NAME did not contain what was expected — nothing installed."
    exit 1
fi

# A running Pob holds the app it was launched from; replacing that underneath it
# leaves a half-live process and an install nobody can trust.
if pgrep -x Pob > /dev/null 2>&1; then
    echo "⚠️  Pob is running — quit it first (or \`pob kill\`) so the app can be replaced."
    exit 1
fi

UPGRADE=0
[ -d "$APP" ] && UPGRADE=1

mkdir -p "$PREFIX" "$BIN_DIR"

echo "📦 Installing to ${PREFIX}…"
# Copy in under a dotted name first: Launchpad and Spotlight skip it, so a copy
# that fails halfway is never a second Pob in the launcher, and the app already
# installed stays usable until the rename swaps it out in one step.
STAGE="$PREFIX/.Pob.app.incoming"
rm -rf "$STAGE"
ditto "$SRC/Pob.app" "$STAGE"

# Nothing curl downloads is quarantined, but a zip that arrived through a
# browser or a proxy can be — and macOS opens a quarantined app that is not
# notarized, as this one is not, by refusing it as "damaged".
xattr -dr com.apple.quarantine "$STAGE" 2>/dev/null || true

rm -rf "$APP"
mv "$STAGE" "$APP"
STAGE=""

# The command on the PATH is the CLI, not the app: `pob` typed in a terminal
# inspects and drives the instance, and `pob launch` is what starts the window.
# It stays a symlink — the same one the app's menu item makes — so an upgrade in
# place is picked up without reinstalling.
ln -sfn "$CLI_IN_APP" "$BIN_DIR/pob"

echo "✅ Installed:"
echo "   app  $APP"
echo "   cli  $BIN_DIR/pob → $CLI_IN_APP"

# ── is it actually reachable? ────────────────────────────────────────────────

case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *)
        echo ""
        echo "⚠️  $BIN_DIR is not on your PATH. Add this to ~/.zshrc (or ~/.bash_profile):"
        echo ""
        echo "   export PATH=\"$BIN_DIR:\$PATH\""
        echo ""
        echo "then open a new terminal and try \`pob\`. Pob ▸ Install 'pob' Command…"
        echo "in the app's own menu puts it in /usr/local/bin instead, for a password."
        ;;
esac

# ── what macOS still wants from you ──────────────────────────────────────────

echo ""
echo "Pob $VERSION needs two permissions, both in System Settings ▸ Privacy & Security:"
echo ""
echo "   Accessibility     to move the mouse and type. Nothing prompts for this"
echo "                     one — open it, press +, and add $APP by hand."
echo "                     Until you do, Pob looks like it is working while every"
echo "                     click it makes is dropped in silence."
echo "   Screen Recording  to see the screen. Prompts for itself, the first time"
echo "                     Pob captures anything."

if [ "$UPGRADE" -eq 1 ]; then
    echo ""
    echo "⚠️  This replaced an app that was already there, and macOS ties both grants to"
    echo "   the exact copy it was shown — Pob is not signed with a Developer ID, so the"
    echo "   switches stay on in the list while clicking does nothing and screenshots"
    echo "   come back empty. Clear them so the new copy can be given them:"
    echo ""
    echo "   tccutil reset All $BUNDLE_ID"
fi

echo ""
echo "Open it with:  open -a Pob   (or \`pob launch\`)"
echo "Guide: https://github.com/$REPO#readme"

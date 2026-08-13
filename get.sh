#!/bin/sh
# Installs Pob on Linux in one command: works out which release fits this
# machine, downloads it, and hands the unzipped folder to its own install.sh —
# the same install a user gets by downloading the zip and running it by hand.
#
# Written for POSIX sh so it can be piped straight into whatever /bin/sh is:
#
#   curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/lhypds/pob/master/get.sh | sudo sh
#
# Anything after `-s --` reaches install.sh:
#
#   curl -fsSL .../get.sh | sh -s -- --prefix /opt/pob --bin /usr/local/bin
#   curl -fsSL .../get.sh | sh -s -- --uninstall
#
# Env:
#   POB_VERSION=0.2.3      install this version instead of the latest release

set -eu

REPO="lhypds/pob"
VERSION="${POB_VERSION:-}"
PREFIX=""
BIN_DIR=""
UNINSTALL=0

usage() {
    cat <<EOF
Installs Pob (Linux) and puts the \`pob\` command on the PATH.

Usage:
  curl -fsSL https://raw.githubusercontent.com/$REPO/master/get.sh | sh
  curl -fsSL https://raw.githubusercontent.com/$REPO/master/get.sh | sudo sh

Without sudo it installs for this user (~/.local); with sudo, for everyone
(/opt/pob + /usr/local/bin).

Options (after \`sh -s --\`):
  --version VER  install a specific version   (default: the latest release)
  --prefix DIR   where the app tree goes
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

# What install.sh itself defaults to, repeated here only so --uninstall knows
# where to look and the messages below can name the real directory.
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

# ── uninstall ────────────────────────────────────────────────────────────────

# Every install leaves its own installer behind in $PREFIX, so taking Pob back
# off again never means finding the zip — or this script's release — a second
# time.
if [ "$UNINSTALL" -eq 1 ]; then
    if [ ! -x "$PREFIX/install.sh" ]; then
        echo "❌ No install found at $PREFIX."
        echo "   If Pob is somewhere else, pass --prefix DIR (and --bin DIR)."
        echo "   A root install lives in /opt/pob — try again with sudo."
        exit 1
    fi
    # shellcheck disable=SC2086
    exec "$PREFIX/install.sh" --uninstall $FORWARD_ARGS
fi

# ── is this machine one of ours? ─────────────────────────────────────────────

OS="$(uname -s)"
if [ "$OS" != "Linux" ]; then
    echo "❌ This installer is for Linux — this is $OS."
    case "$OS" in
        Darwin) echo "   On macOS download Pob-<version>-macos.zip from" ;;
        *)      echo "   For Windows download Pob-<version>-windows-<arch>.zip from" ;;
    esac
    echo "   https://github.com/$REPO/releases and follow the README."
    exit 1
fi

case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "❌ No Pob release for $(uname -m) — only amd64 and arm64 are built."
        echo "   You can build it from source: https://github.com/$REPO"
        exit 1
        ;;
esac

if command -v curl > /dev/null 2>&1; then
    FETCH="curl"
elif command -v wget > /dev/null 2>&1; then
    FETCH="wget"
else
    echo "❌ Neither curl nor wget is installed — install one first."
    exit 1
fi

if ! command -v unzip > /dev/null 2>&1; then
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

ZIP_NAME="Pob-${VERSION}-linux-${ARCH}.zip"
ZIP_URL="https://github.com/$REPO/releases/download/v${VERSION}/${ZIP_NAME}"

# ── download ─────────────────────────────────────────────────────────────────

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/pob-install.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM HUP

echo "⬇️  Downloading Pob $VERSION for linux/$ARCH…"
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
unzip -q "$TMP_DIR/$ZIP_NAME" -d "$TMP_DIR"

SRC="$TMP_DIR/Pob"
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

# ── install ──────────────────────────────────────────────────────────────────

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

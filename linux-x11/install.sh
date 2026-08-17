#!/bin/bash
# Installs Pob on this machine: the app tree goes somewhere it can stay, and
# the `pob` command lands on the PATH — the Linux counterpart of the macOS
# app's "Install 'pob' Command…" menu item.
#
# Runs from either end of the release: inside an unzipped Pob/ folder (what a
# user downloads), or from the repository, where it builds dist/Pob first.
#
# Usage:
#   ./install.sh                 # just this user   (~/.local)
#   sudo ./install.sh            # everyone         (/opt/pob + /usr/local/bin)
#   ./install.sh --uninstall     # take it all back off again
#
# Options:
#   --prefix DIR   where the app tree goes
#   --bin DIR      where the `pob` symlink goes (must be on the PATH)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Root installs go where every user can reach them; a user install stays in
# ~/.local, which needs no password and is where XDG says this belongs.
#
# DATA_DIR is the XDG data root the app menu entry and its icon go under, so
# Pob turns up in the desktop's launcher — the Linux counterpart of the macOS
# app bundle being visible in /Applications.
if [ "$(id -u)" -eq 0 ]; then
    PREFIX="/opt/pob"
    BIN_DIR="/usr/local/bin"
    DATA_DIR="/usr/local/share"
else
    PREFIX="${XDG_DATA_HOME:-$HOME/.local/share}/pob"
    BIN_DIR="$HOME/.local/bin"
    DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}"
fi

usage() {
    cat <<EOF
Installs Pob and puts the \`pob\` command on the PATH.

Usage:
  ./install.sh                 # just this user   (~/.local)
  sudo ./install.sh            # everyone         (/opt/pob + /usr/local/bin)
  ./install.sh --uninstall     # take it all back off again

Options:
  --prefix DIR   where the app tree goes    (default: $PREFIX)
  --bin DIR      where the pob symlink goes (default: $BIN_DIR)
EOF
}

UNINSTALL=0
while [ $# -gt 0 ]; do
    case "$1" in
        --uninstall) UNINSTALL=1; shift ;;
        --prefix) [ $# -ge 2 ] || { echo "❌ --prefix needs a directory"; exit 1; }; PREFIX="$2"; shift 2 ;;
        --bin) [ $# -ge 2 ] || { echo "❌ --bin needs a directory"; exit 1; }; BIN_DIR="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "❌ Unknown option: $1 (try --help)"; exit 1 ;;
    esac
done

LINK="$BIN_DIR/pob"
DESKTOP_FILE="$DATA_DIR/applications/pob.desktop"
ICON_FILE="$DATA_DIR/icons/hicolor/256x256/apps/pob.png"

# ── uninstall ────────────────────────────────────────────────────────────────

if [ "$UNINSTALL" -eq 1 ]; then
    # Same rule as the symlink below: only ever remove a desktop entry that
    # launches *this* install — a system-wide copy is not a user uninstall's
    # to delete, and the icon beside it belongs to the same entry.
    if [ -f "$DESKTOP_FILE" ] && grep -qxF "Exec=$PREFIX/pob" "$DESKTOP_FILE"; then
        rm -f "$DESKTOP_FILE" "$ICON_FILE"
        echo "✅ Removed $DESKTOP_FILE"
    elif [ -e "$DESKTOP_FILE" ]; then
        echo "⏭  Left $DESKTOP_FILE alone — it does not launch $PREFIX/pob"
    fi
    # Only ever remove a link that points into this install — someone else's
    # pob on the PATH is not ours to delete.
    if [ -L "$LINK" ] && [ "$(readlink "$LINK")" = "$PREFIX/Helpers/pob" ]; then
        rm -f "$LINK"
        echo "✅ Removed $LINK"
    elif [ -e "$LINK" ]; then
        echo "⏭  Left $LINK alone — it does not point at $PREFIX"
    fi
    if [ -d "$PREFIX" ]; then
        rm -rf "$PREFIX"
        echo "✅ Removed $PREFIX"
    fi
    echo ""
    echo "Done. ~/.pob is untouched — settings, instances and logs are still there."
    exit 0
fi

# ── what to install from ─────────────────────────────────────────────────────

# Either this script sits inside an unzipped release beside the binaries, or it
# is the copy in the repository and the release has to be built first.
if [ -x "$SCRIPT_DIR/pob" ] && [ -x "$SCRIPT_DIR/pob-core" ]; then
    SRC="$SCRIPT_DIR"
elif [ -d "$SCRIPT_DIR/dist/Pob" ]; then
    SRC="$SCRIPT_DIR/dist/Pob"
elif [ -x "$SCRIPT_DIR/build.sh" ]; then
    echo "🔨 No dist/Pob yet — building it…"
    "$SCRIPT_DIR/build.sh"
    SRC="$SCRIPT_DIR/dist/Pob"
else
    echo "❌ Nothing to install from — run this inside an unzipped Pob folder or the repository."
    exit 1
fi

for f in pob pob-core Helpers/pob; do
    if [ ! -x "$SRC/$f" ]; then
        echo "❌ $SRC/$f is missing — the build is incomplete."
        exit 1
    fi
done

# ── install ──────────────────────────────────────────────────────────────────

# A running Pob holds its own binaries open — the copy below would fail on the
# first one with "Text file busy" and leave the install half done.
if pgrep -x pob >/dev/null 2>&1; then
    echo "⚠️  Pob is running — quit it first (or \`pob kill\`) so the binaries can be replaced."
    exit 1
fi

mkdir -p "$PREFIX/Helpers" "$BIN_DIR"

# Run from inside the install directory itself, there is nothing to copy —
# every install below would be a file onto itself — but the link is still
# worth (re)making.
if [ "$SRC" = "$PREFIX" ]; then
    echo "📦 Already in $PREFIX — linking only."
else
    echo "📦 Installing to ${PREFIX}…"
    install -m 755 "$SRC/pob" "$PREFIX/pob"
    install -m 755 "$SRC/pob-core" "$PREFIX/pob-core"
    install -m 755 "$SRC/Helpers/pob" "$PREFIX/Helpers/pob"
    if [ -f "$SRC/VERSION" ]; then
        install -m 644 "$SRC/VERSION" "$PREFIX/VERSION"
    fi
    # The app looks for its icon beside its own binary, so it keeps that spot
    # here too — the desktop entry below gets its own copy.
    if [ -f "$SRC/pob.png" ]; then
        install -m 644 "$SRC/pob.png" "$PREFIX/pob.png"
    fi
    # The installer, the guide and the license go along too, so uninstalling
    # later — or reading what any of this was, or what it may be used for —
    # does not mean finding the zip again.
    install -m 755 "$0" "$PREFIX/install.sh"
    if [ -f "$SRC/README.txt" ]; then
        install -m 644 "$SRC/README.txt" "$PREFIX/README.txt"
    fi
    if [ -f "$SRC/LICENSE" ]; then
        install -m 644 "$SRC/LICENSE" "$PREFIX/LICENSE"
    fi
fi

# The command on the PATH is the CLI, not the app: `pob` typed in a terminal
# inspects and drives the instance, and `pob launch` is what starts the window.
# It stays a symlink so an upgrade in $PREFIX is picked up without reinstalling.
ln -sfn "$PREFIX/Helpers/pob" "$LINK"

echo "✅ Installed:"
echo "   app  $PREFIX/pob"
echo "   core $PREFIX/pob-core"
echo "   cli  $LINK → $PREFIX/Helpers/pob"

# ── desktop entry ────────────────────────────────────────────────────────────

# Puts Pob in the applications menu with its icon. Nothing here is needed to
# run the app, so a desktop without these directories is not an error.
if [ -f "$PREFIX/pob.png" ]; then
    mkdir -p "$(dirname "$DESKTOP_FILE")" "$(dirname "$ICON_FILE")"
    install -m 644 "$PREFIX/pob.png" "$ICON_FILE"
    cat > "$DESKTOP_FILE" << DESKTOP
[Desktop Entry]
Type=Application
Name=Pob
GenericName=Perception and Operation Bridge
Comment=Translucent overlay that lets an AI see and drive your desktop
Exec=$PREFIX/pob
Icon=pob
Terminal=false
Categories=Utility;Development;
StartupWMClass=Pob
DESKTOP
    chmod 644 "$DESKTOP_FILE"

    # Both caches are advisory — the entry shows up on the next login without
    # them, and neither tool is installed everywhere.
    command -v update-desktop-database >/dev/null 2>&1 &&
        update-desktop-database "$(dirname "$DESKTOP_FILE")" >/dev/null 2>&1 || true
    command -v gtk-update-icon-cache >/dev/null 2>&1 &&
        gtk-update-icon-cache -qtf "$DATA_DIR/icons/hicolor" >/dev/null 2>&1 || true

    echo "   menu $DESKTOP_FILE"
fi

# ── is it actually reachable? ────────────────────────────────────────────────

case ":$PATH:" in
    *":$BIN_DIR:"*)
        echo ""
        echo "Done — try \`pob\` (or \`pob launch\` to start the app)."
        ;;
    *)
        echo ""
        echo "⚠️  $BIN_DIR is not on your PATH. Add this to ~/.profile (or ~/.bashrc, ~/.zshrc):"
        echo ""
        echo "   export PATH=\"$BIN_DIR:\$PATH\""
        echo ""
        echo "then open a new terminal and try \`pob\`."
        ;;
esac

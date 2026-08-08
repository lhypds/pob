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
if [ "$(id -u)" -eq 0 ]; then
    PREFIX="/opt/pob"
    BIN_DIR="/usr/local/bin"
else
    PREFIX="${XDG_DATA_HOME:-$HOME/.local/share}/pob"
    BIN_DIR="$HOME/.local/bin"
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

# ── uninstall ────────────────────────────────────────────────────────────────

if [ "$UNINSTALL" -eq 1 ]; then
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

echo "📦 Installing to ${PREFIX}…"
mkdir -p "$PREFIX/Helpers" "$BIN_DIR"

install -m 755 "$SRC/pob" "$PREFIX/pob"
install -m 755 "$SRC/pob-core" "$PREFIX/pob-core"
install -m 755 "$SRC/Helpers/pob" "$PREFIX/Helpers/pob"
if [ -f "$SRC/VERSION" ]; then
    install -m 644 "$SRC/VERSION" "$PREFIX/VERSION"
fi

# The command on the PATH is the CLI, not the app: `pob` typed in a terminal
# inspects and drives the instance, and `pob launch` is what starts the window.
# It stays a symlink so an upgrade in $PREFIX is picked up without reinstalling.
ln -sfn "$PREFIX/Helpers/pob" "$LINK"

echo "✅ Installed:"
echo "   app  $PREFIX/pob"
echo "   core $PREFIX/pob-core"
echo "   cli  $LINK → $PREFIX/Helpers/pob"

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

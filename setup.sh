#!/bin/bash

# Pob setup dispatcher. Asks which OS shell to use, records the choice in
# the SYSTEM file (macos | linux-x11), then runs that shell's setup.sh.
# The other root scripts (build/start/stop/restart) read SYSTEM and dispatch
# to the same folder.
#
# Usage:
#   ./setup.sh              # interactive selection (default from uname)
#   ./setup.sh macos        # non-interactive
#   ./setup.sh linux-x11

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SYSTEM_FILE="$SCRIPT_DIR/SYSTEM"

# Default suggestion from the running kernel.
case "$(uname -s)" in
    Darwin) DEFAULT="macos" ;;
    Linux)  DEFAULT="linux-x11" ;;
    *)      DEFAULT="" ;;
esac

normalize() {
    case "$1" in
        1|macos|macOS|mac) echo "macos" ;;
        2|linux|linux-x11|x11) echo "linux-x11" ;;
        *) echo "" ;;
    esac
}

SYSTEM="$(normalize "${1:-}")"

if [ -z "$SYSTEM" ]; then
    echo "Select your OS:"
    echo "  1) macOS        (macos)"
    echo "  2) Linux / X11  (linux-x11)"
    if [ -n "$DEFAULT" ]; then
        read -r -p "Choice [default: $DEFAULT]: " ANSWER
    else
        read -r -p "Choice: " ANSWER
    fi
    if [ -z "$ANSWER" ]; then
        SYSTEM="$DEFAULT"
    else
        SYSTEM="$(normalize "$ANSWER")"
    fi
fi

if [ -z "$SYSTEM" ] || [ ! -f "$SCRIPT_DIR/$SYSTEM/setup.sh" ]; then
    echo "❌ Invalid choice. Use: ./setup.sh [macos|linux-x11]"
    exit 1
fi

echo "$SYSTEM" > "$SYSTEM_FILE"
echo "✅ SYSTEM set to: $SYSTEM"
echo ""

exec "$SCRIPT_DIR/$SYSTEM/setup.sh"

#!/bin/bash

# Dispatches to an OS shell's build script. By default the OS comes from the
# SYSTEM file (see ./setup.sh); pass --os to build for a specific OS without
# changing SYSTEM — e.g. ./build.sh --os win (the Windows shell cross-builds
# from macOS/Linux, see win/build.sh).
#
# Usage:
#   ./build.sh                      # build the OS recorded in SYSTEM
#   ./build.sh --os macos
#   ./build.sh --os linux-x11
#   ./build.sh --os win [args…]     # remaining args forwarded to the script

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SYSTEM_FILE="$SCRIPT_DIR/SYSTEM"

normalize() {
    case "$1" in
        macos|macOS|mac) echo "macos" ;;
        linux|linux-x11|x11) echo "linux-x11" ;;
        win|windows) echo "win" ;;
        *) echo "" ;;
    esac
}

SYSTEM=""
ARGS=()
while [ $# -gt 0 ]; do
    case "$1" in
        --os)
            shift
            if [ $# -eq 0 ]; then
                echo "❌ --os requires a value. Use: --os [macos|linux-x11|win]"
                exit 1
            fi
            SYSTEM="$(normalize "$1")"
            if [ -z "$SYSTEM" ]; then
                echo "❌ Unknown OS '$1'. Use: --os [macos|linux-x11|win]"
                exit 1
            fi
            ;;
        --os=*)
            SYSTEM="$(normalize "${1#--os=}")"
            if [ -z "$SYSTEM" ]; then
                echo "❌ Unknown OS '${1#--os=}'. Use: --os [macos|linux-x11|win]"
                exit 1
            fi
            ;;
        *)
            ARGS+=("$1")
            ;;
    esac
    shift
done

if [ -z "$SYSTEM" ]; then
    if [ ! -f "$SYSTEM_FILE" ]; then
        echo "❌ No SYSTEM file found — run ./setup.sh first, or pass --os [macos|linux-x11|win]."
        exit 1
    fi
    SYSTEM="$(tr -d '[:space:]' < "$SYSTEM_FILE")"
fi

if [ ! -f "$SCRIPT_DIR/$SYSTEM/build.sh" ]; then
    echo "❌ Unknown SYSTEM '$SYSTEM' — run ./setup.sh again."
    exit 1
fi

# ${ARGS[@]+…} keeps the empty-array expansion safe under set -u on bash 3.2.
exec "$SCRIPT_DIR/$SYSTEM/build.sh" ${ARGS[@]+"${ARGS[@]}"}

#!/usr/bin/env bash
# Builds and runs Pob Keyboard: a full-size on-screen keyboard and a trackpad
# in their own window, driving a running Pob instance through its web UI.
#
#   ./keyboard.sh
#   ./keyboard.sh -url http://192.168.1.40:8033/pb-3f9a
#
# With no address it opens Settings… so the machine, port and instance id can
# be typed in — or the whole line `pob status` prints pasted into the first
# field, which fills all three.

set -euo pipefail

cd "$(dirname "$0")/keyboard"

# The binary is built rather than `go run`, and named the way the app should
# read: on macOS the menu bar takes the application name from the running
# executable, so this is what appears beside the Settings item. `go run` would
# put its own temporary name up there instead.
BIN="Pob Keyboard"

if ! command -v go >/dev/null 2>&1; then
    echo "Go is not installed (or not on PATH)." >&2
    echo "Install it from https://go.dev/dl/ and re-run this script." >&2
    exit 1
fi

# go list resolves the imports without compiling, so this is a cheap way to ask
# whether anything is still missing — a first run with a cold module cache, or
# a go.mod that predates a new import being added.
if ! go list -deps . >/dev/null 2>&1; then
    echo "Fetching dependencies (first run takes a while)..."
    go mod tidy
fi

echo "Building $BIN..."
if ! go build -o "$BIN" .; then
    echo >&2
    echo "Build failed. The GUI draws through OpenGL, so it needs a C compiler:" >&2
    echo "  macOS  xcode-select --install" >&2
    echo "  Debian/Ubuntu  sudo apt install gcc libgl1-mesa-dev xorg-dev" >&2
    exit 1
fi

# Pass arguments through, so `./keyboard.sh -url http://host:8033/pb-3f9a` works.
exec "./$BIN" "$@"

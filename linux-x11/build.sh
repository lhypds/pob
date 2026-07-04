#!/bin/bash
# Builds a distributable Linux/X11 release natively (run on a Linux machine):
# Go core (pob-core) + GTK shell, assembled side by side (the shell looks for
# pob-core next to its own binary, like the macOS bundle layout).
# Produces: ./linux-x11/dist/Pob/  and  Pob-<version>-linux-<arch>.zip
#
# When SYSTEM is macos (or the host is Darwin) there is no native GTK/X11
# toolchain, so this delegates to ./build_docker.sh automatically.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SYSTEM="$( { tr -d '[:space:]' < "$ROOT_DIR/SYSTEM"; } 2>/dev/null || true)"
if [[ "$SYSTEM" == "macos" || ( -z "$SYSTEM" && "$(uname -s)" == "Darwin" ) ]]; then
    exec "$SCRIPT_DIR/build_docker.sh" "$@"
fi

VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) ARCH="$(uname -m)" ;;
esac
DIST_DIR="$SCRIPT_DIR/dist/Pob"
ZIP_PATH="$ROOT_DIR/Pob-${VERSION}-linux-${ARCH}.zip"

if ! command -v zip &> /dev/null; then
    echo "❌ zip not found — install it first (e.g. sudo apt install zip)."
    exit 1
fi

# ── build core (Go) ──────────────────────────────────────────────────────────
echo "Building pob-core (Go)…"
(cd "$ROOT_DIR/core" && go build -trimpath -ldflags="-s -w" -o bin/pob-core ./cmd/pob-core)

# ── build shell (C/GTK) ──────────────────────────────────────────────────────
echo "Building Linux shell (release)…"
(cd "$SCRIPT_DIR" && make clean >/dev/null && make)

# ── assemble ─────────────────────────────────────────────────────────────────
echo "Assembling dist/Pob…"
rm -rf "$SCRIPT_DIR/dist"
mkdir -p "$DIST_DIR"
cp "$SCRIPT_DIR/bin/pob" "$DIST_DIR/pob"
cp "$ROOT_DIR/core/bin/pob-core" "$DIST_DIR/pob-core"
cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

echo "Creating ${ZIP_PATH}…"
rm -f "$ZIP_PATH"
(cd "$SCRIPT_DIR/dist" && zip -qr "$ZIP_PATH" Pob)

echo ""
echo "Done: $DIST_DIR"
echo "  Version : $VERSION"
echo "  Zip     : $ZIP_PATH"
echo ""
echo "Run with:  $DIST_DIR/pob"

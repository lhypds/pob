#!/bin/bash
# Cross-builds the Linux/X11 release from any Docker-capable host (macOS
# included — release.sh uses this). The Go core is cross-compiled on the
# host; the GTK shell is compiled inside a Debian container for the target
# architecture.
# Produces: ./linux-x11/dist/Pob/  and  Pob-<version>-linux-<arch>.zip
#
# Env:
#   LINUX_ARCH=amd64|arm64   target architecture (default: amd64)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

ARCH="${LINUX_ARCH:-amd64}"
VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
DIST_DIR="$SCRIPT_DIR/dist/Pob"
ZIP_PATH="$ROOT_DIR/Pob-${VERSION}-linux-${ARCH}.zip"

case "$ARCH" in
    amd64|arm64) ;;
    *) echo "❌ Unsupported LINUX_ARCH '$ARCH' (use amd64 or arm64)"; exit 1 ;;
esac

if ! command -v docker &> /dev/null; then
    echo "❌ Docker is required to cross-build the Linux shell."
    exit 1
fi
if ! docker info &> /dev/null; then
    echo "❌ Docker daemon is not running — start Docker first."
    exit 1
fi
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed."
    exit 1
fi
if ! command -v zip &> /dev/null; then
    echo "❌ zip not found."
    exit 1
fi

# ── build core (Go, cross-compiled on the host) ─────────────────────────────
echo "Building pob-core (Go, linux/$ARCH)…"
(cd "$ROOT_DIR/core" && GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "bin/pob-core-linux-$ARCH" ./cmd/pob-core)

# ── build shell (C/GTK, inside a Debian container) ──────────────────────────
echo "Building Linux shell in Docker (linux/$ARCH)…"
docker run --rm --platform "linux/$ARCH" \
    -v "$SCRIPT_DIR":/src -w /src debian:stable sh -c '
        set -e
        apt-get update -qq >/dev/null 2>&1
        apt-get install -y -qq gcc make pkg-config \
            libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev >/dev/null 2>&1
        make clean >/dev/null
        make
    '

# ── assemble ─────────────────────────────────────────────────────────────────
echo "Assembling dist/Pob…"
rm -rf "$SCRIPT_DIR/dist"
mkdir -p "$DIST_DIR"
cp "$SCRIPT_DIR/bin/pob" "$DIST_DIR/pob"
cp "$ROOT_DIR/core/bin/pob-core-linux-$ARCH" "$DIST_DIR/pob-core"
cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

echo "Creating ${ZIP_PATH}…"
rm -f "$ZIP_PATH"
(cd "$SCRIPT_DIR/dist" && zip -qr "$ZIP_PATH" Pob)

echo ""
echo "Done: $ZIP_PATH (linux/$ARCH)"

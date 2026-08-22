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

MISSING=""
for pkg in gtk+-3.0 json-glib-1.0 x11 xtst; do
    if ! pkg-config --exists "$pkg"; then
        MISSING="$MISSING $pkg"
    fi
done
if [ -n "$MISSING" ]; then
    echo "❌ Missing development libraries:$MISSING"
    echo "   Debian/Ubuntu/Raspberry Pi OS: sudo apt install libgtk-3-dev libjson-glib-dev libx11-dev libxtst-dev"
    echo "   Fedora: sudo dnf install gtk3-devel json-glib-devel libX11-devel libXtst-devel"
    exit 1
fi

# ── clean old zips ───────────────────────────────────────────────────────────
# A zip of another version is stale the moment this build starts, and release.sh
# globs Pob-*.zip — one left behind looks like an asset of this release.
for OLD in "$ROOT_DIR"/Pob-*.zip; do
    [[ -e "$OLD" ]] || continue
    OLD_VERSION="${OLD##*/}"; OLD_VERSION="${OLD_VERSION%.zip}"; OLD_VERSION="${OLD_VERSION#Pob-}"
    if [[ "${OLD_VERSION%%-*}" != "$VERSION" ]]; then
        echo "Removing old zip: ${OLD##*/}"
        rm -f "$OLD"
    fi
done

# ── build core (Go) ──────────────────────────────────────────────────────────
echo "Building pob-core and pob CLI (Go)…"
(cd "$ROOT_DIR/core" \
  && go build -trimpath -ldflags="-s -w" -o bin/pob-core ./cmd/pob-core \
  && go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o bin/pob ./cmd/pob)

# ── build shell (C/GTK) ──────────────────────────────────────────────────────
echo "Building Linux shell (release)…"
(cd "$SCRIPT_DIR" && make clean >/dev/null && make POB_VERSION="$VERSION")

# ── assemble ─────────────────────────────────────────────────────────────────
echo "Assembling dist/Pob…"
rm -rf "$SCRIPT_DIR/dist"
mkdir -p "$DIST_DIR"
cp "$SCRIPT_DIR/bin/pob" "$DIST_DIR/pob"
cp "$ROOT_DIR/core/bin/pob-core" "$DIST_DIR/pob-core"
# The CLI goes to Helpers/, the same place the macOS bundle keeps it — and the
# one directory the installer puts on the PATH, so the app's own `pob` never
# shadows it.
mkdir -p "$DIST_DIR/Helpers"
cp "$ROOT_DIR/core/bin/pob" "$DIST_DIR/Helpers/pob"
cp "$SCRIPT_DIR/install.sh" "$DIST_DIR/install.sh"
# What `pob launch --msb` runs: the microVM's image, its workload, and the
# script that starts it. Named one by one rather than copied as a directory, so
# a local .build/ stamp never travels into a release.
mkdir -p "$DIST_DIR/vm/msb"
for MSB_FILE in launch.sh run.sh Dockerfile README.md; do
    cp "$ROOT_DIR/vm/msb/$MSB_FILE" "$DIST_DIR/vm/msb/$MSB_FILE"
done
chmod 755 "$DIST_DIR/vm/msb/launch.sh" "$DIST_DIR/vm/msb/run.sh"
# The app icon, shared with macOS and Windows. The shell loads it from beside
# its own binary, and install.sh registers it with the desktop environment.
cp "$ROOT_DIR/assets/icon/pob.png" "$DIST_DIR/pob.png"
# What the person who unzips this reads first: how to install it, and what the
# pob command is for.
cp "$SCRIPT_DIR/README.txt" "$DIST_DIR/README.txt"
# The license travels with the binary: it permits redistribution only when the
# terms come along with it.
cp "$ROOT_DIR/LICENSE" "$DIST_DIR/LICENSE"
cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

echo "Creating ${ZIP_PATH}…"
rm -f "$ZIP_PATH"
(cd "$SCRIPT_DIR/dist" && zip -qr "$ZIP_PATH" Pob)

echo ""
echo "Done: $DIST_DIR"
echo "  Version : $VERSION"
echo "  Zip     : $ZIP_PATH"
echo ""
echo "Run with:      $DIST_DIR/pob"
echo "Or install it: $DIST_DIR/install.sh   (puts \`pob\` on the PATH)"

#!/bin/bash
# Builds a distributable Windows release from macOS or Linux using Docker
# (no local .NET SDK needed — mirrors linux-x11/build_docker.sh):
# the Go core is cross-compiled on the host (GOOS=windows), and the WPF
# shell is cross-compiled inside the .NET 8 SDK container
# (EnableWindowsTargeting — .NET can compile, but not run, Windows desktop
# apps on Linux).
# Produces: ./win/dist/Pob/  and  Pob-<version>-windows-<arch>.zip
#
# Override the target list with:  WIN_ARCHS="amd64 arm64" ./win/build_docker.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
WIN_ARCHS="${WIN_ARCHS:-amd64}"
SDK_IMAGE="mcr.microsoft.com/dotnet/sdk:8.0"

if ! command -v go &> /dev/null; then
    echo "❌ Go not found — install it first (the core is cross-compiled on the host)."
    exit 1
fi
if ! command -v docker &> /dev/null; then
    echo "❌ Docker not found — install Docker Desktop first."
    exit 1
fi
if ! docker info &> /dev/null; then
    echo "❌ Docker is not running — start Docker Desktop first."
    exit 1
fi
if ! command -v zip &> /dev/null; then
    echo "❌ zip not found — install it first."
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

for ARCH in $WIN_ARCHS; do
    case "$ARCH" in
        amd64) RID="win-x64" ;;
        arm64) RID="win-arm64" ;;
        *) echo "❌ Unknown arch '$ARCH' (use amd64 or arm64)"; exit 1 ;;
    esac

    # ── build core (Go, cross-compiled on the host) ──────────────────────────
    echo "Building pob-core and pob CLI (Go, windows/$ARCH)…"
    (cd "$ROOT_DIR/core" && \
        GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 \
        go build -trimpath -ldflags="-s -w" -o bin/pob-core.exe ./cmd/pob-core)
    (cd "$ROOT_DIR/core" && \
        GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 \
        go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o bin/pob.exe ./cmd/pob)

    # ── build shell (C#/WPF in the .NET SDK container) ───────────────────────
    echo "Building Windows shell in Docker (release, $RID)…"
    docker run --rm \
        -v "$ROOT_DIR":/src \
        -w /src/win \
        -e DOTNET_CLI_TELEMETRY_OPTOUT=1 \
        "$SDK_IMAGE" \
        dotnet publish Pob.csproj -c Release -r "$RID" \
            --self-contained true -p:PublishSingleFile=true \
            -p:IncludeNativeLibrariesForSelfExtract=true \
            -o "publish-$ARCH"

    # ── assemble ─────────────────────────────────────────────────────────────
    DIST_DIR="$SCRIPT_DIR/dist/Pob"
    ZIP_PATH="$ROOT_DIR/Pob-${VERSION}-windows-${ARCH}.zip"
    echo "Assembling dist/Pob ($ARCH)…"
    rm -rf "$SCRIPT_DIR/dist"
    mkdir -p "$DIST_DIR/Helpers"
    cp "$SCRIPT_DIR/publish-$ARCH/Pob.exe" "$DIST_DIR/Pob.exe"
    cp "$ROOT_DIR/core/bin/pob-core.exe" "$DIST_DIR/pob-core.exe"
    # The CLI goes to Helpers\, the same place the macOS bundle keeps it: on a
    # case-insensitive filesystem its pob.exe and the app's Pob.exe are the
    # same filename, so they cannot share a directory. Helpers\ is what the
    # installer puts on the PATH.
    cp "$ROOT_DIR/core/bin/pob.exe" "$DIST_DIR/Helpers/pob.exe"
    cp "$SCRIPT_DIR/install.ps1" "$DIST_DIR/install.ps1"
    # What the person who unzips this reads first: how to install it, and what
    # the pob command is for.
    cp "$SCRIPT_DIR/README.txt" "$DIST_DIR/README.txt"
    cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

    echo "Creating ${ZIP_PATH}…"
    rm -f "$ZIP_PATH"
    (cd "$SCRIPT_DIR/dist" && zip -qr "$ZIP_PATH" Pob)

    echo ""
    echo "Done: Pob-${VERSION}-windows-${ARCH}.zip"
done

echo ""
echo "Version: $VERSION"
echo "Unzip on a Windows machine and run Pob.exe — or install.ps1 to put \`pob\` on the PATH."

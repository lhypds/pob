#!/bin/bash
# Builds a distributable Windows release from macOS or Linux:
# Go core cross-compiled with GOOS=windows, WPF shell cross-compiled with the
# .NET SDK (EnableWindowsTargeting — .NET can compile, but not run, Windows
# desktop apps on Unix). Assembled side by side like the other shells.
# Produces: ./win/dist/Pob/  and  Pob-<version>-windows-<arch>.zip
#
# Requires: go + the .NET 8 SDK (`brew install dotnet-sdk`). Without a local
# .NET SDK, use ./build_docker.sh instead (only needs Docker).
# Override the target list with:  WIN_ARCHS="amd64 arm64" ./win/build.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
WIN_ARCHS="${WIN_ARCHS:-amd64}"

if ! command -v go &> /dev/null; then
    echo "❌ Go not found — install it first."
    exit 1
fi
if ! command -v dotnet &> /dev/null; then
    echo "❌ .NET SDK not found — install it (macOS: brew install dotnet-sdk)"
    echo "   or build with Docker instead: ./win/build_docker.sh"
    exit 1
fi
if ! command -v zip &> /dev/null; then
    echo "❌ zip not found — install it first."
    exit 1
fi

for ARCH in $WIN_ARCHS; do
    case "$ARCH" in
        amd64) RID="win-x64" ;;
        arm64) RID="win-arm64" ;;
        *) echo "❌ Unknown arch '$ARCH' (use amd64 or arm64)"; exit 1 ;;
    esac

    # ── build core (Go, cross-compiled) ──────────────────────────────────────
    echo "Building pob-core (Go, windows/$ARCH)…"
    (cd "$ROOT_DIR/core" && \
        GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 \
        go build -trimpath -ldflags="-s -w" -o bin/pob-core.exe ./cmd/pob-core)

    # ── build shell (C#/WPF, self-contained single file) ─────────────────────
    echo "Building Windows shell (release, $RID)…"
    dotnet publish "$SCRIPT_DIR/Pob.csproj" -c Release -r "$RID" \
        --self-contained true -p:PublishSingleFile=true \
        -p:IncludeNativeLibrariesForSelfExtract=true \
        -o "$SCRIPT_DIR/publish-$ARCH"

    # ── assemble ─────────────────────────────────────────────────────────────
    DIST_DIR="$SCRIPT_DIR/dist/Pob"
    ZIP_PATH="$ROOT_DIR/Pob-${VERSION}-windows-${ARCH}.zip"
    echo "Assembling dist/Pob ($ARCH)…"
    rm -rf "$SCRIPT_DIR/dist"
    mkdir -p "$DIST_DIR"
    cp "$SCRIPT_DIR/publish-$ARCH/Pob.exe" "$DIST_DIR/Pob.exe"
    cp "$ROOT_DIR/core/bin/pob-core.exe" "$DIST_DIR/pob-core.exe"
    cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

    echo "Creating ${ZIP_PATH}…"
    rm -f "$ZIP_PATH"
    (cd "$SCRIPT_DIR/dist" && zip -qr "$ZIP_PATH" Pob)

    echo ""
    echo "Done: Pob-${VERSION}-windows-${ARCH}.zip"
done

echo ""
echo "Version: $VERSION"
echo "Unzip on a Windows machine and run Pob.exe."

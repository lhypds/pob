#!/bin/bash
# Builds Pob.app bundle: Go core (pob-core) + Swift shell, assembled together.
# Produces: ./macos/macos_app/Pob.app  (ad-hoc signed, no sandbox)
#
# Usage:
#   ./build.sh              # release build
#   ./build.sh --debug      # debug build

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── options ─────────────────────────────────────────────────────────────────
CONFIG="release"
SWIFT_CONFIG_FLAG="-c release"
for arg in "$@"; do
  [[ "$arg" == "--debug" ]] && { CONFIG="debug"; SWIFT_CONFIG_FLAG=""; }
done

VERSION="$(cat "$ROOT_DIR/VERSION" 2>/dev/null || echo '0.0.1')"
APP_NAME="Pob"
BUNDLE_ID="jp.co.linktivity.pob"
MACOS_DIR="$SCRIPT_DIR"
OUTPUT_DIR="$MACOS_DIR/macos_app"
APP_BUNDLE="$OUTPUT_DIR/$APP_NAME.app"
CONTENTS="$APP_BUNDLE/Contents"
BINARY_SRC="$MACOS_DIR/.build/$CONFIG/$APP_NAME"
CORE_BINARY="$ROOT_DIR/core/bin/pob-core"
CLI_BINARY="$ROOT_DIR/core/bin/pob"

# ── clean old zips ───────────────────────────────────────────────────────────
# The macOS zip itself is made by release.sh, but a build here is still a build:
# a zip of another version is stale the moment this one starts, and release.sh
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

# ── build shell (Swift) ──────────────────────────────────────────────────────
echo "Building macOS shell ($CONFIG)…"
(cd "$MACOS_DIR" && swift build $SWIFT_CONFIG_FLAG)

# ── assemble bundle ──────────────────────────────────────────────────────────
echo "Assembling $APP_NAME.app…"
rm -rf "$APP_BUNDLE"
mkdir -p "$CONTENTS/MacOS"

cp "$BINARY_SRC" "$CONTENTS/MacOS/$APP_NAME"
cp "$CORE_BINARY" "$CONTENTS/MacOS/pob-core"
# The CLI goes to Helpers/, not MacOS/: the filesystem is case-insensitive,
# so MacOS/pob would overwrite the Pob app executable.
mkdir -p "$CONTENTS/Helpers"
cp "$CLI_BINARY" "$CONTENTS/Helpers/pob"

# ── app icon ─────────────────────────────────────────────────────────────────
echo "Generating app icon…"
mkdir -p "$CONTENTS/Resources"
ICONSET_DIR="$MACOS_DIR/.build/Pob.iconset"
ICNS_PATH="$CONTENTS/Resources/AppIcon.icns"
mkdir -p "$ICONSET_DIR"

# Generate 1024x1024 base PNG via Swift script
BASE_PNG="$MACOS_DIR/.build/pob_icon_1024.png"
swift "$MACOS_DIR/generate_icon.swift" "$BASE_PNG"

# Resize to all required iconset sizes
for SIZE in 16 32 128 256 512; do
  sips -z $SIZE $SIZE "$BASE_PNG" --out "$ICONSET_DIR/icon_${SIZE}x${SIZE}.png" > /dev/null
  DOUBLE=$((SIZE * 2))
  sips -z $DOUBLE $DOUBLE "$BASE_PNG" --out "$ICONSET_DIR/icon_${SIZE}x${SIZE}@2x.png" > /dev/null
done

iconutil -c icns "$ICONSET_DIR" -o "$ICNS_PATH"
rm -rf "$ICONSET_DIR"

# ── Info.plist ───────────────────────────────────────────────────────────────
cat > "$CONTENTS/Info.plist" << PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>             <string>$APP_NAME</string>
  <key>CFBundleDisplayName</key>      <string>$APP_NAME</string>
  <key>CFBundleIdentifier</key>       <string>$BUNDLE_ID</string>
  <key>CFBundleVersion</key>          <string>$VERSION</string>
  <key>CFBundleShortVersionString</key><string>$VERSION</string>
  <key>CFBundleExecutable</key>       <string>$APP_NAME</string>
  <key>CFBundlePackageType</key>      <string>APPL</string>
  <key>CFBundleIconFile</key>         <string>AppIcon</string>
  <key>CFBundleSignature</key>        <string>????</string>
  <key>LSMinimumSystemVersion</key>   <string>12.0</string>
  <key>NSHighResolutionCapable</key>  <true/>
  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>

  <!-- Privacy usage descriptions shown in System Settings prompts -->
  <key>NSAccessibilityUsageDescription</key>
  <string>Pob needs Accessibility access to control the mouse and keyboard on your behalf.</string>

  <key>NSScreenCaptureUsageDescription</key>
  <string>Pob needs Screen Recording access to capture the screen for AI analysis.</string>

  <key>NSLocalNetworkUsageDescription</key>
  <string>Pob serves its remote control page to your phone or another computer on this network.</string>
</dict>
</plist>
PLIST

# ── entitlements (no sandbox — CGEvent/AXUIElement require it disabled) ──────
ENTITLEMENTS="$OUTPUT_DIR/Pob.entitlements"
mkdir -p "$OUTPUT_DIR"
cat > "$ENTITLEMENTS" << ENT
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>com.apple.security.app-sandbox</key>        <false/>
  <key>com.apple.security.network.server</key>     <true/>
  <key>com.apple.security.network.client</key>     <true/>
</dict>
</plist>
ENT

# ── code sign ────────────────────────────────────────────────────────────────
# Prefer the first available Developer ID; fall back to ad-hoc (-).
IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null \
  | grep -o '"Developer ID Application:[^"]*"' | head -1 | tr -d '"' || true)

if [[ -z "$IDENTITY" ]]; then
  echo "No Developer ID found — using ad-hoc signature."
  IDENTITY="-"
fi

echo "Signing with: $IDENTITY"
# Sign the embedded Go binaries first, then the bundle.
codesign --force --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$CONTENTS/MacOS/pob-core"
codesign --force --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$CONTENTS/Helpers/pob"
codesign --force --deep --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$APP_BUNDLE"

# ── assemble dist ────────────────────────────────────────────────────────────
# What release.sh zips: the app with the guide beside it, the same folder shape
# the Linux and Windows releases unzip to. A .app on its own has nowhere to put
# a README a person can read before installing anything.
DIST_DIR="$OUTPUT_DIR/dist/Pob"
echo "Assembling dist/Pob…"
rm -rf "$OUTPUT_DIR/dist"
mkdir -p "$DIST_DIR"
ditto "$APP_BUNDLE" "$DIST_DIR/Pob.app"
cp "$MACOS_DIR/README.txt" "$DIST_DIR/README.txt"
cp "$ROOT_DIR/VERSION" "$DIST_DIR/VERSION" 2>/dev/null || true

echo ""
echo "Done: $APP_BUNDLE"
echo "  Version : $VERSION"
echo "  Config  : $CONFIG"
echo "  Signed  : $IDENTITY"
echo "  Dist    : $DIST_DIR"
echo ""
echo "Run with:  open \"$APP_BUNDLE\""

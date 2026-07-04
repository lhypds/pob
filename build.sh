#!/bin/bash
# Builds Pob.app bundle: Go core (pob-core) + Swift shell, assembled together.
# Produces: ./macos/macos_app/Pob.app  (ad-hoc signed, no sandbox)
#
# Usage:
#   ./build.sh              # release build
#   ./build.sh --debug      # debug build

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# ── options ─────────────────────────────────────────────────────────────────
CONFIG="release"
SWIFT_CONFIG_FLAG="-c release"
for arg in "$@"; do
  [[ "$arg" == "--debug" ]] && { CONFIG="debug"; SWIFT_CONFIG_FLAG=""; }
done

VERSION="$(cat VERSION 2>/dev/null || echo '0.0.1')"
APP_NAME="Pob"
BUNDLE_ID="jp.co.linktivity.pob"
MACOS_DIR="$SCRIPT_DIR/macos"
OUTPUT_DIR="$MACOS_DIR/macos_app"
APP_BUNDLE="$OUTPUT_DIR/$APP_NAME.app"
CONTENTS="$APP_BUNDLE/Contents"
BINARY_SRC="$MACOS_DIR/.build/$CONFIG/$APP_NAME"
CORE_BINARY="$SCRIPT_DIR/core/bin/pob-core"

# ── build core (Go) ──────────────────────────────────────────────────────────
echo "Building pob-core (Go)…"
(cd "$SCRIPT_DIR/core" && go build -trimpath -ldflags="-s -w" -o bin/pob-core ./cmd/pob-core)

# ── build shell (Swift) ──────────────────────────────────────────────────────
echo "Building macOS shell ($CONFIG)…"
(cd "$MACOS_DIR" && swift build $SWIFT_CONFIG_FLAG)

# ── assemble bundle ──────────────────────────────────────────────────────────
echo "Assembling $APP_NAME.app…"
rm -rf "$APP_BUNDLE"
mkdir -p "$CONTENTS/MacOS"

cp "$BINARY_SRC" "$CONTENTS/MacOS/$APP_NAME"
cp "$CORE_BINARY" "$CONTENTS/MacOS/pob-core"

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
# Sign the embedded Go core first, then the bundle.
codesign --force --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$CONTENTS/MacOS/pob-core"
codesign --force --deep --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$IDENTITY" \
  "$APP_BUNDLE"

echo ""
echo "Done: $APP_BUNDLE"
echo "  Version : $VERSION"
echo "  Config  : $CONFIG"
echo "  Signed  : $IDENTITY"
echo ""
echo "Run with:  open \"$APP_BUNDLE\""

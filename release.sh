#!/bin/bash
# Releases Pob to GitHub: builds BOTH OS shells — the macOS app bundle
# natively and the Linux/X11 shell via Docker — and publishes the two zips
# as release assets.
#
# Run on macOS with Docker installed and running.
#
# Env:
#   LINUX_ARCH=amd64|arm64   Linux target architecture (default: amd64)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(cat VERSION)"
TAG="v$VERSION"
LINUX_ARCH="${LINUX_ARCH:-amd64}"

APP_BUNDLE="macos/macos_app/Pob.app"
MACOS_ZIP="Pob-${VERSION}-macos.zip"
LINUX_ZIP="Pob-${VERSION}-linux-${LINUX_ARCH}.zip"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "❌ release.sh must run on macOS (it builds the macOS app bundle)."
  exit 1
fi
if ! command -v docker &> /dev/null || ! docker info &> /dev/null; then
  echo "❌ Docker is required for the Linux build — install/start Docker first."
  exit 1
fi

echo "==> Releasing Pob $TAG"

# ── build macOS ──────────────────────────────────────────────────────────────
echo "==> Building macOS app…"
./macos/build.sh

echo "==> Zipping $APP_BUNDLE -> $MACOS_ZIP"
rm -f "$MACOS_ZIP"
ditto -c -k --keepParent "$APP_BUNDLE" "$MACOS_ZIP"

# ── build Linux (via Docker) ─────────────────────────────────────────────────
echo "==> Building Linux/X11 (linux/$LINUX_ARCH, via Docker)…"
LINUX_ARCH="$LINUX_ARCH" ./linux-x11/build-docker.sh

if [[ ! -f "$LINUX_ZIP" ]]; then
  echo "❌ Expected $LINUX_ZIP was not produced — aborting."
  exit 1
fi

# ── release notes ────────────────────────────────────────────────────────────
read -r -p "Release notes: " NOTES
if [[ -z "$NOTES" ]]; then
  echo "Release notes are empty — aborting."
  exit 1
fi

# ── git tag ──────────────────────────────────────────────────────────────────
if git rev-parse "$TAG" &>/dev/null; then
  echo "==> Tag $TAG already exists, skipping tag creation."
else
  echo "==> Creating git tag $TAG"
  git tag -a "$TAG" -m "Release $TAG"
  git push origin "$TAG"
fi

# ── github release ────────────────────────────────────────────────────────────
echo "==> Creating GitHub release $TAG"
gh release create "$TAG" \
  --title "Pob $TAG" \
  --notes "$NOTES" \
  "$MACOS_ZIP" \
  "$LINUX_ZIP"

echo ""
echo "Released: $TAG"
echo "Assets:   $MACOS_ZIP"
echo "          $LINUX_ZIP"

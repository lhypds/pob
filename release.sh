#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(cat VERSION)"
TAG="v$VERSION"
APP_BUNDLE="macos/macos_app/Pob.app"
ZIP_NAME="Pob-${VERSION}-macos.zip"

echo "==> Releasing Pob $TAG"

# ── build ────────────────────────────────────────────────────────────────────
echo "==> Building..."
./build.sh

# ── zip ──────────────────────────────────────────────────────────────────────
echo "==> Zipping $APP_BUNDLE -> $ZIP_NAME"
rm -f "$ZIP_NAME"
ditto -c -k --keepParent "$APP_BUNDLE" "$ZIP_NAME"

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
  "$ZIP_NAME"

echo ""
echo "Released: $TAG"
echo "Asset:    $ZIP_NAME"

#!/bin/bash
# Releases Pob to GitHub and publishes the zips as release assets.
#
# What gets built depends on the SYSTEM file (see ./setup.sh):
#   macos      the macOS app bundle natively, plus the Linux/X11 shell via
#              Docker (./linux-x11/build_docker.sh) and the Windows shell via
#              Docker (./win/build_docker.sh) for every architecture in
#              LINUX_ARCHS / WIN_ARCHS — requires Docker installed and running.
#   linux-*    the Linux/X11 shell natively (./linux-x11/build.sh) for the
#              host architecture only.
#
# Env:
#   LINUX_ARCHS="amd64 arm64"   Linux target architectures (default: both;
#                               macOS/Docker builds only)
#   WIN_ARCHS="amd64 arm64"     Windows target architectures (default: both;
#                               macOS/Docker builds only)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(cat VERSION)"
TAG="v$VERSION"
LINUX_ARCHS="${LINUX_ARCHS:-amd64 arm64}"
WIN_ARCHS="${WIN_ARCHS:-amd64 arm64}"

SYSTEM="$( { tr -d '[:space:]' < SYSTEM; } 2>/dev/null || true)"
if [[ -z "$SYSTEM" ]]; then
  echo "❌ No SYSTEM file found — run ./setup.sh first."
  exit 1
fi

echo "==> Releasing Pob $TAG (SYSTEM: $SYSTEM)"

ASSETS=()

if [[ "$SYSTEM" == "macos" ]]; then
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "❌ SYSTEM is macos but this host is not macOS."
    exit 1
  fi
  if ! command -v docker &> /dev/null || ! docker info &> /dev/null; then
    echo "❌ Docker is required for the Linux build — install/start Docker first."
    exit 1
  fi

  # ── build macOS ────────────────────────────────────────────────────────────
  APP_BUNDLE="macos/macos_app/Pob.app"
  MACOS_ZIP="Pob-${VERSION}-macos.zip"

  echo "==> Building macOS app…"
  ./macos/build.sh

  echo "==> Zipping $APP_BUNDLE -> $MACOS_ZIP"
  rm -f "$MACOS_ZIP"
  ditto -c -k --keepParent "$APP_BUNDLE" "$MACOS_ZIP"
  ASSETS+=("$MACOS_ZIP")

  # ── build Linux (via Docker, one zip per architecture) ─────────────────────
  for ARCH in $LINUX_ARCHS; do
    echo "==> Building Linux/X11 (linux/$ARCH, via Docker)…"
    LINUX_ARCH="$ARCH" ./linux-x11/build_docker.sh

    LINUX_ZIP="Pob-${VERSION}-linux-${ARCH}.zip"
    if [[ ! -f "$LINUX_ZIP" ]]; then
      echo "❌ Expected $LINUX_ZIP was not produced — aborting."
      exit 1
    fi
    ASSETS+=("$LINUX_ZIP")
  done

  # ── build Windows (via Docker, one zip per architecture) ───────────────────
  for ARCH in $WIN_ARCHS; do
    echo "==> Building Windows (windows/$ARCH, via Docker)…"
    WIN_ARCHS="$ARCH" ./win/build_docker.sh

    WIN_ZIP="Pob-${VERSION}-windows-${ARCH}.zip"
    if [[ ! -f "$WIN_ZIP" ]]; then
      echo "❌ Expected $WIN_ZIP was not produced — aborting."
      exit 1
    fi
    ASSETS+=("$WIN_ZIP")
  done
elif [[ "$SYSTEM" == linux-* ]]; then
  # ── build Linux (natively, host architecture) ──────────────────────────────
  case "$(uname -m)" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) ARCH="$(uname -m)" ;;
  esac

  echo "==> Building Linux/X11 (linux/$ARCH, native)…"
  ./linux-x11/build.sh

  LINUX_ZIP="Pob-${VERSION}-linux-${ARCH}.zip"
  if [[ ! -f "$LINUX_ZIP" ]]; then
    echo "❌ Expected $LINUX_ZIP was not produced — aborting."
    exit 1
  fi
  ASSETS+=("$LINUX_ZIP")
else
  echo "❌ Unknown SYSTEM '$SYSTEM' — run ./setup.sh again."
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
  "${ASSETS[@]}"

echo ""
echo "Released: $TAG"
for Z in "${ASSETS[@]}"; do
  echo "  Asset:  $Z"
done

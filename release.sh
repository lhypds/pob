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

normalize_version() {
  local v="$1"
  v="${v#v}"
  v="$(printf '%s' "$v" | tr -d '[:space:]')"
  printf '%s' "$v"
}

version_compare() {
  local lhs="$1"
  local rhs="$2"
  local IFS='.'
  local -a a_parts b_parts
  local max_len i a_seg b_seg

  read -r -a a_parts <<< "$lhs"
  read -r -a b_parts <<< "$rhs"

  max_len="${#a_parts[@]}"
  if [ "${#b_parts[@]}" -gt "$max_len" ]; then
    max_len="${#b_parts[@]}"
  fi

  for ((i = 0; i < max_len; i++)); do
    a_seg="${a_parts[i]:-0}"
    b_seg="${b_parts[i]:-0}"
    if ! [[ "$a_seg" =~ ^[0-9]+$ ]] || ! [[ "$b_seg" =~ ^[0-9]+$ ]]; then
      echo "Error: VERSION contains non-numeric segments ($lhs vs $rhs)."
      exit 1
    fi

    if ((10#$a_seg > 10#$b_seg)); then
      echo 1
      return
    fi
    if ((10#$a_seg < 10#$b_seg)); then
      echo -1
      return
    fi
  done

  echo 0
}

bump_version_interactive() {
  local current="$1"
  local IFS='.'
  local -a parts
  local count choice idx i

  read -r -a parts <<< "$current"
  count="${#parts[@]}"
  if [ "$count" -eq 0 ]; then
    echo "Error: invalid VERSION '$current'."
    exit 1
  fi

  for i in "${parts[@]}"; do
    if ! [[ "$i" =~ ^[0-9]+$ ]]; then
      echo "Error: VERSION contains non-numeric segments ($current)."
      exit 1
    fi
  done

  read -r -p "VERSION $current equals latest release. Which segment to bump from right? [1=last, 2=second last, ...] (default: 1): " choice
  choice="${choice:-1}"
  if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt "$count" ]; then
    echo "Error: invalid segment selection '$choice'."
    exit 1
  fi

  idx=$((count - choice))
  parts[idx]=$((10#${parts[idx]} + 1))
  for ((i = idx + 1; i < count; i++)); do
    parts[i]=0
  done

  local result="${parts[0]}"
  for ((i = 1; i < count; i++)); do
    result+=".${parts[i]}"
  done
  printf '%s' "$result"
}

prepare_version_for_release() {
  if [ ! -f VERSION ]; then
    echo "Error: VERSION file not found."
    exit 1
  fi

  if ! command -v gh &>/dev/null; then
    echo "Error: GitHub CLI (gh) is required."
    exit 1
  fi
  if ! gh auth status &>/dev/null; then
    echo "Error: gh is not authenticated. Run: gh auth login"
    exit 1
  fi

  local current draft_tag draft_tags latest_tag latest cmp new_version branch
  current="$(normalize_version "$(cat VERSION)")"
  if [ -z "$current" ]; then
    echo "Error: VERSION file is empty."
    exit 1
  fi

  if ! draft_tags="$(gh release list --limit 1000 --json tagName,isDraft \
    --jq '.[] | select(.isDraft) | .tagName' 2>/dev/null)"; then
    echo "Error: unable to check GitHub for draft releases."
    exit 1
  fi
  if [ -n "$draft_tags" ]; then
    echo "Warning: GitHub draft release(s) found:"
    while IFS= read -r draft_tag; do
      echo "  - $draft_tag"
    done <<< "$draft_tags"
    echo "Review, publish, or delete the draft release(s) before continuing."
    exit 1
  fi

  latest_tag="$(gh release view --json tagName --jq '.tagName' 2>/dev/null || true)"
  if [ "$latest_tag" = "null" ]; then
    latest_tag=""
  fi

  if [ -z "$latest_tag" ]; then
    echo "No existing GitHub release found. Releasing VERSION $current."
    VERSION="$current"
    return
  fi

  latest="$(normalize_version "$latest_tag")"
  cmp="$(version_compare "$current" "$latest")"

  if [ "$cmp" -gt 0 ]; then
    echo "VERSION $current is greater than latest release $latest. Continue releasing."
    VERSION="$current"
    return
  fi

  if [ "$cmp" -lt 0 ]; then
    echo "Error: VERSION $current is lower than latest release $latest."
    exit 1
  fi

  new_version="$(bump_version_interactive "$current")"
  printf '%s\n' "$new_version" > VERSION

  git add VERSION
  git commit -m "$new_version"

  branch="$(git branch --show-current 2>/dev/null || true)"
  if [ -n "$branch" ]; then
    git push origin "$branch"
  else
    git push
  fi

  echo "VERSION bumped to $new_version, committed, and pushed."
  VERSION="$new_version"
}

prepare_version_for_release
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
  # The zip holds the Pob folder macos/build.sh assembles — Pob.app with the
  # README and the LICENSE beside it — so all three releases unzip to the same
  # shape.
  MACOS_DIST="macos/macos_app/dist/Pob"
  MACOS_ZIP="Pob-${VERSION}-macos.zip"

  echo "==> Building macOS app…"
  ./macos/build.sh

  echo "==> Zipping $MACOS_DIST -> $MACOS_ZIP"
  rm -f "$MACOS_ZIP"
  # --sequesterRsrc keeps the metadata twins macOS attaches to every file out
  # of the folder itself (they go under __MACOSX), so what unzips beside
  # Pob.app is the README and the LICENSE and nothing else.
  ditto -c -k --sequesterRsrc --keepParent "$MACOS_DIST" "$MACOS_ZIP"
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

# ── cleanup ──────────────────────────────────────────────────────────────────
echo "==> Cleaning up zip files…"
rm -f Pob-*.zip

#!/usr/bin/env bash
# Installs the `pob` skill — skills/pob in this folder — where Claude Code looks
# for a user's skills, so any session on this machine can start Pob and drive
# the computer through it.
#
# Where that is:
#   macOS, Linux   ~/.claude/skills/pob
#   Windows        %USERPROFILE%\.claude\skills\pob   (run this from Git Bash)
#
# CLAUDE_CONFIG_DIR moves ~/.claude, the way it does for Claude Code itself.
#
# Usage:
#   ./install-skill.sh               # copy the skill into place
#   ./install-skill.sh --link        # symlink it instead, so edits here are live
#   ./install-skill.sh --uninstall   # take it back out
#   ./install-skill.sh --dir DIR     # install into DIR/pob instead

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SKILL_NAME="pob"
SOURCE="$SCRIPT_DIR/skills/$SKILL_NAME"

usage() {
    sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
}

# ── where skills live on this machine ────────────────────────────────────────

home_dir() {
    case "$(uname -s)" in
        MINGW*|MSYS*|CYGWIN*)
            # Git Bash's HOME is usually the profile folder, but it can be set
            # to anything; Claude Code on Windows reads the profile itself.
            if [ -n "${USERPROFILE:-}" ] && command -v cygpath >/dev/null 2>&1; then
                cygpath -u "$USERPROFILE"
                return
            fi
            ;;
    esac
    echo "$HOME"
}

skills_dir() {
    if [ -n "${CLAUDE_CONFIG_DIR:-}" ]; then
        echo "$CLAUDE_CONFIG_DIR/skills"
    else
        echo "$(home_dir)/.claude/skills"
    fi
}

# ── options ──────────────────────────────────────────────────────────────────

ACTION="install"
LINK=""
SKILLS_DIR=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --link)      LINK=1 ;;
        --uninstall) ACTION="uninstall" ;;
        --dir)
            [ "$#" -ge 2 ] || { echo "--dir needs a directory" >&2; exit 1; }
            SKILLS_DIR="$2"
            shift ;;
        -h|--help)   usage; exit 0 ;;
        *)           echo "unknown option: $1" >&2; usage >&2; exit 1 ;;
    esac
    shift
done

[ -n "$SKILLS_DIR" ] || SKILLS_DIR="$(skills_dir)"
TARGET="$SKILLS_DIR/$SKILL_NAME"

# is_ours says whether what is at the target is this skill — a link to it, or a
# copy with its SKILL.md — so nothing else that happens to be called "pob" is
# ever removed.
is_ours() {
    [ -L "$TARGET" ] && return 0
    [ -f "$TARGET/SKILL.md" ] && grep -q "^name: $SKILL_NAME\$" "$TARGET/SKILL.md"
}

remove_target() {
    if [ -L "$TARGET" ]; then
        rm -f "$TARGET"
    else
        rm -rf "$TARGET"
    fi
}

# ── uninstall ────────────────────────────────────────────────────────────────

if [ "$ACTION" = "uninstall" ]; then
    if [ ! -e "$TARGET" ] && [ ! -L "$TARGET" ]; then
        echo "Nothing to remove: $TARGET is not there."
        exit 0
    fi
    is_ours || { echo "$TARGET is not the $SKILL_NAME skill — leaving it alone." >&2; exit 1; }
    remove_target
    echo "Removed $TARGET"
    exit 0
fi

# ── install ──────────────────────────────────────────────────────────────────

[ -f "$SOURCE/SKILL.md" ] || { echo "No skill at $SOURCE — run this from the Pob repository." >&2; exit 1; }

if [ -e "$TARGET" ] || [ -L "$TARGET" ]; then
    is_ours || { echo "$TARGET exists and is not the $SKILL_NAME skill — move it away first." >&2; exit 1; }
    remove_target
fi

mkdir -p "$SKILLS_DIR"
if [ -n "$LINK" ]; then
    ln -s "$SOURCE" "$TARGET"
    echo "Linked $TARGET -> $SOURCE"
else
    cp -R "$SOURCE" "$TARGET"
    echo "Installed $TARGET"
fi

if ! command -v pob >/dev/null 2>&1; then
    echo "Note: the pob command is not on the PATH yet — the skill needs it (see README.md)."
elif ! pob help 2>/dev/null | grep -q '^  view '; then
    echo "Note: the installed pob has no view/click/type commands yet — run ./install.sh or pob update."
fi
echo "Start a new Claude Code session and ask it to operate the computer, or type /$SKILL_NAME."

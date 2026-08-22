#!/bin/bash
# Takes an install this folder made back off again: the app and the `pob` link
# go, ~/.pob stays. The other half of ./install.sh, which does the work — this
# is the name you look for when you want it gone.
#
# Usage:
#   ./uninstall.sh               # remove the install for this user
#   sudo ./uninstall.sh          # remove a system-wide one
#
# Options:
#   --os OS        macos | linux-x11    (default: the SYSTEM file)
#   --prefix DIR   where the app was installed (if not the default)
#   --bin DIR      where the pob symlink went  (if not the default)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

usage() {
    cat <<EOF
Removes an install ./install.sh made. ~/.pob is left alone — settings,
instances and logs stay where they are.

Usage:
  ./uninstall.sh               # remove the install for this user
  sudo ./uninstall.sh          # remove a system-wide one

Options:
  --os OS        macos | linux-x11    (default: the SYSTEM file)
  --prefix DIR   where the app was installed (if not the default)
  --bin DIR      where the pob symlink went  (if not the default)
EOF
}

for ARG in "$@"; do
    case "$ARG" in
        -h|--help) usage; exit 0 ;;
    esac
done

exec "$SCRIPT_DIR/install.sh" --uninstall "$@"

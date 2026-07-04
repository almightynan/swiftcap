#!/usr/bin/env bash
# Removes swiftcap binaries and the desktop entry installed by install.sh.
#
# Usage:
#   ./scripts/uninstall.sh [--prefix DIR]

set -euo pipefail

PREFIX="${SWIFTCAP_PREFIX:-/usr/local}"
while [ $# -gt 0 ]; do
  case "$1" in
    --prefix)
      PREFIX="${2:?--prefix requires a directory}"
      shift 2
      ;;
    --prefix=*)
      PREFIX="${1#*=}"
      shift
      ;;
    *)
      echo "Usage: uninstall.sh [--prefix DIR]" >&2
      exit 1
      ;;
  esac
done

SUDO=""
[ "$(id -u)" -ne 0 ] && [ "$PREFIX" = "/usr/local" ] && SUDO="sudo"

$SUDO rm -f "$PREFIX/bin/swiftcap"
$SUDO rm -f "$PREFIX/bin/swiftcap-ui"
$SUDO rm -f "$PREFIX/share/applications/swiftcap.desktop"

echo "swiftcap removed from $PREFIX"

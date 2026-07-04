#!/usr/bin/env bash
# swiftcap installer.
#
# Detects the host distro's package manager, installs the runtime
# dependencies swiftcap needs (ffmpeg, gstreamer plugins, xdg-desktop-portal,
# pipewire, a clipboard tool, and the X11/GL libs Fyne links against),
# builds the CLI and UI from source, and installs both plus a desktop
# launcher entry.
#
# Usage:
#   ./scripts/install.sh [--prefix DIR] [--no-deps] [--help]
#
# Env overrides:
#   SWIFTCAP_PREFIX   same as --prefix

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${SWIFTCAP_PREFIX:-}"
SKIP_DEPS=0

log()  { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$1" >&2; }
die()  { printf '\033[1;31mERROR:\033[0m %s\n' "$1" >&2; exit 1; }

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
    --no-deps)
      SKIP_DEPS=1
      shift
      ;;
    --help|-h)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *)
      die "Unknown option: $1 (see --help)"
      ;;
  esac
done

if [ -z "$PREFIX" ]; then
  if [ "$(id -u)" -eq 0 ] || command -v sudo >/dev/null 2>&1; then
    PREFIX="/usr/local"
  else
    PREFIX="$HOME/.local"
    warn "No sudo found, installing to $PREFIX instead. Make sure $PREFIX/bin is on your PATH."
  fi
fi
BIN_DIR="$PREFIX/bin"
SHARE_DIR="$PREFIX/share/applications"

SUDO=""
if [ "$(id -u)" -ne 0 ] && [ "$PREFIX" = "/usr/local" ]; then
  SUDO="sudo"
fi

detect_pkg_manager() {
  if command -v apt-get >/dev/null 2>&1; then echo apt; return; fi
  if command -v dnf >/dev/null 2>&1; then echo dnf; return; fi
  if command -v yum >/dev/null 2>&1; then echo yum; return; fi
  if command -v pacman >/dev/null 2>&1; then echo pacman; return; fi
  if command -v zypper >/dev/null 2>&1; then echo zypper; return; fi
  if command -v apk >/dev/null 2>&1; then echo apk; return; fi
  echo unknown
}

install_deps() {
  local pm="$1"
  case "$pm" in
    apt)
      $SUDO apt-get update -y
      $SUDO apt-get install -y \
        ffmpeg gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad \
        xdg-desktop-portal pipewire xclip wl-clipboard \
        libgl1 libx11-6 libxcursor1 libxrandr2 libxinerama1 libxi6
      ;;
    dnf|yum)
      warn "Fedora/RHEL distros ship ffmpeg via RPM Fusion, not through the base repos. If the installation below fails on ffmpeg, enable it first. Read how on: https://rpmfusion.org/Configuration"
      $SUDO "$pm" install -y \
        ffmpeg gstreamer1-plugins-base gstreamer1-plugins-good gstreamer1-plugins-bad-free \
        xdg-desktop-portal pipewire xclip wl-clipboard \
        mesa-libGL libX11 libXcursor libXrandr libXinerama libXi
      ;;
    pacman)
      $SUDO pacman -Sy --needed --noconfirm \
        ffmpeg gst-plugins-base gst-plugins-good gst-plugins-bad \
        xdg-desktop-portal pipewire xclip wl-clipboard \
        mesa libx11 libxcursor libxrandr libxinerama libxi
      ;;
    zypper)
      $SUDO zypper --non-interactive install \
        ffmpeg gstreamer-plugins-base gstreamer-plugins-good gstreamer-plugins-bad \
        xdg-desktop-portal pipewire xclip wl-clipboard \
        Mesa-libGL1 libX11-6 libXcursor1 libXrandr2 libXinerama1 libXi6
      ;;
    apk)
      $SUDO apk add --no-cache \
        ffmpeg gst-plugins-base gst-plugins-good gst-plugins-bad \
        xdg-desktop-portal pipewire xclip wl-clipboard \
        mesa-gl libx11 libxcursor libxrandr libxinerama libxi
      ;;
    *)
      warn "Unable to recognize the package manager on this machine. Please install these dependencies manually: ffmpeg, gstreamer (base/good/bad plugins), xdg-desktop-portal, pipewire, xclip [or] wl-clipboard."
      return 1
      ;;
  esac
}

check_go() {
  command -v go >/dev/null 2>&1 || die "Go is not installed. Install Go 1.22+ from https://go.dev/dl/ and re-run this script."
  local have want
  have="$(go env GOVERSION | sed 's/go//')"
  want="1.22"
  if [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)" != "$want" ]; then
    die "Go $have found, but swiftcap needs Go $want or newer."
  fi
  log "Go $have OK"
}

build_swiftcap() {
  log "Building swiftcap (CLI)..."
  (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$REPO_ROOT/swiftcap" ./cmd/swiftcap)
  log "Building swiftcap-ui (this links against system GL/X11 libs, so it needs cgo)..."
  (cd "$REPO_ROOT" && go build -trimpath -ldflags "-s -w" -o "$REPO_ROOT/swiftcap-ui" ./cmd/swiftcap-ui)
}

install_binaries() {
  log "Installing binaries to $BIN_DIR"
  $SUDO mkdir -p "$BIN_DIR"
  $SUDO install -m 0755 "$REPO_ROOT/swiftcap" "$BIN_DIR/swiftcap"
  $SUDO install -m 0755 "$REPO_ROOT/swiftcap-ui" "$BIN_DIR/swiftcap-ui"
}

install_desktop_entry() {
  $SUDO mkdir -p "$SHARE_DIR"
  printf '%s\n' \
    '[Desktop Entry]' \
    'Type=Application' \
    'Name=swiftcap' \
    'Comment=Lightweight screen recorder and screenshot tool' \
    "Exec=$BIN_DIR/swiftcap-ui" \
    'Icon=camera-video' \
    'Terminal=false' \
    'Categories=AudioVideo;Recorder;Utility;' \
    | $SUDO tee "$SHARE_DIR/swiftcap.desktop" >/dev/null
}

main() {
  if [ "$SKIP_DEPS" -eq 0 ]; then
    log "Detecting package manager..."
    pm="$(detect_pkg_manager)"
    log "Found: $pm"
    install_deps "$pm" || warn "Dependency install was skipped or partial — verify ffmpeg/gstreamer/xdg-desktop-portal/pipewire/clipboard tooling manually."
  else
    log "Skipping dependency install (--no-deps)"
  fi

  check_go
  build_swiftcap
  install_binaries
  install_desktop_entry

  log "Installed swiftcap + swiftcap-ui to $BIN_DIR"
  case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *) warn "$BIN_DIR is not on your PATH. Add this to your shell rc: export PATH=\"$BIN_DIR:\$PATH\"" ;;
  esac
  log "Done. Run 'swiftcap-ui' to launch the app, or 'swiftcap --help' for the CLI."
}

main

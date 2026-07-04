<div align="center">

# swiftcap

Fast, lightweight screen recorder and screenshot tool for Linux.

[![Go Version](https://img.shields.io/github/go-mod/go-version/almightynan/swiftcap?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/github/license/almightynan/swiftcap?color=blue)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Linux-333?logo=linux&logoColor=white)](#)
[![Built with Fyne](https://img.shields.io/badge/built%20with-Fyne-7c4dff)](https://fyne.io)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](#contributing)
[![Last Commit](https://img.shields.io/github/last-commit/almightynan/swiftcap)](https://github.com/almightynan/swiftcap/commits)

</div>

swiftcap records your screen and takes screenshots on X11 and Wayland. It ships a CLI (`swiftcap`) for scripting and a desktop app (`swiftcap-ui`) built with [Fyne](https://fyne.io).

## Features

- Screen recording on X11 (x11grab) and Wayland (xdg-desktop-portal)
- Region, freeform, and full-screen capture
- Pause and resume, with segments joined on stop
- Screenshots copied to the clipboard with a desktop notification
- System tray controls with a live timer
- In-app preview with a built-in video player
- Countdown before capture

## Install

```bash
git clone https://github.com/almightynan/swiftcap.git
cd swiftcap
./scripts/install.sh
```

The installer detects your package manager (apt, dnf, pacman, zypper, apk), pulls in the runtime dependencies, builds both binaries, and adds a desktop entry. It needs Go 1.22+.

Common flags:

```bash
./scripts/install.sh --prefix ~/.local   # install somewhere else
./scripts/install.sh --no-deps           # skip dependency install
```

Uninstall with `./scripts/uninstall.sh`.

## Usage

Launch the app from your menu or run `swiftcap-ui`. Set the FPS and output directory, then start a recording or grab a screenshot.

The CLI works standalone:

```bash
swiftcap record --out out.mp4 --fps 60 --audio on
swiftcap record --out out.mp4 --region 1280x720+0+0
swiftcap screenshot --out shot.png
swiftcap --help
```

## Dependencies

- `ffmpeg` (required)
- `xclip` or `wl-clipboard` for clipboard support
- `xdg-desktop-portal`, `pipewire`, and `gstreamer` plugins for Wayland capture
- OpenGL and X11 libs for the GUI (`libGL`, `libX11`, `libXcursor`, `libXrandr`, `libXi`)

On Fedora and RHEL, `ffmpeg` lives in [RPM Fusion](https://rpmfusion.org/Configuration) rather than the base repos.

## Build from source

```bash
go build -o swiftcap ./cmd/swiftcap
go build -o swiftcap-ui ./cmd/swiftcap-ui
```

The CLI is pure Go. The UI uses cgo to link against system GL/X11.

## Contributing

Issues and pull requests are welcome. Please run `go build ./...` and `go test ./...` before opening a PR.
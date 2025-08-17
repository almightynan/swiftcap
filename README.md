# swiftcap
swiftcap is aimed to be a lightweight and minimal screen utility tool mainly for linux with cross platform support. 

> swift in swiftcap stands for fast, and cap is a shorter form of capture.

This repository contains two main components:

- **swiftcap-go**: A fast, low-resource, cross-platform screen recorder and screenshot CLI, written in Go. Supports X11, Wayland, and other major platforms.
- **swiftcap-ui**: A minimal graphical frontend for swiftcap, written in C++ using wxWidgets.

Please note, this project is still in development. A fully downloadable version is still not available. If you would like to try it out and contribute new features, compile from source. Build instructions are given further below. 

---

## Directory Structure

```
swiftcap-go/         # Go CLI tool for screen capture and recording
    cmd/swiftcap/    # Main entry point for CLI
    internal/        # Internal packages (cli, detect, execx, ...)
    packaging/       # Packaging scripts and docs
    scripts/         # Build and dependency scripts

swiftcap-ui/         # C++ wxWidgets GUI frontend
    main.cpp         # Main application source
    Makefile         # Build instructions for the UI
    swiftcap_ui      # The compiled C++ binary for UI
```

---

## swiftcap-go (CLI)

### Features

- Record screen or take screenshots via command line.
- Supports X11 (via ffmpeg) and Wayland (via xdg-desktop-portal).
- Audio capture (PulseAudio).
- Region selection, monitor selection, format/container options.
- Minimal runtime dependencies.

### Dependencies

- Go 1.22+
- ffmpeg
- gstreamer1.0, gst-plugins-base, gst-plugins-good, gst-plugins-bad
- xdg-desktop-portal (+ backend)
- PipeWire

#### Install dependencies (Debian/Ubuntu):

```
sudo apt install ffmpeg gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad xdg-desktop-portal pipewire
```

### Build

To build a static binary:

```
cd swiftcap-go/scripts
./build_static.sh
```

The resulting binary will be `swiftcap` in the `swiftcap-go/` directory.

### Usage

```
swiftcap record --out out.mp4 --fps 60 --region 1280x720+0+0 --audio on --container mp4
swiftcap screenshot --out shot.png --region 1280x720+0+0 --format png
```

Run `swiftcap` with no arguments for full usage and options.

---

## swiftcap-ui (GUI)

### Features

- Minimal wxWidgets-based frontend for launching screen capture and screenshot tasks.
- Calls the `swiftcap` CLI under the hood.

### Dependencies

- wxWidgets 3.2+
- g++ (or compatible C++ compiler)

#### Build

```
cd swiftcap-ui
make
```

This produces the `swiftcap_frontend` binary.

---

## Packaging

See `swiftcap-go/README.md` for packaging details and additional notes.

## Notes

- The CLI is the primary interface; the GUI is optional and minimal.
- For bug reports or contributions, please open an issue or pull request.

---

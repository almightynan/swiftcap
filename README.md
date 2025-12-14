# swiftcap
swiftcap is a lightweight screen utility tool targeting Linux (with growing cross-platform support). The CLI recorder is designed for speed and low resource usage, while the UI front-end focuses on a simple workflow: countdown, record, pause/resume, and quick access to completed captures.

> swift = fast, cap = capture.

---

## Layout

```
cmd/
    swiftcap/     # CLI entrypoint
    swiftcap-ui/  # Go desktop UI wrapper around the CLI
internal/         # Shared packages (cli parsing, portal helpers, recorders, GUI logic, etc.)
packaging/        # Packaging metadata
scripts/          # Build helpers
```

Everything in this repository is pure Go – no C or C++ sources remain.

---

## CLI (`cmd/swiftcap`)

### Highlights
- Screen recording via ffmpeg on X11, Wayland screencasting via xdg-desktop-portal.
- Screenshot capture for X11/Wayland/fallback modes.
- Audio capture (PulseAudio/PipeWire) with bitrate, region, cursor, thread, and duration controls.
- Minimal runtime deps beyond ffmpeg + portal stacks.

### Dependencies
- Go 1.22+
- ffmpeg
- gstreamer1.0, gst-plugins-base, gst-plugins-good, gst-plugins-bad
- xdg-desktop-portal (+ backend) and PipeWire

#### Debian/Ubuntu helper
```
sudo apt install ffmpeg gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
                 gstreamer1.0-plugins-bad xdg-desktop-portal pipewire
```

### Build

```
cd scripts
./build_static.sh   # produces ./swiftcap
```

### Usage

```
swiftcap record --out out.mp4 --fps 60 --region 1280x720+0+0 --audio on --container mp4
swiftcap screenshot --out shot.png --region 1280x720+0+0 --format png
```

Run `swiftcap --help` for the full flag set.

---

## UI (`cmd/swiftcap-ui`)

The UI is now written in Go (Fyne). It wraps the CLI, providing:

- Countdown overlay (click to abort) before recording starts.
- System tray integration with dynamic icons, elapsed timer, start/stop/pause/resume entries.
- Pause/resume implemented by segmenting recordings and concatenating with ffmpeg on stop.
- Toast notification inside the app with “Open Folder / Open File”.
- Same video directory + ffmpeg requirements as the CLI.

### Build & run

```
go build ./cmd/swiftcap-ui
./swiftcap-ui
```

The UI searches for the `swiftcap` CLI in:
1. `SWIFTCAP_CLI_PATH` (if set),
2. The same directory as the UI binary,
3. `$PATH`.

---

## Packaging

Packaging metadata lives under `packaging/`. See `scripts/` for static build helpers and dependency checks.

---

## Contributing

Open issues/PRs for bugs or feature ideas. Please make sure `go test ./...` and linting pass before submitting.

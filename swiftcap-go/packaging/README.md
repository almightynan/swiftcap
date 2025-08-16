# swiftcap-go
compiled binary is available as `swiftcap` in this directory.

## Dependencies
- ffmpeg
- gstreamer1.0
- gst-plugins-base
- gst-plugins-good
- gst-plugins-bad
- xdg-desktop-portal (+backend)
- PipeWire

## Install (Debian/Ubuntu)
```
sudo apt install ffmpeg gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad xdg-desktop-portal pipewire
```

## Usage
```
swiftcap record --out out.mp4 --fps 60 --region 1280x720+0+0 --audio on --container mp4
swiftcap screenshot --out shot.png --region 1280x720+0+0 --format png
```

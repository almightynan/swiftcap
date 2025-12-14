#!/bin/bash
set -e
missing=""
command -v ffmpeg >/dev/null 2>&1 || missing="$missing ffmpeg"
command -v gst-launch-1.0 >/dev/null 2>&1 || missing="$missing gstreamer"
command -v xdg-desktop-portal >/dev/null 2>&1 || missing="$missing xdg-desktop-portal"
if [ -n "$missing" ]; then
  echo "Missing dependencies:$missing"
  exit 1
else
  echo "All dependencies present."
fi

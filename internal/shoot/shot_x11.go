package shoot

import (
	"os/exec"
)

func ScreenshotX11(region, out, format string) error {
	cmd := exec.Command("ffmpeg", "-f", "x11grab", "-video_size", region, "-i", "$DISPLAY+0,0", "-vframes", "1", out)
	return cmd.Run()
}

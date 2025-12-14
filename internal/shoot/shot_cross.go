package shoot

import (
	"fmt"
	"os/exec"
	"runtime"
)

// ScreenshotCross handles screenshots for all major platforms
func ScreenshotCross(region, out, format string) error {
	osys := runtime.GOOS
	switch osys {
	case "linux":
		// X11 or wayland handled in main.go via session type
		return fmt.Errorf("ScreenshotCross should not be called for Linux; use ScreenshotX11 or ScreenshotWayland")
	case "windows":
		// use ffmpeg gdigrab
		cmd := exec.Command("ffmpeg", "-f", "gdigrab", "-framerate", "1", "-i", "desktop", "-vframes", "1", out)
		return cmd.Run()
	case "darwin":
		// use ffmpeg avfoundation
		cmd := exec.Command("ffmpeg", "-f", "avfoundation", "-framerate", "1", "-i", "1:none", "-vframes", "1", out)
		return cmd.Run()
	default:
		return fmt.Errorf("unsupported OS for screenshot")
	}
}

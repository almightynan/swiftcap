package shoot

import "swiftcap/internal/portal"

func ScreenshotWayland(region, out string) error {
	return portal.TakeScreenshot(region, out)
}

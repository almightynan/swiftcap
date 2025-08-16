package shoot

import "swiftcap-go/internal/portal"

func ScreenshotWayland(region, out string) error {
	return portal.TakeScreenshot(region, out)
}

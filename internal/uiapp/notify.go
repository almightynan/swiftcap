package uiapp

import (
	"os"
	"os/exec"

	"github.com/godbus/dbus/v5"

	"swiftcap/internal/detect"
)

// copyImageToClipboard copies the image at path onto the system clipboard as
// real image data (so it can be pasted into other apps), not just the path
// string. Best-effort — callers should not treat a failure as fatal.
func copyImageToClipboard(path string) error {
	session, err := detect.Session()
	if err != nil {
		return err
	}

	switch session {
	case detect.SessionWayland:
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		cmd := exec.Command("wl-copy", "--type", "image/png")
		cmd.Stdin = f
		return cmd.Run()
	default:
		return exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-i", path).Run()
	}
}

// sendNotification fires a desktop notification via the freedesktop
// Notifications D-Bus service (works under both X11 and Wayland session
// compositors without depending on a notify-send binary being installed).
func sendNotification(summary, body string) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
	call := obj.Call(
		"org.freedesktop.Notifications.Notify", 0,
		"SwiftCap", uint32(0), "",
		summary, body,
		[]string{}, map[string]dbus.Variant{}, int32(5000),
	)
	return call.Err
}

/*
	we use this package to detect the type of window/session the user
	is on. will need to add support for all linux session types.
*/

package detect

import (
	"os"
	"runtime"
)

type SessionType int

const (
	SessionNone SessionType = iota
	SessionWayland
	SessionX11
	SessionWindows
	SessionMac
	SessionUnknown
)

func Session() (SessionType, error) {
	goos := runtime.GOOS
	switch goos {
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			return SessionWayland, nil
		}
		if os.Getenv("DISPLAY") != "" {
			return SessionX11, nil
		}
		return SessionNone, ErrNoDisplay // todo: make it work across all session types on linux
	case "windows":
		return SessionWindows, nil
	case "darwin":
		return SessionMac, nil // if macos fails, open an issue or a pr. i have and never will use macos
	default:
		return SessionUnknown, ErrNoDisplay
	}
}

var ErrNoDisplay = &NoDisplayError{}

type NoDisplayError struct{}

func (e *NoDisplayError) Error() string {
	return "E_NO_DISPLAY=10: No display/session detected or unsupported OS."
}

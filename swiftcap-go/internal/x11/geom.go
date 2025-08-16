/*
	geometry helper for screencast/record
*/

package x11

import (
	"os/exec"
)

func GetGeometry() (string, error) {
	out, err := exec.Command("xdpyinfo").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

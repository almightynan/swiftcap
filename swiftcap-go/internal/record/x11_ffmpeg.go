package record

import "fmt"

func FFmpegCmd(display string, region string, fps int, out string, audio bool, aSrc string, bitrate int, qp int, container string, maxDur int, threads int, cursor bool) []string {
	args := []string{"-y"}
	wxh := ""
	offset := ""
	if region != "" {
		// region is expected as WxH+X+Y
		var w, h, x, y int
		n, err := fmt.Sscanf(region, "%dx%d+%d+%d", &w, &h, &x, &y)
		if n == 4 && err == nil {
			wxh = fmt.Sprintf("%dx%d", w, h)
			offset = fmt.Sprintf("+%d,%d", x, y)
		} else {
			wxh = region // fallback
		}
	}
	if wxh != "" {
		args = append(args, "-video_size", wxh)
	}
	if fps > 0 {
		args = append(args, "-framerate", fmt.Sprintf("%d", fps))
	}
	input := display
	if offset != "" {
		input = fmt.Sprintf("%s%s", display, offset)
	}
	args = append(args, "-f", "x11grab", "-i", input)
	if audio {
		args = append(args, "-f", "pulse", "-i", aSrc)
	}
	args = append(args, "-c:v", "libx264", "-preset", "ultrafast")
	if audio {
		args = append(args, "-c:a", "aac")
	}
	args = append(args, out)
	return args
}

package record

import "fmt"

func FFmpegCmd(display string, region string, fps int, out string, audio bool, aSrc string, bitrate int, qp int, container string, maxDur int, threads int, cursor bool) []string {
	args := []string{"-y"}

	// Parse region → video_size and offset.
	// Width and height must be even for yuv420p / H.264; round down if needed.
	wxh := ""
	offset := ""
	if region != "" {
		var w, h, x, y int
		n, err := fmt.Sscanf(region, "%dx%d+%d+%d", &w, &h, &x, &y)
		if n == 4 && err == nil {
			if w%2 != 0 {
				w--
			}
			if h%2 != 0 {
				h--
			}
			if w < 2 {
				w = 2
			}
			if h < 2 {
				h = 2
			}
			wxh = fmt.Sprintf("%dx%d", w, h)
			offset = fmt.Sprintf("+%d,%d", x, y)
		} else {
			wxh = region
		}
	}

	// ── x11grab input ──────────────────────────────────────────────────────────
	if wxh != "" {
		args = append(args, "-video_size", wxh)
	}
	if fps > 0 {
		args = append(args, "-framerate", fmt.Sprintf("%d", fps))
	}
	args = append(args, "-thread_queue_size", "512")

	input := display
	if offset != "" {
		input = fmt.Sprintf("%s%s", display, offset)
	}
	// draw_mouse must go after -f x11grab and before -i (device-specific input option)
	args = append(args, "-f", "x11grab")
	if cursor {
		args = append(args, "-draw_mouse", "1")
	} else {
		args = append(args, "-draw_mouse", "0")
	}
	args = append(args, "-i", input)

	// ── PulseAudio input ───────────────────────────────────────────────────────
	if audio {
		args = append(args, "-thread_queue_size", "512", "-f", "pulse", "-i", aSrc)
	}

	// ── Video encoding ─────────────────────────────────────────────────────────
	args = append(args, "-c:v", "libx264")
	args = append(args, "-preset", "veryfast")
	args = append(args, "-tune", "zerolatency")
	args = append(args, "-profile:v", "baseline")

	if threads > 0 {
		args = append(args, "-threads", fmt.Sprintf("%d", threads))
	} else {
		args = append(args, "-threads", "0")
	}

	if qp > 0 {
		args = append(args, "-qp", fmt.Sprintf("%d", qp))
	} else if bitrate > 0 {
		args = append(args, "-b:v", fmt.Sprintf("%dk", bitrate))
		args = append(args, "-maxrate", fmt.Sprintf("%dk", bitrate))
		args = append(args, "-bufsize", fmt.Sprintf("%dk", bitrate*2))
		args = append(args, "-rc-lookahead", "0")
	}

	args = append(args, "-pix_fmt", "yuv420p")

	// ── Audio encoding ─────────────────────────────────────────────────────────
	if audio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ar", "44100", "-ac", "2")
	}

	// ── Output container ───────────────────────────────────────────────────────
	switch container {
	case "mkv":
		args = append(args, "-f", "matroska")
	case "mov":
		args = append(args, "-f", "mov")
	case "avi":
		args = append(args, "-f", "avi")
	default:
		args = append(args, "-f", "mp4", "-movflags", "+faststart")
	}

	if maxDur > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", maxDur))
	}

	args = append(args, out)
	return args
}

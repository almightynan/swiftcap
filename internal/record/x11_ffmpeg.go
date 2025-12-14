package record

import "fmt"

func FFmpegCmd(display string, region string, fps int, out string, audio bool, aSrc string, bitrate int, qp int, container string, maxDur int, threads int, cursor bool) []string {
	args := []string{"-y"}
	
	// Input video settings
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
	
	// x11grab options for better performance
	args = append(args, "-thread_queue_size", "512") // Larger buffer for smoother capture
	input := display
	if offset != "" {
		input = fmt.Sprintf("%s%s", display, offset)
	}
	args = append(args, "-f", "x11grab", "-i", input)
	
	// Audio input
	if audio {
		args = append(args, "-thread_queue_size", "512", "-f", "pulse", "-i", aSrc)
	}
	
	// Video encoding - optimized for performance
	args = append(args, "-c:v", "libx264")
	
	// Use hardware acceleration if available (try vaapi, then nvenc, then software)
	// For now, use optimized software encoding
	args = append(args, "-preset", "veryfast") // Changed from ultrafast for better quality/performance balance
	args = append(args, "-tune", "zerolatency") // Zero latency for real-time recording
	args = append(args, "-profile:v", "baseline") // Baseline profile for compatibility and speed
	
	// Thread optimization
	if threads > 0 {
		args = append(args, "-threads", fmt.Sprintf("%d", threads))
	} else {
		// Auto-detect optimal threads (typically CPU cores)
		args = append(args, "-threads", "0") // 0 = auto
	}
	
	// Rate control - use QP if specified, otherwise bitrate
	if qp > 0 {
		args = append(args, "-qp", fmt.Sprintf("%d", qp))
	} else if bitrate > 0 {
		args = append(args, "-b:v", fmt.Sprintf("%dk", bitrate))
		args = append(args, "-maxrate", fmt.Sprintf("%dk", bitrate))
		args = append(args, "-bufsize", fmt.Sprintf("%dk", bitrate*2)) // 2x bitrate buffer
		args = append(args, "-rc-lookahead", "0") // Disable lookahead for lower latency
	}
	
	// Pixel format for better performance
	args = append(args, "-pix_fmt", "yuv420p")
	
	// Cursor option
	if !cursor {
		args = append(args, "-draw_mouse", "0")
	} else {
		args = append(args, "-draw_mouse", "1")
	}
	
	// Audio encoding
	if audio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ar", "44100")
		args = append(args, "-ac", "2") // Stereo
	}
	
	// Output format based on container
	if container == "mkv" {
		args = append(args, "-f", "matroska")
	} else {
		args = append(args, "-f", "mp4")
		// Fast start for MP4 (allows streaming)
		args = append(args, "-movflags", "+faststart")
	}
	
	// Max duration
	if maxDur > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", maxDur))
	}
	
	args = append(args, out)
	return args
}

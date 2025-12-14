package record

import "fmt"

func GStreamerCmd(videoNode, audioNode, out string, fps, bitrate int, container string) []string {
	args := []string{"-e", "pipewiresrc", "do-timestamp=true"}
	args = append(args, "!", "videoconvert", "!", fmt.Sprintf("video/x-raw,format=NV12,framerate=%d/1", fps))
	args = append(args, "!", "x264enc", "tune=zerolatency", "speed-preset=veryfast", fmt.Sprintf("bitrate=%d", bitrate), fmt.Sprintf("key-int-max=%d", 2*fps))
	args = append(args, "!", "h264parse")
	if container == "mp4" {
		args = append(args, "!", "mp4mux", "faststart=true")
	} else {
		args = append(args, "!", "matroskamux")
	}
	args = append(args, "!", "filesink", fmt.Sprintf("location=%s", out))
	return args
}

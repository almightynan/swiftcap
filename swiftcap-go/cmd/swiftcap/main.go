/*
	everything works from here, ive only written good comments in this file :P
*/

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"swiftcap-go/internal/cli"
	"swiftcap-go/internal/detect"
	"swiftcap-go/internal/portal"
	"swiftcap-go/internal/record"
	"swiftcap-go/internal/shoot"
	"syscall"
	"time"
)

// splitLines splits a string into lines (cross-platform)
func splitLines(s string) []string {
	var lines []string
	l := 0
	for i := range s {
		if s[i] == '\n' {
			lines = append(lines, s[l:i])
			l = i + 1
		}
	}
	if l < len(s) {
		lines = append(lines, s[l:])
	}
	return lines
}

func main() {
	args := os.Args[1:]
	cfg, err := cli.Parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	session, err := detect.Session()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(10)
	}

	switch cfg.Mode {
	case "record":
		recordMain(cfg, session)
	case "screenshot":
		screenshotMain(cfg, session)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", cfg.Mode)
		os.Exit(1)
	}
}

func screenshotMain(cfg cli.Config, session detect.SessionType) {
	region := cfg.Region
	if region == "" {
		region = "640x360+0+0"
	}
	out := cfg.Out
	format := cfg.Format
	var err error
	switch session {
	case detect.SessionX11:
		// remove offset for -video_size, pass offset in -i
		var w, h, x, y int
		wxh := ""
		offset := "+0,0"
		n, scanErr := fmt.Sscanf(region, "%dx%d+%d+%d", &w, &h, &x, &y)
		if n == 4 && scanErr == nil {
			wxh = fmt.Sprintf("%dx%d", w, h)
			offset = fmt.Sprintf("+%d,%d", x, y)
		} else {
			wxh = region
		}
		display := os.Getenv("DISPLAY")
		cmd := exec.Command("ffmpeg", "-y", "-f", "x11grab", "-video_size", wxh, "-i", display+offset, "-vframes", "1", out)
		err = cmd.Run()
	case detect.SessionWayland:
		err = portal.TakeScreenshot(region, out)
	case detect.SessionWindows, detect.SessionMac:
		// use cross-platform fallback
		err = shoot.ScreenshotCross(region, out, format)
	default:
		err = fmt.Errorf("no supported display/session for screenshot")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Screenshot failed: %v\n", err)
		os.Exit(30)
	}
	fmt.Println("Screenshot saved to", out)
}

func recordMain(cfg cli.Config, session detect.SessionType) {
	if session == detect.SessionWayland {
		err := portal.StartScreencast()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(21)
		}
		return
	}

	if session == detect.SessionX11 {
		// validate audio source
		audioSrc := cfg.ASrc
		if cfg.Audio == "on" && audioSrc == "" {
			audioSrc = "default"
		}
		// aggressive resource saving defaults
		fps := cfg.Fps
		if fps <= 0 {
			fps = 10
		}
		threads := cfg.Threads
		if threads <= 0 {
			threads = 1
		}
		region := cfg.Region
		if region == "" {
			// auto-detect full display size
			out, err := exec.Command("xdpyinfo").Output()
			wxh := ""
			if err == nil {
				var w, h int
				lines := string(out)
				for _, line := range splitLines(lines) {
					if n, _ := fmt.Sscanf(line, "dimensions: %dx%d", &w, &h); n == 2 {
						wxh = fmt.Sprintf("%dx%d+0+0", w, h)
						break
					}
				}
			}
			if wxh == "" {
				wxh = "1024x768+0+0" // fallback
			}
			region = wxh
		}

		bitrate := cfg.Bitrate
		if bitrate <= 0 {
			bitrate = 400
		}
		args := record.FFmpegCmd(
			os.Getenv("DISPLAY"),
			region,
			fps,
			cfg.Out,
			cfg.Audio == "on",
			audioSrc,
			bitrate,
			cfg.Qp,
			cfg.Container,
			cfg.MaxDur,
			threads,
			cfg.Cursor == "on",
		)
		// hide ffmpeg logs, show pretty progress
		fmt.Printf("\033[1;36mRecording...\033[0m (Ctrl+C to stop)\nSaving to: \033[1;32m%s\033[0m\n", cfg.Out)
		var cmd *exec.Cmd
		var ctx context.Context
		var cancel context.CancelFunc
		if cfg.MaxDur > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg.MaxDur+5)*time.Second)
			defer cancel()
			cmd = exec.CommandContext(ctx, "ffmpeg", args...)
		} else {
			cmd = exec.Command("ffmpeg", args...)
		}
		// trying to show user-friendly progress, but its a fucking mess so i need to rewrite this
		stderr, _ := cmd.StderrPipe()
		cmd.Stdout = nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if cfg.Nice != 0 {
			syscall.Setpriority(syscall.PRIO_PROCESS, 0, cfg.Nice)
		}

		// handle ctrl+c: forward SIGINT to ffmpeg process group
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		userStopped := false
		ctxTimer, cancelTimer := context.WithCancel(context.Background())
		defer cancelTimer()

		progressRe := regexp.MustCompile(`frame= *([0-9]+).*fps= *([0-9.]+).*time= *([0-9:.]+)`)
		spinner := []string{"|", "/", "-", "\\"}
		spinIdx := 0
		startTime := time.Now()
		lastFrame, lastFps, lastTime := "", "", ""
		progressCh := make(chan struct{}, 1)

		// timer goroutine for real-time updates
		go func() {
			for {
				select {
				case <-ctxTimer.Done():
					return
				case <-time.After(time.Second):
					progressCh <- struct{}{}
				}
			}
		}()

		err := cmd.Start()
		if err == nil {
			scanner := bufio.NewScanner(stderr)
			readDone := make(chan struct{})
			go func() {
				for scanner.Scan() {
					line := scanner.Text()
					m := progressRe.FindStringSubmatch(line)
					if m != nil {
						lastFrame, lastFps, lastTime = m[1], m[2], m[3]
					}
				}
				close(readDone)
			}()
		loop:
			for {
				select {
				case <-progressCh:
					t := time.Since(startTime).Truncate(time.Second)
					fmt.Printf("\r\033[1;36mRecording %s\033[0m | Elapsed: %s | Time: %s | Frame: %s | FPS: %s", spinner[spinIdx], t, lastTime, lastFrame, lastFps)
					spinIdx = (spinIdx + 1) % len(spinner)
				case <-sigCh:
					userStopped = true
					if cmd.Process != nil {
						pgid, _ := syscall.Getpgid(cmd.Process.Pid)
						syscall.Kill(-pgid, syscall.SIGINT)
					}
					break loop
				case <-readDone:
					break loop
				}
			}
			err = cmd.Wait()
		}
		cancelTimer()
		signal.Stop(sigCh)
		close(sigCh)
		if userStopped {
			fmt.Printf("\n\033[1;33mRecording stopped by user.\033[0m Saved to: %s\n", cfg.Out)
			return
		}
		if err != nil {
			if cfg.MaxDur > 0 && ctx != nil && ctx.Err() == context.DeadlineExceeded && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
				pgid, _ := syscall.Getpgid(cmd.Process.Pid)
				syscall.Kill(-pgid, syscall.SIGTERM)
				fmt.Fprintf(os.Stderr, "\033[1;31mE_RUNTIME=100:\033[0m FFmpeg timed out\n")
				os.Exit(100)
			}
			fmt.Fprintf(os.Stderr, "\033[1;31mE_FFMPEG_MISSING=20:\033[0m FFmpeg failed: %v\n", err)
			os.Exit(20)
		}
		fmt.Printf("\n\033[1;32mRecording complete!\033[0m Saved to: %s\n", cfg.Out)
		return
	}
}

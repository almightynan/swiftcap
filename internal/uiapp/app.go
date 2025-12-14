package uiapp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	countdownSeconds = 3
)

type exitIntent int

const (
	exitIntentNone exitIntent = iota
	exitIntentPause
	exitIntentStop
)

type RecordingUI struct {
	app        fyne.App
	mainWin    fyne.Window
	startBtn   *widget.Button
	stopBtn    *widget.Button
	statusText *widget.Label

	desktopApp   desktop.App
	toast        *toastHandle
	countdown    *countdownOverlay
	settingsWin  *settingsWindow
	config       *RecordingConfig
	cliPath      string
	videosDir    string
	activeRegion string

	mu             sync.Mutex
	recorderCmd    *exec.Cmd
	recorderCancel context.CancelFunc
	recorderDone   chan struct{}
	exitIntent     exitIntent
	isPaused       bool
	finalizing     bool

	segmentIndex   int
	segmentFiles   []string
	concatListPath string

	elapsedSeconds int
	elapsedTicker  *time.Ticker
	elapsedQuit    chan struct{}
	flashState     bool
	playIconTimer  int

	lastRecorderErr error
}

func Run() error {
	application := app.NewWithID("swiftcap-ui")
	application.Settings().SetTheme(theme.DarkTheme())
	ui := newRecordingUI(application)
	ui.buildMainWindow()
	ui.refreshUI()
	ui.updateTray()
	application.Run()
	return nil
}

func newRecordingUI(a fyne.App) *RecordingUI {
	ui := &RecordingUI{
		app:    a,
		config: NewRecordingConfig(),
	}
	if desk, ok := a.(desktop.App); ok {
		ui.desktopApp = desk
	}
	return ui
}

func (ui *RecordingUI) buildMainWindow() {
	win := ui.app.NewWindow("SwiftCap")
	win.SetIcon(baseAppIcon())
	win.Resize(fyne.NewSize(480, 280))
	win.SetFixedSize(true)

	title := widget.NewLabelWithStyle("SwiftCap", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	desc := widget.NewLabel("fast, minimal, cross-platform screen utility tool")
	desc.Wrapping = fyne.TextWrapWord

	ui.statusText = widget.NewLabel("Idle")
	ui.startBtn = widget.NewButton("Start Recording", func() {
		go ui.handleStart()
	})
	ui.stopBtn = widget.NewButton("Stop Recording", func() {
		go ui.handleStop()
	})
	ui.stopBtn.Disable()

	settingsBtn := widget.NewButton("Settings", func() {
		ui.showSettings()
	})

	buttonRow := container.NewHBox(ui.startBtn, layout.NewSpacer(), ui.stopBtn)
	settingsRow := container.NewHBox(layout.NewSpacer(), settingsBtn)
	content := container.NewVBox(
		title,
		desc,
		widget.NewSeparator(),
		buttonRow,
		ui.statusText,
		settingsRow,
	)
	win.SetContent(container.NewPadded(content))
	ui.mainWin = win
	win.Show()
}

func (ui *RecordingUI) handleStart() {
	ui.mu.Lock()
	if ui.recorderCmd != nil || ui.finalizing {
		ui.mu.Unlock()
		ui.showInfo("SwiftCap", "Recording already in progress.")
		return
	}
	if ui.isPaused {
		ui.mu.Unlock()
		ui.showInfo("SwiftCap", "Recording is paused. Use Resume instead.")
		return
	}
	if ui.countdown != nil {
		ui.mu.Unlock()
		return
	}
	ui.mu.Unlock()

	ui.runOnMain(func() {
		ui.countdown = newCountdownOverlay(ui.app, countdownSeconds, func() {
			ui.mu.Lock()
			ui.countdown = nil
			ui.mu.Unlock()
			go ui.startRecording()
		}, func() {
			ui.mu.Lock()
			ui.countdown = nil
			ui.mu.Unlock()
			ui.cancelPendingRecording()
		})
	})
}

func (ui *RecordingUI) handleStop() {
	ui.mu.Lock()
	if ui.recorderCmd == nil && !ui.isPaused {
		if len(ui.segmentFiles) == 0 {
			ui.mu.Unlock()
			ui.showInfo("SwiftCap", "No recording in progress.")
			return
		}
	}
	if ui.finalizing {
		ui.mu.Unlock()
		return
	}
	ui.finalizing = true
	done := ui.recorderDone
	cmd := ui.recorderCmd
	ui.exitIntent = exitIntentStop
	ui.mu.Unlock()

	if cmd != nil {
		sendInterrupt(cmd.Process)
		if done != nil {
			<-done
		}
	}

	ui.mu.Lock()
	ui.stopElapsedTickerLocked()
	files := append([]string(nil), ui.segmentFiles...)
	listPath := ui.concatListPath
	ui.segmentFiles = nil
	ui.concatListPath = ""
	ui.recorderCmd = nil
	ui.recorderCancel = nil
	ui.recorderDone = nil
	ui.isPaused = false
	ui.playIconTimer = 0
	ui.exitIntent = exitIntentNone
	ui.mu.Unlock()

	ui.setStatus("Finalizing recording...")
	finalPath, err := ui.concatSegments(files, listPath)
	ui.mu.Lock()
	ui.finalizing = false
	ui.mu.Unlock()

	if err != nil {
		ui.showError("SwiftCap", err.Error())
		ui.refreshUI()
		return
	}
	ui.showToast(finalPath)
	ui.refreshUI()
}

func (ui *RecordingUI) handlePause() {
	ui.mu.Lock()
	if ui.recorderCmd == nil || ui.isPaused {
		ui.mu.Unlock()
		return
	}
	ui.exitIntent = exitIntentPause
	done := ui.recorderDone
	cmd := ui.recorderCmd
	ui.mu.Unlock()

	sendInterrupt(cmd.Process)
	if done != nil {
		<-done
	}

	ui.mu.Lock()
	ui.isPaused = true
	ui.stopElapsedTickerLocked()
	ui.exitIntent = exitIntentNone
	ui.mu.Unlock()
	ui.setStatus("Recording paused")
	ui.refreshUI()
}

func (ui *RecordingUI) handleResume() {
	ui.mu.Lock()
	if !ui.isPaused {
		ui.mu.Unlock()
		return
	}
	nextIndex := ui.segmentIndex + 1
	ui.mu.Unlock()

	dir, err := ui.ensureVideosDir()
	if err != nil {
		ui.showError("SwiftCap", err.Error())
		return
	}

	segmentPath := filepath.Join(dir, fmt.Sprintf("swiftcap_segment_%d.mp4", nextIndex))
	absSeg, err := filepath.Abs(segmentPath)
	if err != nil {
		absSeg = segmentPath
	}

	ui.mu.Lock()
	if !ui.isPaused {
		ui.mu.Unlock()
		return
	}
	ui.segmentIndex = nextIndex
	ui.segmentFiles = append(ui.segmentFiles, absSeg)
	ui.mu.Unlock()

	if err := ui.launchRecorder(absSeg); err != nil {
		ui.showError("SwiftCap", err.Error())
		return
	}

	ui.mu.Lock()
	ui.isPaused = false
	ui.playIconTimer = 5
	ui.mu.Unlock()
	ui.setStatus("Recording...")
	ui.refreshUI()
}

func (ui *RecordingUI) cancelPendingRecording() {
	ui.setStatus("Idle")
	ui.refreshUI()
}

func (ui *RecordingUI) startRecording() {
	ui.setStatus("Starting recording...")
	segmentPath, err := ui.initialSegmentPath()
	if err != nil {
		ui.showError("SwiftCap", err.Error())
		ui.setStatus("Idle")
		return
	}
	if err := ui.launchRecorder(segmentPath); err != nil {
		ui.showError("SwiftCap", err.Error())
		ui.setStatus("Idle")
		return
	}
	ui.setStatus("Recording...")
	ui.refreshUI()
}

func (ui *RecordingUI) initialSegmentPath() (string, error) {
	dir, err := ui.ensureVideosDir()
	if err != nil {
		return "", err
	}
	now := time.Now().UnixNano()
	seg := filepath.Join(dir, fmt.Sprintf("swiftcap_%d_segment_1.mp4", now))
	absSeg, err := filepath.Abs(seg)
	if err != nil {
		absSeg = seg
	}
	ui.mu.Lock()
	ui.segmentIndex = 1
	ui.segmentFiles = []string{absSeg}
	ui.concatListPath = filepath.Join(dir, fmt.Sprintf("swiftcap_concat_%d.txt", now))
	ui.mu.Unlock()
	return absSeg, nil
}

func (ui *RecordingUI) launchRecorder(outPath string) error {
	cli, err := ui.resolveCLIBinary()
	if err != nil {
		return err
	}

	args := []string{"record", "--out", outPath}

	// Audio
	if ui.config.GetAudio() {
		args = append(args, "--audio", "on")
	} else {
		args = append(args, "--audio", "off")
	}

	// Cursor
	if ui.config.GetCursor() {
		args = append(args, "--cursor", "on")
	} else {
		args = append(args, "--cursor", "off")
	}

	// FPS
	if fps := ui.config.GetFPS(); fps > 0 {
		args = append(args, "--fps", fmt.Sprintf("%d", fps))
	}

	// Bitrate
	if bitrate := ui.config.GetBitrate(); bitrate > 0 {
		args = append(args, "--bitrate", fmt.Sprintf("%d", bitrate))
	}

	// Container
	if container := ui.config.GetContainer(); container != "" {
		args = append(args, "--container", container)
	}

	// Region
	region := ui.config.GetRegion()
	if region == "" {
		region = ui.detectRegion()
	}
	if region != "" {
		args = append(args, "--region", region)
		ui.mu.Lock()
		ui.activeRegion = region
		ui.mu.Unlock()
	}

	// Max Duration
	if maxDur := ui.config.GetMaxDur(); maxDur > 0 {
		args = append(args, "--max-dur", fmt.Sprintf("%d", maxDur))
	}

	// Threads
	if threads := ui.config.GetThreads(); threads > 0 {
		args = append(args, "--threads", fmt.Sprintf("%d", threads))
	}

	// QP
	if qp := ui.config.GetQP(); qp > 0 {
		args = append(args, "--qp", fmt.Sprintf("%d", qp))
	}

	// Nice
	if nice := ui.config.GetNice(); nice != 0 {
		args = append(args, "--nice", fmt.Sprintf("%d", nice))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cli, args...)

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start swiftcap CLI: %w", err)
	}

	ui.mu.Lock()
	ui.recorderCmd = cmd
	ui.recorderCancel = cancel
	ui.recorderDone = make(chan struct{})
	ui.exitIntent = exitIntentNone
	ui.elapsedSeconds = 0
	ui.startElapsedTickerLocked()
	ui.mu.Unlock()

	go ui.monitorRecorder(cmd)
	return nil
}

func (ui *RecordingUI) monitorRecorder(cmd *exec.Cmd) {
	err := cmd.Wait()
	ui.mu.Lock()
	done := ui.recorderDone
	intent := ui.exitIntent
	ui.exitIntent = exitIntentNone
	ui.recorderCmd = nil
	ui.recorderCancel = nil
	ui.recorderDone = nil
	ui.lastRecorderErr = err
	ui.mu.Unlock()
	if done != nil {
		close(done)
	}
	if intent == exitIntentNone {
		ui.mu.Lock()
		ui.stopElapsedTickerLocked()
		ui.isPaused = false
		ui.mu.Unlock()
		if err != nil {
			ui.showError("SwiftCap", fmt.Sprintf("Recording ended unexpectedly: %v", err))
		}
		ui.setStatus("Idle")
		ui.refreshUI()
	}
}

func (ui *RecordingUI) concatSegments(files []string, listPath string) (string, error) {
	if len(files) == 0 {
		return "", errors.New("no recorded segments to merge")
	}
	dir, err := ui.ensureVideosDir()
	if err != nil {
		return "", err
	}
	if listPath == "" {
		listPath = filepath.Join(dir, fmt.Sprintf("swiftcap_concat_%d.txt", time.Now().UnixNano()))
	}
	f, err := os.Create(listPath)
	if err != nil {
		return "", err
	}
	for _, seg := range files {
		if _, statErr := os.Stat(seg); statErr != nil {
			f.Close()
			return "", fmt.Errorf("missing segment: %s", seg)
		}
		if _, writeErr := fmt.Fprintf(f, "file '%s'\n", seg); writeErr != nil {
			f.Close()
			return "", writeErr
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	out := filepath.Join(dir, fmt.Sprintf("recording_%s.mp4", time.Now().Format("20060102_150405")))
	var ffmpegOut bytes.Buffer
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", out)
	cmd.Stdout = &ffmpegOut
	cmd.Stderr = &ffmpegOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg concat failed: %w\n%s", err, ffmpegOut.String())
	}
	for _, seg := range files {
		_ = os.Remove(seg)
	}
	_ = os.Remove(listPath)
	return out, nil
}

func (ui *RecordingUI) ensureVideosDir() (string, error) {
	ui.mu.Lock()
	if ui.videosDir != "" {
		dir := ui.videosDir
		ui.mu.Unlock()
		return dir, nil
	}
	ui.mu.Unlock()

	dir := os.Getenv("SWIFTCAP_VIDEOS_DIR")
	if dir == "" {
		if xdg := lookupXDGVideos(); xdg != "" {
			dir = xdg
		} else {
			home, _ := os.UserHomeDir()
			if home != "" {
				dir = filepath.Join(home, "Videos")
			} else {
				dir = "./videos"
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ui.mu.Lock()
	ui.videosDir = dir
	ui.mu.Unlock()
	return dir, nil
}

func lookupXDGVideos() string {
	cmd := exec.Command("xdg-user-dir", "VIDEOS")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return ""
	}
	return path
}

func (ui *RecordingUI) resolveCLIBinary() (string, error) {
	ui.mu.Lock()
	if ui.cliPath != "" {
		path := ui.cliPath
		ui.mu.Unlock()
		return path, nil
	}
	ui.mu.Unlock()

	if env := os.Getenv("SWIFTCAP_CLI_PATH"); env != "" {
		if fileExists(env) {
			ui.mu.Lock()
			ui.cliPath = env
			ui.mu.Unlock()
			return env, nil
		}
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "swiftcap")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if fileExists(candidate) {
			ui.mu.Lock()
			ui.cliPath = candidate
			ui.mu.Unlock()
			return candidate, nil
		}
	}

	if inPath, err := exec.LookPath("swiftcap"); err == nil {
		ui.mu.Lock()
		ui.cliPath = inPath
		ui.mu.Unlock()
		return inPath, nil
	}
	return "", errors.New("swiftcap CLI binary not found. Install it or set SWIFTCAP_CLI_PATH.")
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (ui *RecordingUI) detectRegion() string {
	out, err := exec.Command("xdpyinfo").Output()
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		var w, h int
		if _, scanErr := fmt.Sscanf(line, "dimensions: %dx%d pixels", &w, &h); scanErr == nil {
			return fmt.Sprintf("%dx%d+0+0", w, h)
		}
	}
	return ""
}

func (ui *RecordingUI) startElapsedTickerLocked() {
	if ui.elapsedTicker != nil {
		return
	}
	ui.elapsedTicker = time.NewTicker(time.Second)
	ui.elapsedQuit = make(chan struct{})
	go func() {
		for {
			select {
			case <-ui.elapsedTicker.C:
				ui.incrementElapsed()
			case <-ui.elapsedQuit:
				return
			}
		}
	}()
}

func (ui *RecordingUI) stopElapsedTickerLocked() {
	if ui.elapsedTicker == nil {
		return
	}
	ui.elapsedTicker.Stop()
	close(ui.elapsedQuit)
	ui.elapsedTicker = nil
	ui.elapsedQuit = nil
}

func (ui *RecordingUI) incrementElapsed() {
	ui.mu.Lock()
	if ui.recorderCmd != nil && !ui.isPaused {
		ui.elapsedSeconds++
		if ui.playIconTimer > 0 {
			ui.playIconTimer--
		} else {
			ui.flashState = !ui.flashState
		}
	}
	elapsed := ui.elapsedSeconds
	recording := ui.recorderCmd != nil
	paused := ui.isPaused
	flash := ui.flashState
	playTimer := ui.playIconTimer > 0
	ui.mu.Unlock()

	ui.runOnMain(func() {
		ui.updateStatus(elapsed, recording, paused)
	})
	ui.updateTrayIcon(recording, paused, flash, playTimer, elapsed)
}

func (ui *RecordingUI) updateStatus(elapsed int, recording, paused bool) {
	if ui.statusText == nil {
		return
	}
	switch {
	case recording:
		ui.statusText.SetText(fmt.Sprintf("Recording... %s", formatElapsed(elapsed)))
	case paused:
		ui.statusText.SetText(fmt.Sprintf("Paused %s", formatElapsed(elapsed)))
	default:
		ui.statusText.SetText("Idle")
	}
}

func (ui *RecordingUI) setStatus(text string) {
	ui.runOnMain(func() {
		if ui.statusText != nil {
			ui.statusText.SetText(text)
		}
	})
}

func formatElapsed(seconds int) string {
	mins := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func (ui *RecordingUI) refreshUI() {
	ui.mu.Lock()
	recording := ui.recorderCmd != nil
	paused := ui.isPaused
	finalizing := ui.finalizing
	ui.mu.Unlock()

	ui.runOnMain(func() {
		if ui.startBtn != nil {
			ui.startBtn.Enable()
			if recording || paused || finalizing {
				ui.startBtn.Disable()
			}
		}
		if ui.stopBtn != nil {
			if recording || paused || finalizing {
				ui.stopBtn.Enable()
			} else {
				ui.stopBtn.Disable()
			}
		}
	})
	ui.updateTray()
}

func (ui *RecordingUI) updateTray() {
	ui.mu.Lock()
	recording := ui.recorderCmd != nil
	paused := ui.isPaused
	elapsed := ui.elapsedSeconds
	flash := ui.flashState
	playIcon := ui.playIconTimer > 0
	finalizing := ui.finalizing
	ui.mu.Unlock()

	ui.updateTrayIcon(recording, paused, flash, playIcon, elapsed)
	ui.updateTrayMenu(recording, paused, elapsed, finalizing)
}

func (ui *RecordingUI) updateTrayIcon(recording, paused, flash, showPlay bool, elapsed int) {
	if ui.desktopApp == nil {
		return
	}
	icon := trayIcon(recording, paused, flash, showPlay)
	ui.desktopApp.SetSystemTrayIcon(icon)
	if recording {
		ui.desktopApp.SetSystemTrayMenu(ui.buildTrayMenu(recording, paused, elapsed, ui.finalizing))
	}
}

func (ui *RecordingUI) updateTrayMenu(recording, paused bool, elapsed int, finalizing bool) {
	if ui.desktopApp == nil {
		return
	}
	ui.desktopApp.SetSystemTrayMenu(ui.buildTrayMenu(recording, paused, elapsed, finalizing))
}

func (ui *RecordingUI) buildTrayMenu(recording, paused bool, elapsed int, finalizing bool) *fyne.Menu {
	elapsedItem := fyne.NewMenuItem(fmt.Sprintf("Elapsed: %s", formatElapsed(elapsed)), nil)
	elapsedItem.Disabled = true

	startItem := fyne.NewMenuItem("Start Recording", func() { go ui.handleStart() })
	stopItem := fyne.NewMenuItem("Stop Recording", func() { go ui.handleStop() })
	pauseItem := fyne.NewMenuItem("Pause Recording", func() { go ui.handlePause() })
	resumeItem := fyne.NewMenuItem("Resume Recording", func() { go ui.handleResume() })
	quitItem := fyne.NewMenuItem("Quit", func() {
		ui.runOnMain(func() {
			ui.app.Quit()
		})
	})

	startItem.Disabled = recording || paused || finalizing
	stopItem.Disabled = !recording && !paused
	pauseItem.Disabled = !recording
	resumeItem.Disabled = !paused

	return fyne.NewMenu("SwiftCap",
		elapsedItem,
		fyne.NewMenuItemSeparator(),
		startItem,
		stopItem,
		pauseItem,
		resumeItem,
		fyne.NewMenuItemSeparator(),
		quitItem,
	)
}

func (ui *RecordingUI) showInfo(title, msg string) {
	ui.runOnMain(func() {
		if ui.mainWin == nil {
			return
		}
		dialog.ShowInformation(title, msg, ui.mainWin)
	})
}

func (ui *RecordingUI) showError(title, msg string) {
	ui.runOnMain(func() {
		if ui.mainWin == nil {
			return
		}
		dialog.ShowError(errors.New(msg), ui.mainWin)
	})
}

func (ui *RecordingUI) runOnMain(fn func()) {
	if fn == nil {
		return
	}
	fn()
}

func sendInterrupt(proc *os.Process) {
	if proc == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = proc.Signal(os.Kill)
		return
	}
	_ = proc.Signal(syscall.SIGINT)
}

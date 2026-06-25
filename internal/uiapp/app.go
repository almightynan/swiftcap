package uiapp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
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
	statusDot  *canvas.Circle
	regionLabel *widget.Label
	configSummary *widget.Label
	audioToggle   *widget.Check
	cursorToggle  *widget.Check
	containerSelect *widget.Select

	desktopApp    desktop.App
	toast         *toastHandle
	countdown     *countdownOverlay
	settingsWin   *settingsWindow
	recordingsList *recordingsList
	config        *RecordingConfig
	cliPath       string
	videosDir     string
	activeRegion  string

	mu             sync.Mutex
	recorderCmd    *exec.Cmd
	recorderCancel context.CancelFunc
	recorderDone   chan struct{}
	exitIntent     exitIntent
	isPaused       bool
	finalizing     bool
	windowVisible  bool

	segmentIndex   int
	segmentFiles   []string
	concatListPath string

	elapsedSeconds int
	elapsedTicker  *time.Ticker
	elapsedQuit    chan struct{}
	flashState     bool
	playIconTimer  int

	lastRecorderErr    error
	recorderStderr     *bytes.Buffer
}

var (
	statusReadyColor     = color.NRGBA{0x27, 0xb3, 0x72, 0xff}
	statusRecordingColor = color.NRGBA{0xee, 0x4f, 0x3f, 0xff}
	statusPausedColor    = color.NRGBA{0xf1, 0xc0, 0x5a, 0xff}
)

func Run() error {
	application := app.NewWithID("swiftcap-ui")
	application.Settings().SetTheme(theme.LightTheme())
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
	win.Resize(fyne.NewSize(1080, 760))
	win.SetFixedSize(false)
	win.CenterOnScreen()
	
	// Handle window close - hide instead of quit
	win.SetCloseIntercept(func() {
		ui.mu.Lock()
		ui.windowVisible = false
		ui.mu.Unlock()
		win.Hide()
		ui.updateTray()
	})

	title := canvas.NewText("SwiftCap", color.NRGBA{0x18, 0x1b, 0x20, 0xff})
	title.Alignment = fyne.TextAlignLeading
	title.TextSize = 32
	title.TextStyle = fyne.TextStyle{Bold: true}
	desc := widget.NewLabel("Bento-styled screen capture for fast, focused recordings")
	desc.Wrapping = fyne.TextWrapWord
	desc.Importance = widget.LowImportance

	// Control buttons
	ui.startBtn = widget.NewButton("Start Recording", func() {
		go ui.handleStart()
	})
	ui.startBtn.Importance = widget.HighImportance
	
	ui.stopBtn = widget.NewButton("Stop", func() {
		go ui.handleStop()
	})
	ui.stopBtn.Disable()

	openFolderBtn := widget.NewButton("Open Videos", func() {
		dir, err := ui.ensureVideosDir()
		if err != nil {
			ui.showError("Videos", err.Error())
			return
		}
		if err := openFolder(dir); err != nil {
			ui.showError("Open Folder", err.Error())
		}
	})
	settingsBtn := widget.NewButton("Settings", func() {
		ui.showSettings()
	})
	statusBadge := ui.buildStatusBadge()
	recordControls := container.NewVBox(
		statusBadge,
		container.NewHBox(ui.startBtn, ui.stopBtn),
		openFolderBtn,
	)
	recordCard := bentoCard("Recording", "Start, stop, and manage outputs", accentMint, recordControls)

	ui.regionLabel = widget.NewLabel("")
	ui.regionLabel.TextStyle = fyne.TextStyle{Monospace: true}
	ui.regionLabel.Wrapping = fyne.TextWrapWord
	selectRegionBtn := widget.NewButton("Select Area", func() {
		ui.selectRegion()
	})
	clearRegionBtn := widget.NewButton("Full Screen", func() {
		ui.clearRegion()
	})
	regionHint := widget.NewLabel("Select Area uses slop for true overlay selection when available.")
	regionHint.Wrapping = fyne.TextWrapWord
	regionHint.Importance = widget.LowImportance
	captureContent := container.NewVBox(
		ui.regionLabel,
		container.NewHBox(selectRegionBtn, clearRegionBtn),
		regionHint,
	)
	captureCard := bentoCard("Capture Area", "Choose the portion of the screen", accentSky, captureContent)

	ui.audioToggle = widget.NewCheck("Record audio", func(checked bool) {
		ui.config.SetAudio(checked)
	})
	ui.cursorToggle = widget.NewCheck("Show cursor", func(checked bool) {
		ui.config.SetCursor(checked)
	})
	formatLabel := widget.NewLabel("Format")
	ui.containerSelect = widget.NewSelect([]string{"mp4", "mkv"}, func(selected string) {
		ui.config.SetContainer(selected)
		ui.refreshConfigSummary()
	})
	ui.containerSelect.SetSelected(ui.config.GetContainer())
	ui.configSummary = widget.NewLabel("")
	ui.configSummary.Wrapping = fyne.TextWrapWord
	ui.configSummary.Importance = widget.LowImportance
	quickContent := container.NewVBox(
		ui.audioToggle,
		ui.cursorToggle,
		container.NewBorder(nil, nil, formatLabel, nil, ui.containerSelect),
		ui.configSummary,
		settingsBtn,
	)
	quickCard := bentoCard("Quick Controls", "Audio, cursor, and format", accentCoral, quickContent)

	// Recordings list
	videosDir, _ := ui.ensureVideosDir()
	ui.recordingsList = newRecordingsList(videosDir, func(path string) {
		if err := openFileFromToast(path); err != nil {
			ui.showError("Open File", err.Error())
		}
	})
	recordingsCard := bentoCard("Recent Recordings", "Latest captures and exports", accentSun, ui.recordingsList.getContainer())

	header := container.NewVBox(title, desc)
	topRow := container.NewGridWithColumns(3, recordCard, captureCard, quickCard)
	content := container.NewVBox(
		header,
		topRow,
		recordingsCard,
	)

	background := newAmbientBackground()
	fadeOverlay := canvas.NewRectangle(color.NRGBA{0xff, 0xff, 0xff, 0xff})
	win.SetContent(container.NewStack(background, container.NewPadded(content), fadeOverlay))
	ui.mainWin = win
	ui.windowVisible = true
	ui.syncQuickControls()
	ui.updateRegionLabel()
	ui.refreshConfigSummary()
	win.Show()
	ui.fadeOverlay(fadeOverlay)
}

func (ui *RecordingUI) buildStatusBadge() fyne.CanvasObject {
	ui.statusText = widget.NewLabel("Ready")
	ui.statusText.TextStyle = fyne.TextStyle{Bold: true}
	ui.statusDot = canvas.NewCircle(statusReadyColor)
	ui.statusDot.StrokeWidth = 0

	dotWrap := container.NewGridWrap(fyne.NewSize(10, 10), ui.statusDot)
	row := container.NewHBox(dotWrap, ui.statusText)

	bg := canvas.NewRectangle(blendColor(bentoSurface, accentMint, 0.18))
	bg.StrokeColor = bentoBorder
	bg.StrokeWidth = 1

	return container.NewPadded(container.NewStack(bg, container.NewPadded(row)))
}

func (ui *RecordingUI) fadeOverlay(overlay *canvas.Rectangle) {
	if overlay == nil {
		return
	}
	go func() {
		steps := 12
		for i := 0; i <= steps; i++ {
			alpha := uint8(255 - (i * 255 / steps))
			ui.runOnMain(func() {
				overlay.FillColor = color.NRGBA{0xff, 0xff, 0xff, alpha}
				overlay.Refresh()
				if alpha == 0 {
					overlay.Hide()
				}
			})
			time.Sleep(40 * time.Millisecond)
		}
	}()
}

func (ui *RecordingUI) updateStatusIndicator(recording, paused, flash bool) {
	if ui.statusDot == nil {
		return
	}
	next := statusReadyColor
	switch {
	case recording:
		if flash {
			next = statusRecordingColor
		} else {
			next = blendColor(statusRecordingColor, color.NRGBA{0xff, 0xff, 0xff, 0xff}, 0.35)
		}
	case paused:
		next = statusPausedColor
	}
	ui.runOnMain(func() {
		ui.statusDot.FillColor = next
		ui.statusDot.Refresh()
	})
}

func (ui *RecordingUI) updateRegionLabel() {
	if ui.regionLabel == nil {
		return
	}
	region := ui.config.GetRegion()
	label := "Full Screen"
	if region != "" {
		label = region
	}
	ui.runOnMain(func() {
		ui.regionLabel.SetText(label)
	})
}

func (ui *RecordingUI) selectRegion() {
	if ui.mainWin == nil {
		return
	}
	selector := newRegionSelector(ui.app, ui.mainWin, func(region string) {
		ui.config.SetRegion(region)
		ui.updateRegionLabel()
	}, nil)
	selector.Show()
}

func (ui *RecordingUI) clearRegion() {
	ui.config.SetRegion("")
	ui.updateRegionLabel()
}

func (ui *RecordingUI) syncQuickControls() {
	ui.runOnMain(func() {
		if ui.audioToggle != nil {
			ui.audioToggle.SetChecked(ui.config.GetAudio())
		}
		if ui.cursorToggle != nil {
			ui.cursorToggle.SetChecked(ui.config.GetCursor())
		}
		if ui.containerSelect != nil {
			containerValue := ui.config.GetContainer()
			if containerValue == "" {
				containerValue = "mp4"
			}
			ui.containerSelect.SetSelected(containerValue)
		}
	})
}

func (ui *RecordingUI) refreshConfigSummary() {
	if ui.configSummary == nil {
		return
	}
	containerValue := ui.config.GetContainer()
	if containerValue == "" {
		containerValue = "auto"
	}
	fps := ui.config.GetFPS()
	bitrate := ui.config.GetBitrate()
	fpsLabel := "auto fps"
	if fps > 0 {
		fpsLabel = fmt.Sprintf("%d fps", fps)
	}
	bitrateLabel := "auto bitrate"
	if bitrate > 0 {
		bitrateLabel = fmt.Sprintf("%d kbps", bitrate)
	}
	summary := fmt.Sprintf("Format %s | %s | %s", strings.ToUpper(containerValue), fpsLabel, bitrateLabel)
	ui.runOnMain(func() {
		ui.configSummary.SetText(summary)
	})
}

func (ui *RecordingUI) refreshRecordingsList() {
	ui.mu.Lock()
	videosDir := ui.videosDir
	ui.mu.Unlock()
	
	if videosDir == "" {
		videosDir, _ = ui.ensureVideosDir()
	}
	
	ui.runOnMain(func() {
		if ui.recordingsList != nil {
			ui.recordingsList.refresh(videosDir)
		}
	})
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

	// Hide main window when starting recording
	ui.runOnMain(func() {
		if ui.mainWin != nil {
			ui.mu.Lock()
			ui.windowVisible = false
			ui.mu.Unlock()
			ui.mainWin.Hide()
			ui.updateTray()
		}
	})

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
			// Show window again if cancelled
			ui.runOnMain(func() {
				if ui.mainWin != nil {
					ui.mu.Lock()
					ui.windowVisible = true
					ui.mu.Unlock()
					ui.mainWin.Show()
					ui.mainWin.RequestFocus()
					ui.updateTray()
				}
			})
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
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				// Timeout - force kill
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
			}
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

	if len(files) == 0 {
		ui.mu.Lock()
		ui.finalizing = false
		ui.mu.Unlock()
		ui.setStatus("Ready")
		ui.refreshUI()
		return
	}

	ui.setStatus("Finalizing recording...")
	ui.refreshUI()
	
	finalPath, err := ui.concatSegments(files, listPath)
	ui.mu.Lock()
	ui.finalizing = false
	ui.mu.Unlock()

	if err != nil {
		ui.showError("SwiftCap", fmt.Sprintf("Failed to finalize recording: %v", err))
		ui.setStatus("Ready")
		ui.refreshUI()
		return
	}
	
	ui.setStatus("Recording saved")
	
	// Refresh recordings list
	ui.refreshRecordingsList()
	
	// Show window after recording finishes
	ui.runOnMain(func() {
		if ui.mainWin != nil {
			ui.mu.Lock()
			ui.windowVisible = true
			ui.mu.Unlock()
			ui.mainWin.Show()
			ui.mainWin.RequestFocus()
			ui.updateTray()
		}
	})
	
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
		ui.showError("SwiftCap", fmt.Sprintf("Failed to access videos directory: %v", err))
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

	ui.setStatus("Resuming recording...")
	ui.refreshUI()
	
	if err := ui.launchRecorder(absSeg); err != nil {
		ui.mu.Lock()
		ui.isPaused = true
		ui.mu.Unlock()
		ui.showError("SwiftCap", fmt.Sprintf("Failed to resume recording: %v", err))
		ui.setStatus("Paused")
		ui.refreshUI()
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
	ui.setStatus("Ready")
	ui.refreshUI()
}


func (ui *RecordingUI) startRecording() {
	ui.setStatus("Starting recording...")
	ui.refreshUI()
	
	segmentPath, err := ui.initialSegmentPath()
	if err != nil {
		ui.showError("SwiftCap", fmt.Sprintf("Failed to prepare recording: %v", err))
		ui.setStatus("Ready")
		ui.refreshUI()
		return
	}
	
	if err := ui.launchRecorder(segmentPath); err != nil {
		ui.showError("SwiftCap", fmt.Sprintf("Failed to start recording: %v", err))
		ui.mu.Lock()
		ui.recorderCmd = nil
		ui.recorderCancel = nil
		ui.recorderDone = nil
		ui.mu.Unlock()
		ui.setStatus("Ready")
		ui.refreshUI()
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

	// Ensure we have an absolute path
	absCli, err := filepath.Abs(cli)
	if err != nil {
		absCli = cli
	}
	
	// Verify the file exists and is executable before trying to run it
	if _, err := os.Stat(absCli); err != nil {
		return fmt.Errorf("swiftcap CLI binary not found at: %s", absCli)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, absCli, args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start swiftcap CLI: %w", err)
	}

	ui.mu.Lock()
	ui.recorderCmd = cmd
	ui.recorderCancel = cancel
	ui.recorderDone = make(chan struct{})
	ui.recorderStderr = &stderrBuf
	ui.exitIntent = exitIntentNone
	ui.elapsedSeconds = 0
	ui.startElapsedTickerLocked()
	ui.mu.Unlock()

	// Update tray immediately so it shows recording state without waiting for first tick
	ui.updateTray()

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
	cliStderr := ""
	if ui.recorderStderr != nil {
		cliStderr = stripANSI(strings.TrimSpace(ui.recorderStderr.String()))
		ui.recorderStderr = nil
	}
	ui.mu.Unlock()
	if done != nil {
		close(done)
	}
	if intent == exitIntentNone {
		ui.mu.Lock()
		ui.stopElapsedTickerLocked()
		ui.isPaused = false
		ui.mu.Unlock()

		// Always show the main window so the user sees any error dialog
		ui.runOnMain(func() {
			if ui.mainWin != nil {
				ui.mu.Lock()
				ui.windowVisible = true
				ui.mu.Unlock()
				ui.mainWin.Show()
				ui.mainWin.RequestFocus()
				ui.updateTray()
			}
		})

		if err != nil {
			if err.Error() != "signal: interrupt" && err.Error() != "exit status 1" {
				msg := fmt.Sprintf("Recording ended unexpectedly: %v", err)
				if cliStderr != "" {
					// Surface the first meaningful line from the CLI/ffmpeg output
					for _, line := range strings.Split(cliStderr, "\n") {
						line = strings.TrimSpace(line)
						if line != "" && !strings.HasPrefix(line, "frame=") {
							msg += "\n\n" + line
							break
						}
					}
				}
				ui.showError("SwiftCap", msg)
			}
		}
		ui.setStatus("Ready")
		ui.refreshUI()
	}
}

func (ui *RecordingUI) concatSegments(files []string, listPath string) (string, error) {
	if len(files) == 0 {
		return "", errors.New("no recorded segments to merge")
	}
	
	// Verify all segments exist before proceeding
	missing := []string{}
	for _, seg := range files {
		if _, err := os.Stat(seg); err != nil {
			missing = append(missing, seg)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("missing segment files: %v", missing)
	}
	
	dir, err := ui.ensureVideosDir()
	if err != nil {
		return "", fmt.Errorf("failed to access videos directory: %w", err)
	}
	
	if listPath == "" {
		listPath = filepath.Join(dir, fmt.Sprintf("swiftcap_concat_%d.txt", time.Now().UnixNano()))
	}
	
	f, err := os.Create(listPath)
	if err != nil {
		return "", fmt.Errorf("failed to create concat list: %w", err)
	}
	defer f.Close()
	
	for _, seg := range files {
		absPath, err := filepath.Abs(seg)
		if err != nil {
			absPath = seg
		}
		if _, writeErr := fmt.Fprintf(f, "file '%s'\n", absPath); writeErr != nil {
			return "", fmt.Errorf("failed to write concat list: %w", writeErr)
		}
	}
	
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close concat list: %w", err)
	}

	out := filepath.Join(dir, fmt.Sprintf("recording_%s.mp4", time.Now().Format("20060102_150405")))
	var ffmpegOut bytes.Buffer
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", out)
	cmd.Stdout = &ffmpegOut
	cmd.Stderr = &ffmpegOut
	
	if err := cmd.Run(); err != nil {
		// Clean up list file on error
		os.Remove(listPath)
		return "", fmt.Errorf("ffmpeg failed to merge segments: %w\nOutput: %s", err, ffmpegOut.String())
	}
	
	// Verify output file was created
	if _, err := os.Stat(out); err != nil {
		os.Remove(listPath)
		return "", fmt.Errorf("output file was not created: %w", err)
	}
	
	// Clean up segment files and list file
	for _, seg := range files {
		os.Remove(seg)
	}
	os.Remove(listPath)
	
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
		if fileExists(path) && isExecutable(path) {
			absPath, _ := filepath.Abs(path)
			return absPath, nil
		}
		// Cached path no longer exists, clear it
		ui.mu.Lock()
		ui.cliPath = ""
		ui.mu.Unlock()
	}
	ui.mu.Unlock()

	// Helper to check and return absolute path
	checkAndReturn := func(candidate string) (string, bool) {
		if runtime.GOOS == "windows" && !strings.HasSuffix(candidate, ".exe") {
			candidate += ".exe"
		}
		if !fileExists(candidate) {
			return "", false
		}
		if !isExecutable(candidate) {
			return "", false
		}
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			return candidate, true
		}
		return absPath, true
	}

	// 1. Check environment variable
	if env := os.Getenv("SWIFTCAP_CLI_PATH"); env != "" {
		if path, ok := checkAndReturn(env); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
	}

	// 2. Check same directory as UI executable
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if path, ok := checkAndReturn(filepath.Join(exeDir, "swiftcap")); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
	}

	// 3. Check current working directory
	if wd, err := os.Getwd(); err == nil {
		if path, ok := checkAndReturn(filepath.Join(wd, "swiftcap")); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
	}

	// 4. Check cmd/swiftcap directory (common build location)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		// Try cmd/swiftcap/swiftcap relative to executable
		candidate := filepath.Join(exeDir, "cmd", "swiftcap", "swiftcap")
		if path, ok := checkAndReturn(candidate); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
		// Try parent/cmd/swiftcap/swiftcap
		parent := filepath.Dir(exeDir)
		candidate = filepath.Join(parent, "cmd", "swiftcap", "swiftcap")
		if path, ok := checkAndReturn(candidate); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
		// Try parent/swiftcap
		candidate = filepath.Join(parent, "swiftcap")
		if path, ok := checkAndReturn(candidate); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
	}

	// 5. Check $PATH
	if inPath, err := exec.LookPath("swiftcap"); err == nil {
		if path, ok := checkAndReturn(inPath); ok {
			ui.mu.Lock()
			ui.cliPath = path
			ui.mu.Unlock()
			return path, nil
		}
	}

	// Build helpful error message
	var suggestions []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		suggestions = append(suggestions, fmt.Sprintf("  - Place 'swiftcap' in: %s", dir))
	}
	if wd, err := os.Getwd(); err == nil {
		suggestions = append(suggestions, fmt.Sprintf("  - Place 'swiftcap' in: %s", wd))
	}
	suggestions = append(suggestions, "  - Set SWIFTCAP_CLI_PATH environment variable")
	suggestions = append(suggestions, "  - Build CLI: go build ./cmd/swiftcap")
	
	msg := "swiftcap CLI binary not found.\n\nTry:\n" + strings.Join(suggestions, "\n")
	return "", errors.New(msg)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	// Check if it's a regular file and has execute permissions
	mode := info.Mode()
	return mode.IsRegular() && (mode&0111 != 0)
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
		ui.updateStatus(elapsed, recording, paused, flash)
	})
	ui.updateTrayIcon(recording, paused, flash, playTimer, elapsed)
}

func (ui *RecordingUI) updateStatus(elapsed int, recording, paused, flash bool) {
	if ui.statusText == nil {
		return
	}
	switch {
	case recording:
		ui.statusText.SetText(fmt.Sprintf("Recording %s", formatElapsed(elapsed)))
	case paused:
		ui.statusText.SetText(fmt.Sprintf("Paused %s", formatElapsed(elapsed)))
	default:
		ui.statusText.SetText("Ready")
	}
	ui.updateStatusIndicator(recording, paused, flash)
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
	flash := ui.flashState
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
	ui.updateStatusIndicator(recording, paused, flash)
	ui.syncQuickControls()
	ui.updateRegionLabel()
	ui.refreshConfigSummary()
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
	
	ui.mu.Lock()
	visible := ui.windowVisible
	ui.mu.Unlock()
	
	showHideItem := fyne.NewMenuItem("Show Window", func() {
		ui.runOnMain(func() {
			if ui.mainWin != nil {
				ui.mu.Lock()
				if ui.windowVisible {
					ui.mainWin.Hide()
					ui.windowVisible = false
				} else {
					ui.mainWin.Show()
					ui.mainWin.RequestFocus()
					ui.windowVisible = true
				}
				ui.mu.Unlock()
				ui.updateTray()
			}
		})
	})
	
	quitItem := fyne.NewMenuItem("Quit", func() {
		ui.runOnMain(func() {
			ui.app.Quit()
		})
	})

	startItem.Disabled = recording || paused || finalizing
	stopItem.Disabled = !recording && !paused
	pauseItem.Disabled = !recording
	resumeItem.Disabled = !paused
	
	if visible {
		showHideItem.Label = "Hide Window"
	} else {
		showHideItem.Label = "Show Window"
	}

	return fyne.NewMenu("SwiftCap",
		elapsedItem,
		fyne.NewMenuItemSeparator(),
		startItem,
		stopItem,
		pauseItem,
		resumeItem,
		fyne.NewMenuItemSeparator(),
		showHideItem,
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

// stripANSI removes ANSI terminal escape sequences from s.
func stripANSI(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until we hit the final byte of the escape sequence (a letter)
			i += 2
			for i < len(s) && (s[i] < 'A' || s[i] > 'Z') && (s[i] < 'a' || s[i] > 'z') {
				i++
			}
			i++ // skip the final letter
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
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

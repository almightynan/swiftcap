package uiapp

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// videoPlayer is a lightweight in-app video player. Fyne has no video widget,
// so it decodes frames with ffmpeg piped as raw RGBA and renders them onto a
// canvas.Image paced at the source frame rate; audio is played by a parallel
// ffplay process started at the same timestamp. Sync is wall-clock based —
// good for previewing, not frame-perfect.
type videoPlayer struct {
	ui   *RecordingUI
	path string

	dispW, dispH int
	fps          float64
	duration     float64 // seconds

	cv       fyne.Canvas
	img      *canvas.Image
	playBtn  *hoverButton
	back10   *hoverButton
	fwd10    *hoverButton
	seek     *widget.Slider
	timeLbl  *widget.Label
	volBtn   *hoverButton

	// Seek indicator overlay (the "«  10s" flash on the video).
	seekIndPanel *fyne.Container
	seekIndText  *canvas.Text
	seekIndBg    *canvas.Rectangle
	seekIndAnim  *fyne.Animation

	// Arrow-key seek state (guarded by mu; the debounce timer fires off-thread).
	lastArrowTime time.Time
	lastArrowDir  int
	arrowStep     float64
	pendingTarget float64
	burstStart    float64
	seekTimer     *time.Timer

	mu        sync.Mutex
	playing   bool
	stopped   bool
	pos       float64   // playback position (base; add elapsed when playing)
	playStart time.Time // wall-clock time playback (re)started
	vol       int       // 0–100
	gen       int       // bumps on every (re)start to retire old frame goroutines
	vidCmd    *exec.Cmd
	audCmd    *exec.Cmd

	suppressSeek bool // guards programmatic seek-slider updates (SetValue fires OnChangeEnded)
	userSeeking  bool // true while the user drags the seek slider
}

func newVideoPlayer(ui *RecordingUI, path string) *videoPlayer {
	w, h, fps, dur := probeVideo(path)
	dw, dh := fitVideoSize(w, h, 620, 348)

	p := &videoPlayer{
		ui:       ui,
		cv:       ui.mainWin.Canvas(),
		path:     path,
		dispW:    dw,
		dispH:    dh,
		fps:      fps,
		duration: dur,
		vol:      100,
	}

	p.img = canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, dw, dh)))
	p.img.FillMode = canvas.ImageFillContain
	p.img.SetMinSize(fyne.NewSize(float32(dw), float32(dh)))

	// Load the first frame as a paused still (async, so opening the modal never
	// blocks the UI thread on ffmpeg).
	go func() {
		im := previewImageFor(path)
		if im == nil {
			return
		}
		p.ui.runOnMain(func() {
			p.mu.Lock()
			idle := !p.playing
			p.mu.Unlock()
			if idle {
				p.img.Image = im
				p.img.Refresh()
			}
		})
	}()

	p.playBtn = newButtonWithIcon("", theme.MediaPlayIcon(), p.togglePlay)
	p.playBtn.Importance = widget.LowImportance
	p.back10 = newButtonWithIcon("", theme.MediaFastRewindIcon(), func() { p.seekBy(-10) })
	p.back10.Importance = widget.LowImportance
	p.fwd10 = newButtonWithIcon("", theme.MediaFastForwardIcon(), func() { p.seekBy(10) })
	p.fwd10.Importance = widget.LowImportance

	// Seek indicator overlay (hidden until a seek flashes it).
	p.seekIndBg = canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0x00})
	p.seekIndBg.CornerRadius = 10
	p.seekIndText = canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0x00})
	p.seekIndText.TextSize = 22
	p.seekIndText.TextStyle = fyne.TextStyle{Bold: true}
	p.seekIndText.Alignment = fyne.TextAlignCenter
	p.seekIndPanel = container.NewCenter(container.NewStack(
		p.seekIndBg,
		container.NewPadded(container.NewPadded(p.seekIndText)),
	))
	p.seekIndPanel.Hide()

	p.seek = widget.NewSlider(0, dur)
	p.seek.Step = 0.1
	p.seek.OnChanged = func(v float64) {
		if p.suppressSeek {
			return
		}
		p.mu.Lock()
		p.userSeeking = true
		p.mu.Unlock()
		p.updateTimeLabel(v)
	}
	p.seek.OnChangeEnded = func(v float64) {
		if p.suppressSeek {
			return
		}
		p.mu.Lock()
		p.userSeeking = false
		p.mu.Unlock()
		p.seekTo(v)
	}

	p.timeLbl = widget.NewLabel("0:00 / " + fmtDur(dur))
	p.timeLbl.TextStyle = fyne.TextStyle{Monospace: true}

	p.volBtn = newButtonWithIcon("", theme.VolumeUpIcon(), p.showVolumePopup)
	p.volBtn.Importance = widget.LowImportance

	go p.positionTicker()
	return p
}

// object assembles the player UI: the video frame with a translucent control
// bar (play/pause, seek, time, volume) overlaid across its bottom edge, like a
// standard video player.
func (p *videoPlayer) object() fyne.CanvasObject {
	black := canvas.NewRectangle(color.NRGBA{0x0a, 0x0a, 0x0a, 0xff})
	// Clicking the picture area toggles play/pause. The control-bar widgets sit
	// on top and consume their own taps, so only the video body triggers this.
	frame := newTapableContainer(
		container.NewStack(black, container.NewCenter(p.img)),
		p.togglePlay,
	)

	barBg := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0xb0})
	left := container.NewHBox(p.playBtn, p.back10, p.fwd10)
	right := container.NewHBox(p.timeLbl, p.volBtn)
	bar := container.NewBorder(nil, nil, left, right, p.seek)
	barStack := container.NewStack(barBg, container.NewPadded(bar))

	// Pin the bar to the bottom of the frame via a top spacer, stacked over it.
	controls := container.NewVBox(layout.NewSpacer(), barStack)
	// Seek indicator floats above everything (transient, non-interactive).
	return container.NewStack(frame, controls, p.seekIndPanel)
}

// showVolumePopup shows a vertical volume slider floating just above the speaker
// button, dismissed by clicking elsewhere.
func (p *videoPlayer) showVolumePopup() {
	if p.cv == nil {
		return
	}
	vs := widget.NewSlider(0, 100)
	vs.Orientation = widget.Vertical
	vs.Step = 1
	p.mu.Lock()
	vs.Value = float64(p.vol)
	p.mu.Unlock()
	vs.OnChangeEnded = func(v float64) { p.applyVolume(int(v)) }

	panel := container.NewStack(
		canvas.NewRectangle(color.NRGBA{0x1c, 0x1c, 0x1c, 0xf5}),
		container.NewPadded(container.NewGridWrap(fyne.NewSize(28, 118), vs)),
	)
	pop := widget.NewPopUp(panel, p.cv)
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(p.volBtn)
	pop.ShowAtPosition(fyne.NewPos(pos.X-4, pos.Y-142))
}

func (p *videoPlayer) togglePlay() {
	p.mu.Lock()
	playing := p.playing
	p.mu.Unlock()
	if playing {
		p.pause()
	} else {
		p.mu.Lock()
		pos := p.pos
		if pos >= p.duration-0.05 {
			pos = 0 // restart from the beginning if we were at the end
		}
		p.mu.Unlock()
		p.startPlayback(pos)
	}
}

func (p *videoPlayer) startPlayback(pos float64) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.gen++
	g := p.gen
	p.pos = pos
	p.playStart = time.Now()
	p.playing = true
	vol := p.vol
	p.mu.Unlock()

	vcmd := exec.Command("ffmpeg",
		"-ss", fmt.Sprintf("%.3f", pos), "-i", p.path,
		"-loglevel", "quiet",
		"-f", "rawvideo", "-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", p.dispW, p.dispH),
		"-r", fmt.Sprintf("%.4f", p.fps),
		"pipe:1")
	stdout, err := vcmd.StdoutPipe()
	if err != nil || vcmd.Start() != nil {
		p.mu.Lock()
		p.playing = false
		p.mu.Unlock()
		return
	}

	acmd := exec.Command("ffplay",
		"-nodisp", "-autoexit", "-loglevel", "quiet",
		"-ss", fmt.Sprintf("%.3f", pos), "-volume", strconv.Itoa(vol),
		"-vn", p.path)
	_ = acmd.Start()

	p.mu.Lock()
	p.vidCmd = vcmd
	p.audCmd = acmd
	p.mu.Unlock()

	p.updatePlayIcon(true)
	go p.readFrames(g, stdout, vcmd)
}

func (p *videoPlayer) readFrames(g int, stdout io.ReadCloser, cmd *exec.Cmd) {
	frameSize := p.dispW * p.dispH * 4
	buf := make([]byte, frameSize)
	interval := time.Duration(float64(time.Second) / p.fps)
	next := time.Now().Add(interval)

	for {
		p.mu.Lock()
		cur := p.gen
		p.mu.Unlock()
		if cur != g {
			break // retired by a newer play/seek/pause
		}

		if _, err := io.ReadFull(stdout, buf); err != nil {
			p.handleEOF(g)
			break
		}

		frame := image.NewRGBA(image.Rect(0, 0, p.dispW, p.dispH))
		copy(frame.Pix, buf)
		p.ui.runOnMain(func() {
			p.mu.Lock()
			c := p.gen
			p.mu.Unlock()
			if c == g {
				p.img.Image = frame
				p.img.Refresh()
			}
		})

		if d := time.Until(next); d > 0 {
			time.Sleep(d)
		}
		next = next.Add(interval)
	}

	_ = stdout.Close()
	_ = cmd.Wait()
}

// handleEOF resets to a paused state at the end of the clip.
func (p *videoPlayer) handleEOF(g int) {
	p.mu.Lock()
	if p.gen != g {
		p.mu.Unlock()
		return
	}
	p.gen++
	p.playing = false
	p.pos = p.duration
	p.mu.Unlock()

	p.updatePlayIcon(false)
	p.ui.runOnMain(func() {
		p.suppressSeek = true
		p.seek.SetValue(p.duration)
		p.suppressSeek = false
		p.updateTimeLabel(p.duration)
	})
}

func (p *videoPlayer) pause() {
	p.mu.Lock()
	if !p.playing {
		p.mu.Unlock()
		return
	}
	p.pos += time.Since(p.playStart).Seconds()
	if p.pos > p.duration {
		p.pos = p.duration
	}
	p.playing = false
	p.gen++
	vcmd, acmd := p.vidCmd, p.audCmd
	p.vidCmd, p.audCmd = nil, nil
	p.mu.Unlock()

	killCmd(vcmd)
	killCmd(acmd)
	p.updatePlayIcon(false)
}

// currentPos returns the live playback position in seconds.
func (p *videoPlayer) currentPos() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.livePosLocked()
}

// seekBy jumps delta seconds from the current position and flashes the indicator.
func (p *videoPlayer) seekBy(delta float64) {
	p.seekTo(p.currentPos() + delta)
	p.flashSeek(delta)
}

// arrowSeek handles keyboard seeking: a tap jumps 10s; holding the key (repeat
// events, same direction, in quick succession) accelerates the step
// exponentially up to 30s per repeat. dir is -1 for rewind, +1 for forward.
//
// The heavy seek (which restarts ffmpeg/ffplay) is debounced: OS key-repeat
// fires ~30x/sec, so we accumulate the target and only actually seek ~180ms
// after the last press. The indicator updates live on every press.
func (p *videoPlayer) arrowSeek(dir int) {
	now := time.Now()
	p.mu.Lock()
	held := now.Sub(p.lastArrowTime) < 350*time.Millisecond && p.lastArrowDir == dir && p.arrowStep > 0
	if held {
		p.arrowStep *= 1.6
		if p.arrowStep > 30 {
			p.arrowStep = 30
		}
	} else {
		p.arrowStep = 10
		p.burstStart = p.livePosLocked()
		p.pendingTarget = p.burstStart
	}
	p.lastArrowTime = now
	p.lastArrowDir = dir

	p.pendingTarget += float64(dir) * p.arrowStep
	if p.pendingTarget < 0 {
		p.pendingTarget = 0
	}
	if p.pendingTarget > p.duration {
		p.pendingTarget = p.duration
	}
	cumulative := p.pendingTarget - p.burstStart

	if p.seekTimer != nil {
		p.seekTimer.Stop()
	}
	p.seekTimer = time.AfterFunc(180*time.Millisecond, func() {
		p.mu.Lock()
		target := p.pendingTarget
		p.mu.Unlock()
		p.seekTo(target)
	})
	p.mu.Unlock()

	p.flashSeek(cumulative)
}

// livePosLocked returns the current position; caller must hold p.mu.
func (p *videoPlayer) livePosLocked() float64 {
	pos := p.pos
	if p.playing {
		pos += time.Since(p.playStart).Seconds()
	}
	if pos < 0 {
		pos = 0
	}
	if pos > p.duration {
		pos = p.duration
	}
	return pos
}

// flashSeek shows the seek amount ("«  10s" / "10s  »") and fades it out.
func (p *videoPlayer) flashSeek(delta float64) {
	amt := int(math.Abs(delta) + 0.5)
	if delta < 0 {
		p.seekIndText.Text = fmt.Sprintf("«  %ds", amt)
	} else {
		p.seekIndText.Text = fmt.Sprintf("%ds  »", amt)
	}
	if p.seekIndAnim != nil {
		p.seekIndAnim.Stop()
	}
	p.seekIndPanel.Show()
	p.seekIndAnim = fyne.NewAnimation(700*time.Millisecond, func(f float32) {
		p.seekIndText.Color = color.NRGBA{0xff, 0xff, 0xff, uint8(255 * (1 - f))}
		p.seekIndBg.FillColor = color.NRGBA{0x00, 0x00, 0x00, uint8(float32(0xcc) * (1 - f))}
		canvas.Refresh(p.seekIndText)
		canvas.Refresh(p.seekIndBg)
		if f >= 1 {
			p.seekIndPanel.Hide()
		}
	})
	p.seekIndAnim.Curve = fyne.AnimationEaseIn
	p.seekIndAnim.Start()
}

func (p *videoPlayer) seekTo(pos float64) {
	if pos < 0 {
		pos = 0
	}
	if pos > p.duration {
		pos = p.duration
	}
	p.mu.Lock()
	wasPlaying := p.playing
	// Retire any running decode and stop audio before restarting.
	p.gen++
	vcmd, acmd := p.vidCmd, p.audCmd
	p.vidCmd, p.audCmd = nil, nil
	p.playing = false
	p.pos = pos
	p.mu.Unlock()
	killCmd(vcmd)
	killCmd(acmd)

	if wasPlaying {
		p.startPlayback(pos)
	} else {
		// Paused seek: show the frame at pos without starting playback.
		p.showStillAt(pos)
		p.updateTimeLabel(pos)
	}
}

// showStillAt grabs a single frame at pos and displays it (used for paused seeks).
func (p *videoPlayer) showStillAt(pos float64) {
	go func() {
		im := frameAt(p.path, pos, p.dispW, p.dispH)
		if im == nil {
			return
		}
		p.ui.runOnMain(func() {
			p.mu.Lock()
			playing := p.playing
			p.mu.Unlock()
			if !playing { // don't clobber live playback
				p.img.Image = im
				p.img.Refresh()
			}
		})
	}()
}

func (p *videoPlayer) applyVolume(v int) {
	p.mu.Lock()
	p.vol = v
	playing := p.playing
	pos := p.pos
	if playing {
		pos += time.Since(p.playStart).Seconds()
	}
	acmd := p.audCmd
	p.mu.Unlock()

	if !playing {
		return // applied on next play
	}
	killCmd(acmd)
	newA := exec.Command("ffplay",
		"-nodisp", "-autoexit", "-loglevel", "quiet",
		"-ss", fmt.Sprintf("%.3f", pos), "-volume", strconv.Itoa(v),
		"-vn", p.path)
	_ = newA.Start()
	p.mu.Lock()
	if p.playing {
		p.audCmd = newA
	} else {
		killCmd(newA)
	}
	p.mu.Unlock()
}

// positionTicker advances the seek slider + time label while playing.
func (p *videoPlayer) positionTicker() {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for range t.C {
		p.mu.Lock()
		if p.stopped {
			p.mu.Unlock()
			return
		}
		playing := p.playing
		seeking := p.userSeeking
		pos := p.pos
		if playing {
			pos += time.Since(p.playStart).Seconds()
			if pos > p.duration {
				pos = p.duration
			}
		}
		p.mu.Unlock()

		if !playing || seeking {
			continue
		}
		p.ui.runOnMain(func() {
			p.suppressSeek = true
			p.seek.SetValue(pos)
			p.suppressSeek = false
			p.updateTimeLabel(pos)
		})
	}
}

func (p *videoPlayer) updateTimeLabel(pos float64) {
	p.timeLbl.SetText(fmtDur(pos) + " / " + fmtDur(p.duration))
}

func (p *videoPlayer) updatePlayIcon(playing bool) {
	p.ui.runOnMain(func() {
		if playing {
			p.playBtn.SetIcon(theme.MediaPauseIcon())
		} else {
			p.playBtn.SetIcon(theme.MediaPlayIcon())
		}
	})
}

// destroy stops playback and releases all processes. Safe to call multiple times.
func (p *videoPlayer) destroy() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	p.playing = false
	p.gen++
	if p.seekTimer != nil {
		p.seekTimer.Stop()
		p.seekTimer = nil
	}
	vcmd, acmd := p.vidCmd, p.audCmd
	p.vidCmd, p.audCmd = nil, nil
	p.mu.Unlock()
	killCmd(vcmd)
	killCmd(acmd)
}

// ─── helpers ────────────────────────────────────────────────────────────────

func killCmd(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	go func() { _ = cmd.Wait() }() // reap without blocking the caller
}

func fmtDur(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(sec + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// fitVideoSize scales (w,h) to fit within (maxW,maxH), preserving aspect ratio,
// and rounds to even dimensions (safer for ffmpeg scaling). Falls back to the
// max box when the source dimensions are unknown.
func fitVideoSize(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return maxW, maxH
	}
	scale := float64(maxW) / float64(w)
	if s := float64(maxH) / float64(h); s < scale {
		scale = s
	}
	if scale > 1 {
		scale = 1 // don't upscale small videos
	}
	dw := int(float64(w)*scale) &^ 1
	dh := int(float64(h)*scale) &^ 1
	if dw < 2 {
		dw = 2
	}
	if dh < 2 {
		dh = 2
	}
	return dw, dh
}

// probeVideo returns width, height, fps, and duration (seconds) via ffprobe.
func probeVideo(path string) (w, h int, fps, dur float64) {
	fps, dur = 30, 0
	out, err := exec.Command("ffprobe", "-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,avg_frame_rate:format=duration",
		"-of", "default=noprint_wrappers=1:nokey=0", path).Output()
	if err != nil {
		return 0, 0, fps, dur
	}
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "width":
			w, _ = strconv.Atoi(v)
		case "height":
			h, _ = strconv.Atoi(v)
		case "avg_frame_rate":
			if num, den, ok := strings.Cut(v, "/"); ok {
				n, _ := strconv.ParseFloat(num, 64)
				d, _ := strconv.ParseFloat(den, 64)
				if d > 0 && n > 0 {
					fps = n / d
				}
			}
		case "duration":
			if d, e := strconv.ParseFloat(v, 64); e == nil && d > 0 {
				dur = d
			}
		}
	}
	if fps <= 0 || fps > 60 {
		fps = 30 // clamp pathological/oddly-reported rates for smooth-enough preview
	}
	return w, h, fps, dur
}

// frameAt extracts a single frame at pos (seconds), scaled to (w,h).
func frameAt(path string, pos float64, w, h int) image.Image {
	tmp := fmt.Sprintf("/tmp/swiftcap_seek_%d.jpg", time.Now().UnixNano())
	defer os.Remove(tmp)
	err := exec.Command("ffmpeg", "-y", "-loglevel", "quiet",
		"-ss", fmt.Sprintf("%.3f", pos), "-i", path,
		"-vframes", "1", "-s", fmt.Sprintf("%dx%d", w, h),
		"-q:v", "3", tmp).Run()
	if err != nil {
		return nil
	}
	return loadAnyImage(tmp)
}

package uiapp

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	bannerCardW = float32(440)
	bannerCardH = float32(88)
	bannerSlide = float32(16) // vertical slide-in distance for the open animation
)

// ─── outer controller ────────────────────────────────────────────────────────

type countdownBanner struct {
	bw   *countdownBannerWidget
	win  fyne.Window
	anim *fyne.Animation
	done chan struct{}
	once sync.Once
}

func newCountdownBanner(
	a fyne.App,
	seconds int,
	prefix string,
	onComplete func(),
	onCancel func(),
) *countdownBanner {
	b := &countdownBanner{done: make(chan struct{})}

	// Snapshot the live desktop so the overlay can show it as its own
	// background — this is what makes the window read as "transparent"
	// (no real OS transparency needed, nothing is dimmed or blocked) while
	// still being a single ordinary window underneath.
	screenW, screenH := getScreenSize()
	tmpFile := fmt.Sprintf("/tmp/swiftcap_cd_%d.png", time.Now().UnixNano())
	_ = takeScreenshot(screenW, screenH, tmpFile)
	bgImg := loadAnyImage(tmpFile)
	os.Remove(tmpFile)

	bw := &countdownBannerWidget{prefix: prefix, remaining: seconds, bgImage: bgImg}
	bw.onCancel = func() { b.finish(onCancel) }
	bw.ExtendBaseWidget(bw)
	b.bw = bw

	// Deliberately not using SetFullScreen: Fyne's glfw driver always shows the
	// window in its normal decorated/windowed state first and only switches it
	// to real fullscreen ~100ms later (internal/driver/glfw/window.go), which
	// is exactly the "window, then it switches" flash. A splash window is
	// borderless from the instant it's created — sized to the full screen, it
	// looks identical but never goes through that transition.
	var win fyne.Window
	if drv, ok := a.Driver().(desktop.Driver); ok {
		win = drv.CreateSplashWindow()
	} else {
		win = a.NewWindow("")
	}
	win.SetPadded(false)
	win.SetFixedSize(true)
	win.Resize(fyne.NewSize(float32(screenW), float32(screenH)))
	win.SetContent(bw)
	win.Canvas().Focus(bw)
	b.win = win
	win.Show()

	// Open animation: 0 → 1, ease-out.
	b.anim = fyne.NewAnimation(220*time.Millisecond, func(t float32) {
		bw.animT = t
		bw.Refresh()
	})
	b.anim.Curve = fyne.AnimationEaseOut
	b.anim.Start()

	go b.tick(seconds, onComplete)
	return b
}

func (b *countdownBanner) tick(seconds int, onComplete func()) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	remaining := seconds
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			remaining--
			b.bw.mu.Lock() 
			b.bw.remaining = remaining
			b.bw.mu.Unlock()
			b.bw.Refresh()
			if remaining <= 0 {
				b.finish(onComplete)
				return
			}
		}
	}
}

func (b *countdownBanner) finish(cb func()) {
	b.once.Do(func() {
		close(b.done)
		startT := b.bw.animT
		if b.anim != nil {
			b.anim.Stop()
		}
		closeAnim := fyne.NewAnimation(160*time.Millisecond, func(t float32) {
			b.bw.animT = startT * (1 - t)
			b.bw.Refresh()
			if t >= 1.0 {
				if b.win != nil {
					b.win.Close()
				}
				go func() {
					time.Sleep(80 * time.Millisecond)
					if cb != nil {
						cb()
					}
				}()
			}
		})
		closeAnim.Curve = fyne.AnimationEaseIn
		b.anim = closeAnim
		closeAnim.Start()
	})
}

func (b *countdownBanner) close() { b.finish(nil) }

// ─── widget ──────────────────────────────────────────────────────────────────

type countdownBannerWidget struct {
	widget.BaseWidget

	prefix    string
	remaining int
	bgImage   image.Image
	onCancel  func()
	animT     float32 // 0 = hidden, 1 = fully shown

	mu          sync.Mutex
	cancelHover bool

	cancelX1, cancelY1, cancelX2, cancelY2 float32
}

func (w *countdownBannerWidget) CreateRenderer() fyne.WidgetRenderer {
	// Background is a snapshot of the desktop taken the instant the overlay
	// opens — this is what reads as "transparent": every pixel outside the
	// card is identical to what was already on screen, nothing is dimmed.
	var bgObj fyne.CanvasObject
	if w.bgImage != nil {
		img := canvas.NewImageFromImage(w.bgImage)
		img.FillMode = canvas.ImageFillStretch
		img.ScaleMode = canvas.ImageScaleFastest
		bgObj = img
	} else {
		bgObj = canvas.NewRectangle(color.Transparent)
	}

	card := canvas.NewRectangle(color.NRGBA{0x1e, 0x1e, 0x1e, 0xf2})
	card.CornerRadius = 14
	card.StrokeColor = color.NRGBA{0x44, 0x44, 0x44, 0xff}
	card.StrokeWidth = 1

	numText := canvas.NewText("0", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	numText.TextSize = 36
	numText.TextStyle = fyne.TextStyle{Bold: true}
	numText.Alignment = fyne.TextAlignCenter

	msgText := canvas.NewText("", color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
	msgText.TextSize = 14

	cancelBg := canvas.NewRectangle(color.NRGBA{0x33, 0x33, 0x33, 0xff})
	cancelBg.CornerRadius = 6

	cancelLbl := canvas.NewText("Cancel", color.NRGBA{0xee, 0xee, 0xee, 0xff})
	cancelLbl.TextSize = 13
	cancelLbl.TextStyle = fyne.TextStyle{Bold: true}
	cancelLbl.Alignment = fyne.TextAlignCenter

	return &countdownBannerRenderer{
		w:         w,
		bgObj:     bgObj,
		card:      card,
		numText:   numText,
		msgText:   msgText,
		cancelBg:  cancelBg,
		cancelLbl: cancelLbl,
		objs:      []fyne.CanvasObject{bgObj, card, numText, msgText, cancelBg, cancelLbl},
	}
}

func (w *countdownBannerWidget) MinSize() fyne.Size {
	return fyne.NewSize(bannerCardW, bannerCardH+bannerSlide)
}

func (w *countdownBannerWidget) Tapped(ev *fyne.PointEvent) {
	if ev.Position.X >= w.cancelX1 && ev.Position.X <= w.cancelX2 &&
		ev.Position.Y >= w.cancelY1 && ev.Position.Y <= w.cancelY2 {
		if w.onCancel != nil {
			w.onCancel()
		}
	}
}
func (w *countdownBannerWidget) TappedSecondary(*fyne.PointEvent) {}

func (w *countdownBannerWidget) MouseMoved(ev *desktop.MouseEvent) {
	hover := ev.Position.X >= w.cancelX1 && ev.Position.X <= w.cancelX2 &&
		ev.Position.Y >= w.cancelY1 && ev.Position.Y <= w.cancelY2
	w.mu.Lock()
	changed := hover != w.cancelHover
	w.cancelHover = hover
	w.mu.Unlock()
	if changed {
		w.Refresh()
	}
}
func (w *countdownBannerWidget) MouseIn(_ *desktop.MouseEvent) {}
func (w *countdownBannerWidget) MouseOut()                     {}

func (w *countdownBannerWidget) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && w.onCancel != nil {
		w.onCancel()
	}
}
func (w *countdownBannerWidget) FocusGained()     {}
func (w *countdownBannerWidget) FocusLost()       {}
func (w *countdownBannerWidget) TypedRune(_ rune) {}

// ─── renderer ────────────────────────────────────────────────────────────────

type countdownBannerRenderer struct {
	w         *countdownBannerWidget
	bgObj     fyne.CanvasObject
	card      *canvas.Rectangle
	numText   *canvas.Text
	msgText   *canvas.Text
	cancelBg  *canvas.Rectangle
	cancelLbl *canvas.Text
	objs      []fyne.CanvasObject
}

func (r *countdownBannerRenderer) Layout(size fyne.Size) {
	w := r.w
	t := w.animT

	w.mu.Lock()
	remaining := w.remaining
	hover := w.cancelHover
	w.mu.Unlock()

	// Background snapshot fills the entire screen-sized window.
	r.bgObj.Move(fyne.NewPos(0, 0))
	r.bgObj.Resize(size)

	// Card centred on screen, sliding up slightly on open/close.
	cardX := (size.Width - bannerCardW) / 2
	baseCardY := (size.Height - bannerCardH) / 2
	slideOff := bannerSlide * (1 - t)
	cardY := baseCardY + slideOff

	r.card.Move(fyne.NewPos(cardX, cardY))
	r.card.Resize(fyne.NewSize(bannerCardW, bannerCardH))

	// Large countdown number — left of card, vertically centred.
	const numW = float32(52)
	numX := cardX + 20
	r.numText.Text = fmt.Sprintf("%d", remaining)
	r.numText.Move(fyne.NewPos(numX, cardY+21))
	r.numText.Resize(fyne.NewSize(numW, 46))
	canvas.Refresh(r.numText)

	// Prefix message — right of number, vertically centred.
	msgX := numX + numW + 10
	r.msgText.Text = w.prefix + "…"
	r.msgText.Move(fyne.NewPos(msgX, cardY+35))
	r.msgText.Resize(fyne.NewSize(bannerCardW-numW-120, 18))
	canvas.Refresh(r.msgText)

	// Cancel button — right edge of card.
	const cancelW = float32(82)
	const cancelH = float32(34)
	cancelX := cardX + bannerCardW - cancelW - 16
	cancelBtnY := cardY + (bannerCardH-cancelH)/2

	if hover {
		r.cancelBg.FillColor = color.NRGBA{0x4a, 0x4a, 0x4a, 0xff}
		r.cancelLbl.Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	} else {
		r.cancelBg.FillColor = color.NRGBA{0x33, 0x33, 0x33, 0xff}
		r.cancelLbl.Color = color.NRGBA{0xee, 0xee, 0xee, 0xff}
	}
	r.cancelBg.Move(fyne.NewPos(cancelX, cancelBtnY))
	r.cancelBg.Resize(fyne.NewSize(cancelW, cancelH))
	r.cancelLbl.Move(fyne.NewPos(cancelX, cancelBtnY+cancelH/2-8))
	r.cancelLbl.Resize(fyne.NewSize(cancelW, 16))

	w.cancelX1 = cancelX
	w.cancelY1 = cancelBtnY
	w.cancelX2 = cancelX + cancelW
	w.cancelY2 = cancelBtnY + cancelH
}

func (r *countdownBannerRenderer) MinSize() fyne.Size {
	return r.w.MinSize()
}
func (r *countdownBannerRenderer) Refresh()                     { r.Layout(r.w.Size()); canvas.Refresh(r.w) }
func (r *countdownBannerRenderer) Destroy()                     {}
func (r *countdownBannerRenderer) Objects() []fyne.CanvasObject { return r.objs }

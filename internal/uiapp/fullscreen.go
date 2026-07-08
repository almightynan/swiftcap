package uiapp

import (
	"image"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ─── fading hint ───────────────────────────────────────────────────────────────

// newFullscreenHint returns a top-centre pill showing text and a function that
// starts a fade-out after a couple of seconds. Add the object to the fullscreen
// content stack and call the returned start func once the window is shown.
func newFullscreenHint(text string) (fyne.CanvasObject, func()) {
	lbl := canvas.NewText(text, color.NRGBA{0xff, 0xff, 0xff, 0xff})
	lbl.TextSize = 15
	lbl.Alignment = fyne.TextAlignCenter
	bg := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0xba})
	bg.CornerRadius = 10
	pill := container.NewStack(bg, container.NewPadded(container.NewPadded(lbl)))
	obj := container.NewVBox(newHeightSpacer(28), container.NewCenter(pill), layout.NewSpacer())

	start := func() {
		time.AfterFunc(2600*time.Millisecond, func() {
			anim := fyne.NewAnimation(600*time.Millisecond, func(f float32) {
				lbl.Color = color.NRGBA{0xff, 0xff, 0xff, uint8(255 * (1 - f))}
				bg.FillColor = color.NRGBA{0, 0, 0, uint8(float32(0xba) * (1 - f))}
				canvas.Refresh(lbl)
				canvas.Refresh(bg)
				if f >= 1 {
					obj.Hide()
				}
			})
			anim.Curve = fyne.AnimationEaseIn
			anim.Start()
		})
	}
	return obj, start
}

// ─── zoomable image (fullscreen photo viewer) ──────────────────────────────────

// zoomImage shows an image centred and contain-fit, with scroll-to-zoom and
// drag-to-pan. A plain click (no drag) invokes onTap (used to exit).
type zoomImage struct {
	widget.BaseWidget
	iw, ih     int
	img        *canvas.Image
	bg         *canvas.Rectangle
	zoom       float32
	panX, panY float32
	onTap      func()
}

func newZoomImage(im image.Image, onTap func()) *zoomImage {
	b := im.Bounds()
	z := &zoomImage{iw: b.Dx(), ih: b.Dy(), zoom: 1, onTap: onTap}
	z.bg = canvas.NewRectangle(color.NRGBA{0x0a, 0x0a, 0x0a, 0xff})
	z.img = canvas.NewImageFromImage(im)
	z.img.FillMode = canvas.ImageFillStretch
	z.img.ScaleMode = canvas.ImageScaleFastest
	z.ExtendBaseWidget(z)
	return z
}

func (z *zoomImage) Tapped(*fyne.PointEvent)          { if z.onTap != nil { z.onTap() } }
func (z *zoomImage) TappedSecondary(*fyne.PointEvent) {}

func (z *zoomImage) Scrolled(ev *fyne.ScrollEvent) {
	z.zoom += ev.Scrolled.DY * 0.06
	if z.zoom < 1 {
		z.zoom = 1
	}
	if z.zoom > 8 {
		z.zoom = 8
	}
	if z.zoom == 1 {
		z.panX, z.panY = 0, 0
	}
	z.Refresh()
}

func (z *zoomImage) Dragged(ev *fyne.DragEvent) {
	if z.zoom <= 1 {
		return
	}
	z.panX += ev.Dragged.DX
	z.panY += ev.Dragged.DY
	z.Refresh()
}
func (z *zoomImage) DragEnd() {}

func (z *zoomImage) Cursor() desktop.Cursor {
	if z.zoom > 1 {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (z *zoomImage) CreateRenderer() fyne.WidgetRenderer {
	return &zoomImageRenderer{z: z, objs: []fyne.CanvasObject{z.bg, z.img}}
}

type zoomImageRenderer struct {
	z    *zoomImage
	objs []fyne.CanvasObject
}

func (r *zoomImageRenderer) Layout(size fyne.Size) {
	z := r.z
	z.bg.Resize(size)
	z.bg.Move(fyne.NewPos(0, 0))
	if z.iw == 0 || z.ih == 0 || size.Width < 1 || size.Height < 1 {
		return
	}
	s := size.Width / float32(z.iw)
	if v := size.Height / float32(z.ih); v < s {
		s = v
	}
	dw := float32(z.iw) * s * z.zoom
	dh := float32(z.ih) * s * z.zoom
	cx := size.Width/2 + z.panX
	cy := size.Height/2 + z.panY
	z.img.Move(fyne.NewPos(cx-dw/2, cy-dh/2))
	z.img.Resize(fyne.NewSize(dw, dh))
	canvas.Refresh(z.img)
}
func (r *zoomImageRenderer) MinSize() fyne.Size           { return fyne.NewSize(320, 240) }
func (r *zoomImageRenderer) Refresh()                     { r.Layout(r.z.Size()) }
func (r *zoomImageRenderer) Destroy()                     {}
func (r *zoomImageRenderer) Objects() []fyne.CanvasObject { return r.objs }

// ─── fullscreen shows ──────────────────────────────────────────────────────────

// showImageFullscreen shows img in a screen-filling viewer. Scroll to zoom, drag
// to pan, click anywhere or press Esc to exit.
func showImageFullscreen(a fyne.App, img image.Image) {
	screenW, screenH := getScreenSize()
	win := newFullscreenOverlayWindow(a, screenW, screenH)

	zi := newZoomImage(img, func() { win.Close() })
	hint, startHint := newFullscreenHint("Scroll to zoom  ·  click or Esc to exit")
	win.SetContent(container.NewStack(zi, hint))
	win.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			win.Close()
		}
	})
	win.Show()
	startHint()
}

// showVideoFullscreen plays the clip in a fullscreen window, resuming from
// startPos. Exit only via Esc or the minimise button in the controls.
func showVideoFullscreen(ui *RecordingUI, path string, startPos float64) {
	screenW, screenH := getScreenSize()
	win := newFullscreenOverlayWindow(ui.app, screenW, screenH)

	// Decode a bit below native for smooth playback; still fills most of a screen.
	maxW := min(screenW, 1600)
	maxH := min(screenH-110, 900)
	p := newVideoPlayer(ui, path, maxW, maxH)
	p.fullscreen = true
	p.onExitFS = func() {
		win.Close()
		p.destroy()
	}

	hint, startHint := newFullscreenHint("Press Esc to exit fullscreen")
	win.SetContent(container.NewStack(p.object(), hint))
	win.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		switch k.Name {
		case fyne.KeyEscape:
			p.onExitFS()
		case fyne.KeySpace:
			p.togglePlay()
		case fyne.KeyLeft:
			p.arrowSeek(-1)
		case fyne.KeyRight:
			p.arrowSeek(1)
		}
	})
	win.Show()
	startHint()

	// Resume from where the modal was, then play. Deferred so the window has laid
	// out before ffmpeg starts feeding frames.
	go func() {
		time.Sleep(120 * time.Millisecond)
		ui.runOnMain(func() {
			if startPos > 0.5 {
				p.seekTo(startPos)
			}
			p.togglePlay()
		})
	}()
}

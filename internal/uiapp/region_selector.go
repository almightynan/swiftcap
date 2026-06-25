package uiapp

import (
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ─── regionSelector ─────────────────────────────────────────────────────────

type regionSelector struct {
	app        fyne.App
	mainWindow fyne.Window
	onSelected func(string)
	onCancel   func()
}

func newRegionSelector(a fyne.App, mainWin fyne.Window, onSelected func(string), onCancel func()) *regionSelector {
	return &regionSelector{app: a, mainWindow: mainWin, onSelected: onSelected, onCancel: onCancel}
}

func (rs *regionSelector) Show() {
	go func() {
		// Prefer slop when installed — it's fast and compositor-aware.
		if slopPath, err := exec.LookPath("slop"); err == nil {
			if rs.mainWindow != nil {
				rs.mainWindow.Hide()
				time.Sleep(200 * time.Millisecond)
			}
			region, err := runSlop(slopPath)
			if rs.mainWindow != nil {
				rs.mainWindow.Show()
				rs.mainWindow.RequestFocus()
			}
			if err == nil {
				if region != "" {
					// Validate that the region has non-zero size
					var w, h, x, y int
					if n, _ := fmt.Sscanf(region, "%dx%d+%d+%d", &w, &h, &x, &y); n == 4 && w >= 4 && h >= 4 {
						if rs.onSelected != nil {
							rs.onSelected(region)
						}
					} else if rs.onCancel != nil {
						rs.onCancel()
					}
				} else if rs.onCancel != nil {
					rs.onCancel()
				}
				return
			}
		}

		// Built-in fallback: fullscreen screenshot overlay.
		rs.showBuiltinSelector()
	}()
}

func (rs *regionSelector) showBuiltinSelector() {
	screenW, screenH := getScreenSize()

	tmpFile := fmt.Sprintf("/tmp/swiftcap_sel_%d.png", time.Now().UnixNano())
	_ = takeScreenshot(screenW, screenH, tmpFile)

	if rs.mainWindow != nil {
		rs.mainWindow.Hide()
		time.Sleep(150 * time.Millisecond)
	}

	resultCh := make(chan string, 1)

	win := rs.app.NewWindow("")
	win.SetPadded(false)
	win.SetFixedSize(true)
	win.SetFullScreen(true)

	overlay := newRegionOverlayWidget(tmpFile, screenW, screenH, func(region string) {
		win.Close()
		os.Remove(tmpFile)
		resultCh <- region
	})

	win.SetContent(overlay)
	win.Canvas().Focus(overlay)
	win.Show()

	region := <-resultCh

	if rs.mainWindow != nil {
		rs.mainWindow.Show()
		rs.mainWindow.RequestFocus()
	}

	if region != "" {
		if rs.onSelected != nil {
			rs.onSelected(region)
		}
	} else if rs.onCancel != nil {
		rs.onCancel()
	}
}

// ─── regionOverlayWidget ────────────────────────────────────────────────────

type regionOverlayWidget struct {
	widget.BaseWidget

	bgFile  string
	bgImage image.Image // decoded screenshot for magnifier sampling (immutable after init)
	screenW int
	screenH int
	onDone  func(string) // WxH+X+Y, or "" for cancel

	mu      sync.Mutex
	started bool
	startX  float32
	startY  float32
	curX    float32
	curY    float32
	mouseX  float32
	mouseY  float32
	done    bool
}

func newRegionOverlayWidget(bgFile string, screenW, screenH int, onDone func(string)) *regionOverlayWidget {
	w := &regionOverlayWidget{
		bgFile:  bgFile,
		screenW: screenW,
		screenH: screenH,
		onDone:  onDone,
	}
	// Load screenshot for pixel-accurate magnifier sampling
	if f, err := os.Open(bgFile); err == nil {
		img, _, _ := image.Decode(f)
		f.Close()
		w.bgImage = img
	}
	w.ExtendBaseWidget(w)
	return w
}

func (w *regionOverlayWidget) CreateRenderer() fyne.WidgetRenderer {
	dim := color.NRGBA{0, 0, 0, 170}

	var bgObj fyne.CanvasObject
	if _, err := os.Stat(w.bgFile); err == nil {
		img := canvas.NewImageFromFile(w.bgFile)
		img.FillMode = canvas.ImageFillStretch
		bgObj = img
	} else {
		bgObj = canvas.NewRectangle(color.NRGBA{0x10, 0x10, 0x10, 0xff})
	}

	dimTop := canvas.NewRectangle(dim)
	dimBot := canvas.NewRectangle(dim)
	dimLeft := canvas.NewRectangle(dim)
	dimRight := canvas.NewRectangle(dim)

	selRect := canvas.NewRectangle(color.Transparent)
	selRect.StrokeColor = color.NRGBA{0x4d, 0xc1, 0xf5, 0xff}
	selRect.StrokeWidth = 2

	sizeText := canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	sizeText.TextSize = 13
	sizeText.TextStyle = fyne.TextStyle{Bold: true}

	instrText := canvas.NewText("Drag to select  ·  ESC to cancel", color.NRGBA{0xff, 0xff, 0xff, 0xbb})
	instrText.TextSize = 13
	instrText.Alignment = fyne.TextAlignCenter

	crossH := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x40})
	crossH.StrokeWidth = 1
	crossV := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x40})
	crossV.StrokeWidth = 1

	r := &regionOverlayRenderer{
		w:         w,
		bgObj:     bgObj,
		dimTop:    dimTop,
		dimBot:    dimBot,
		dimLeft:   dimLeft,
		dimRight:  dimRight,
		selRect:   selRect,
		sizeText:  sizeText,
		instrText: instrText,
		crossH:    crossH,
		crossV:    crossV,
		bgImage:   w.bgImage,
	}

	// Magnifier: zoomed view of the screen around the cursor
	r.magRaster = canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		if pw == 0 || ph == 0 {
			return color.Black
		}
		// The magnifier shows a 32×24 logical pixel region centered on the cursor.
		// Each magnifier pixel maps to 1/5 of a logical screen pixel (5× zoom).
		const regionW = 32.0
		const regionH = 24.0

		cx := r.magCX
		cy := r.magCY
		sx := r.magSX
		sy := r.magSY

		// Map raster pixel to logical screen coordinate
		fx := cx + (float32(px)/float32(pw)-0.5)*regionW
		fy := cy + (float32(py)/float32(ph)-0.5)*regionH

		bg := r.bgImage
		if bg != nil {
			ix := int(fx * sx)
			iy := int(fy * sy)
			bounds := bg.Bounds()
			if ix < bounds.Min.X {
				ix = bounds.Min.X
			}
			if iy < bounds.Min.Y {
				iy = bounds.Min.Y
			}
			if ix >= bounds.Max.X {
				ix = bounds.Max.X - 1
			}
			if iy >= bounds.Max.Y {
				iy = bounds.Max.Y - 1
			}
			rc, gc, bc, ac := bg.At(ix, iy).RGBA()
			return color.NRGBA{uint8(rc >> 8), uint8(gc >> 8), uint8(bc >> 8), uint8(ac >> 8)}
		}
		// Checkerboard pattern when no screenshot available
		if (px/8+py/8)%2 == 0 {
			return color.NRGBA{0x44, 0x44, 0x44, 0xff}
		}
		return color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
	})

	// Border around magnifier
	r.magBorder = canvas.NewRectangle(color.Transparent)
	r.magBorder.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0xcc}
	r.magBorder.StrokeWidth = 1

	// Outer shadow border for depth
	r.magShadow = canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0x99})

	// Crosshair lines inside the magnifier (red, thin)
	r.magCrossH = canvas.NewLine(color.NRGBA{0xff, 0x44, 0x44, 0xdd})
	r.magCrossH.StrokeWidth = 1
	r.magCrossV = canvas.NewLine(color.NRGBA{0xff, 0x44, 0x44, 0xdd})
	r.magCrossV.StrokeWidth = 1

	// Coordinate display below the magnifier
	r.coordBg = canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0xbb})
	r.coordText = canvas.NewText("", color.NRGBA{0x4d, 0xc1, 0xf5, 0xff})
	r.coordText.TextSize = 11
	r.coordText.TextStyle = fyne.TextStyle{Monospace: true}

	r.objects = []fyne.CanvasObject{
		bgObj,
		dimTop, dimBot, dimLeft, dimRight,
		selRect,
		r.magShadow,
		r.magRaster,
		r.magBorder,
		r.magCrossH, r.magCrossV,
		crossH, crossV,
		instrText, sizeText,
		r.coordBg, r.coordText,
	}

	return r
}

// ── mouse ───────────────────────────────────────────────────────────────────

func (w *regionOverlayWidget) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	w.mu.Lock()
	if !w.done {
		w.started = true
		w.startX = ev.Position.X
		w.startY = ev.Position.Y
		w.curX = ev.Position.X
		w.curY = ev.Position.Y
	}
	w.mu.Unlock()
	w.Refresh()
}

func (w *regionOverlayWidget) MouseUp(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	w.mu.Lock()
	if w.done || !w.started {
		w.mu.Unlock()
		return
	}
	sx, sy := w.startX, w.startY
	cx, cy := w.curX, w.curY
	size := w.Size()
	w.done = true
	w.mu.Unlock()

	minX := float32(math.Min(float64(sx), float64(cx)))
	minY := float32(math.Min(float64(sy), float64(cy)))
	maxX := float32(math.Max(float64(sx), float64(cx)))
	maxY := float32(math.Max(float64(sy), float64(cy)))
	selW := maxX - minX
	selH := maxY - minY

	if selW < 4 || selH < 4 {
		if w.onDone != nil {
			w.onDone("")
		}
		return
	}

	scale := float32(1.0)
	if size.Width > 0 {
		scale = float32(w.screenW) / size.Width
	}

	region := fmt.Sprintf("%dx%d+%d+%d",
		int(selW*scale), int(selH*scale),
		int(minX*scale), int(minY*scale),
	)
	if w.onDone != nil {
		w.onDone(region)
	}
}

func (w *regionOverlayWidget) MouseIn(_ *desktop.MouseEvent) {}
func (w *regionOverlayWidget) MouseOut()                     {}

func (w *regionOverlayWidget) MouseMoved(ev *desktop.MouseEvent) {
	w.mu.Lock()
	w.mouseX = ev.Position.X
	w.mouseY = ev.Position.Y
	if w.started && !w.done {
		w.curX = ev.Position.X
		w.curY = ev.Position.Y
	}
	w.mu.Unlock()
	w.Refresh()
}

// ── keyboard ────────────────────────────────────────────────────────────────

func (w *regionOverlayWidget) FocusGained()     {}
func (w *regionOverlayWidget) FocusLost()       {}
func (w *regionOverlayWidget) TypedRune(_ rune) {}

func (w *regionOverlayWidget) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name != fyne.KeyEscape {
		return
	}
	w.mu.Lock()
	if w.done {
		w.mu.Unlock()
		return
	}
	w.done = true
	w.mu.Unlock()
	if w.onDone != nil {
		w.onDone("")
	}
}

// ─── renderer ───────────────────────────────────────────────────────────────

type regionOverlayRenderer struct {
	w         *regionOverlayWidget
	bgObj     fyne.CanvasObject
	bgImage   image.Image
	dimTop    *canvas.Rectangle
	dimBot    *canvas.Rectangle
	dimLeft   *canvas.Rectangle
	dimRight  *canvas.Rectangle
	selRect   *canvas.Rectangle
	sizeText  *canvas.Text
	instrText *canvas.Text
	crossH    *canvas.Line
	crossV    *canvas.Line
	objects   []fyne.CanvasObject

	// Magnifier
	magRaster *canvas.Raster
	magShadow *canvas.Rectangle
	magBorder *canvas.Rectangle
	magCrossH *canvas.Line
	magCrossV *canvas.Line
	coordBg   *canvas.Rectangle
	coordText *canvas.Text

	// Updated each paint() before the raster pixel function reads them
	magCX, magCY float32 // cursor position in widget logical coords
	magSX, magSY float32 // scale: logical coord → image pixel
}

func (r *regionOverlayRenderer) MinSize() fyne.Size           { return fyne.NewSize(200, 100) }
func (r *regionOverlayRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *regionOverlayRenderer) Destroy()                     {}

func (r *regionOverlayRenderer) Layout(size fyne.Size) {
	r.bgObj.Resize(size)
	r.bgObj.Move(fyne.NewPos(0, 0))
	r.paint(size)
}

func (r *regionOverlayRenderer) Refresh() {
	r.paint(r.w.Size())
}

func (r *regionOverlayRenderer) paint(size fyne.Size) {
	w := r.w
	w.mu.Lock()
	started := w.started
	sx, sy := w.startX, w.startY
	cx, cy := w.curX, w.curY
	mx, my := w.mouseX, w.mouseY
	w.mu.Unlock()

	dim := color.NRGBA{0, 0, 0, 170}
	zero := fyne.NewSize(0, 0)

	scale := float32(1.0)
	if size.Width > 0 {
		scale = float32(w.screenW) / size.Width
	}

	// Full-screen crosshair follows cursor at all times
	r.crossH.Position1 = fyne.NewPos(0, my)
	r.crossH.Position2 = fyne.NewPos(size.Width, my)
	r.crossV.Position1 = fyne.NewPos(mx, 0)
	r.crossV.Position2 = fyne.NewPos(mx, size.Height)

	// Instruction bar at the bottom
	r.instrText.Move(fyne.NewPos(0, size.Height-34))
	r.instrText.Resize(fyne.NewSize(size.Width, 22))

	minX := float32(math.Min(float64(sx), float64(cx)))
	minY := float32(math.Min(float64(sy), float64(cy)))
	maxX := float32(math.Max(float64(sx), float64(cx)))
	maxY := float32(math.Max(float64(sy), float64(cy)))
	selW := maxX - minX
	selH := maxY - minY

	if !started || selW < 1 || selH < 1 {
		// No active selection — dim the whole screen
		r.dimTop.FillColor = dim
		r.dimTop.Move(fyne.NewPos(0, 0))
		r.dimTop.Resize(size)
		r.dimBot.Resize(zero)
		r.dimLeft.Resize(zero)
		r.dimRight.Resize(zero)
		r.selRect.Resize(zero)
		r.sizeText.Text = ""
	} else {
		// Four dim panels surrounding the selected area (selection shows bright screenshot)
		r.dimTop.FillColor = dim
		r.dimTop.Move(fyne.NewPos(0, 0))
		r.dimTop.Resize(fyne.NewSize(size.Width, minY))

		r.dimBot.FillColor = dim
		r.dimBot.Move(fyne.NewPos(0, maxY))
		r.dimBot.Resize(fyne.NewSize(size.Width, size.Height-maxY))

		r.dimLeft.FillColor = dim
		r.dimLeft.Move(fyne.NewPos(0, minY))
		r.dimLeft.Resize(fyne.NewSize(minX, selH))

		r.dimRight.FillColor = dim
		r.dimRight.Move(fyne.NewPos(maxX, minY))
		r.dimRight.Resize(fyne.NewSize(size.Width-maxX, selH))

		// Selection border
		r.selRect.Move(fyne.NewPos(minX, minY))
		r.selRect.Resize(fyne.NewSize(selW, selH))

		// Dimension label — above selection or below if near top
		r.sizeText.Text = fmt.Sprintf(" %d × %d ", int(selW*scale), int(selH*scale))
		lblY := minY - 22
		if lblY < 2 {
			lblY = maxY + 4
		}
		r.sizeText.Move(fyne.NewPos(minX, lblY))
	}

	// ── Magnifier ────────────────────────────────────────────────────────────

	// Update the scale fields before the raster pixel function reads them.
	// magSX/SY convert widget logical coords → image pixel coords.
	r.magCX = mx
	r.magCY = my
	if r.bgImage != nil {
		bounds := r.bgImage.Bounds()
		if size.Width > 0 {
			r.magSX = float32(bounds.Dx()) / size.Width
		}
		if size.Height > 0 {
			r.magSY = float32(bounds.Dy()) / size.Height
		}
	} else {
		r.magSX = scale
		r.magSY = scale
	}

	const magW = float32(160)
	const magH = float32(96)
	const coordH = float32(18)
	const offset = float32(22)

	// Position the magnifier box to the lower-right of the cursor.
	// Flip horizontally/vertically when near the screen edges.
	magX := mx + offset
	magY := my + offset
	if magX+magW > size.Width-4 {
		magX = mx - offset - magW
	}
	if magY+magH+coordH+4 > size.Height {
		magY = my - offset - magH - coordH - 4
	}
	if magX < 0 {
		magX = 0
	}
	if magY < 0 {
		magY = 0
	}

	// Shadow (1px dark halo behind everything)
	r.magShadow.Move(fyne.NewPos(magX-2, magY-2))
	r.magShadow.Resize(fyne.NewSize(magW+4, magH+4))

	// Raster (the zoomed image)
	r.magRaster.Move(fyne.NewPos(magX, magY))
	r.magRaster.Resize(fyne.NewSize(magW, magH))
	r.magRaster.Refresh()

	// White border on top of the raster
	r.magBorder.Move(fyne.NewPos(magX-1, magY-1))
	r.magBorder.Resize(fyne.NewSize(magW+2, magH+2))

	// Red crosshair in the center of the magnifier
	r.magCrossH.Position1 = fyne.NewPos(magX, magY+magH/2)
	r.magCrossH.Position2 = fyne.NewPos(magX+magW, magY+magH/2)
	r.magCrossV.Position1 = fyne.NewPos(magX+magW/2, magY)
	r.magCrossV.Position2 = fyne.NewPos(magX+magW/2, magY+magH)

	// Coordinate readout below the magnifier box
	ix := int(mx * scale)
	iy := int(my * scale)
	r.coordText.Text = fmt.Sprintf("  X:%-6dY:%-6d", ix, iy)
	coordY := magY + magH + 2
	r.coordBg.Move(fyne.NewPos(magX-1, coordY-1))
	r.coordBg.Resize(fyne.NewSize(magW+2, coordH+2))
	r.coordText.Move(fyne.NewPos(magX+2, coordY))
	r.coordText.Resize(fyne.NewSize(magW, coordH))
}

// ─── helpers ────────────────────────────────────────────────────────────────

func runSlop(slopPath string) (string, error) {
	cmd := exec.Command(slopPath,
		"-f", "%wx%h+%x+%y",
		"-b", "2",
		"-c", "0.48,0.72,0.95,0.6",
		"-q",
	)
	out, err := cmd.Output()
	if err != nil {
		if ex, ok := err.(*exec.ExitError); ok && ex.ExitCode() == 1 {
			return "", nil // user cancelled
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func takeScreenshot(w, h int, outFile string) error {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}
	if !strings.Contains(display, ".") {
		display += ".0"
	}
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "x11grab",
		"-video_size", fmt.Sprintf("%dx%d", w, h),
		"-i", display+"+0,0",
		"-vframes", "1",
		outFile,
	)
	return cmd.Run()
}

func getScreenSize() (int, int) {
	if runtime.GOOS == "linux" {
		if out, err := exec.Command("xdpyinfo").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				var w, h int
				if n, _ := fmt.Sscanf(strings.TrimSpace(line), "dimensions: %dx%d pixels", &w, &h); n == 2 && w > 0 {
					return w, h
				}
			}
		}
		if out, err := exec.Command("xrandr").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, " connected ") {
					var w, h int
					if n, _ := fmt.Sscanf(line, " %dx%d", &w, &h); n == 2 && w > 0 {
						return w, h
					}
				}
			}
		}
	}
	return 1920, 1080
}

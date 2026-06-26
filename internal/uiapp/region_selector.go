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
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ─── snipMode ────────────────────────────────────────────────────────────────

type snipMode int

const (
	snipRect       snipMode = 0
	snipFreeform   snipMode = 1
	snipFullscreen snipMode = 2
	snipMarkup     snipMode = 3
)

var snipLabels = [4]string{"Rectangle", "Freeform", "Full Screen", "Markup"}

func snipIcon(m snipMode) fyne.Resource {
	switch m {
	case snipRect:
		return theme.ContentCutIcon()
	case snipFreeform:
		return theme.DocumentCreateIcon()
	case snipFullscreen:
		return theme.ViewFullScreenIcon()
	default:
		return theme.DocumentIcon()
	}
}

// toolbar layout constants (logical pixels)
const (
	tbBtnW   = float32(116)
	tbBtnH   = float32(52) // icon + label
	tbPadX   = float32(6)
	tbPadY   = float32(8)
	tbTopOff = float32(14)
)

func tbPanelH() float32 { return tbBtnH + tbPadY*2 }
func tbPanelW() float32 { return 4*tbBtnW + 5*tbPadX }

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

// showSnipOverlay presents the snipping-tool overlay and returns the selected
// region string ("WxH+X+Y") or "" on cancel. Must be called from a goroutine.
func showSnipOverlay(a fyne.App, screenW, screenH int, bgFile string) string {
	resultCh := make(chan string, 1)
	win := a.NewWindow("")
	win.SetPadded(false)
	win.SetFixedSize(true)
	win.SetFullScreen(true)

	overlay := newRegionOverlayWidget(bgFile, screenW, screenH, func(region string) {
		win.Close()
		resultCh <- region
	})
	win.SetContent(overlay)
	win.Canvas().Focus(overlay)
	win.Show()
	return <-resultCh
}

// ─── regionOverlayWidget ────────────────────────────────────────────────────

type regionOverlayWidget struct {
	widget.BaseWidget

	bgFile  string
	bgImage image.Image
	screenW int
	screenH int
	onDone  func(string)

	mu          sync.Mutex
	mode        snipMode
	hoverBtn    int // -1=none, 0-3=button
	started     bool
	mouseDown   bool
	selComplete bool // selection made; waiting for Enter
	startX      float32
	startY      float32
	curX        float32
	curY        float32
	mouseX      float32
	mouseY      float32
	done        bool
	freePoints  []fyne.Position

	// animated indicator (not mutex-protected — always touched on main thread)
	indicX   float32
	modeAnim *fyne.Animation

	// ── Markup mode state (all guarded by mu) ──────────────────────────────
	markTool    markupTool
	markColor   color.NRGBA
	markSize    int
	markFill    bool
	markFillCol color.NRGBA
	markBlurType int
	showPalette bool
	palForFill  bool
	mkBuf       *image.RGBA
	mkBufW      int
	mkBufH      int
	mkUndoBufs  []*image.RGBA
	mkDrawing   bool
	mkStartX    float32
	mkStartY    float32
	mkCurX      float32
	mkCurY      float32
	mkPoints    []image.Point
	mkHoverCode int
}

func newRegionOverlayWidget(bgFile string, screenW, screenH int, onDone func(string)) *regionOverlayWidget {
	w := &regionOverlayWidget{
		bgFile:      bgFile,
		screenW:     screenW,
		screenH:     screenH,
		onDone:      onDone,
		mode:        snipRect,
		hoverBtn:    -1,
		markTool:    mkToolBrush,
		markColor:   color.NRGBA{0xff, 0x33, 0x33, 0xff},
		markSize:    5,
		markFill:    false,
		markFillCol: color.NRGBA{0xff, 0x33, 0x33, 0x60},
		markBlurType: 0,
		mkHoverCode: mkHitNone,
	}
	if f, err := os.Open(bgFile); err == nil {
		img, _, _ := image.Decode(f)
		f.Close()
		w.bgImage = img
	}
	w.ExtendBaseWidget(w)
	return w
}

// ── toolbar geometry ─────────────────────────────────────────────────────────

func (w *regionOverlayWidget) toolbarPanelXY() (panelX, panelY float32) {
	panelX = (w.Size().Width - tbPanelW()) / 2
	panelY = tbTopOff
	return
}

func (w *regionOverlayWidget) btnRect(i int) (x, y, bw, bh float32) {
	px, py := w.toolbarPanelXY()
	x = px + tbPadX + float32(i)*(tbBtnW+tbPadX)
	y = py + tbPadY
	bw = tbBtnW
	bh = tbBtnH
	return
}

func (w *regionOverlayWidget) hitTest(pos fyne.Position) int {
	for i := 0; i < 4; i++ {
		x, y, bw, bh := w.btnRect(i)
		if pos.X >= x && pos.X < x+bw && pos.Y >= y && pos.Y < y+bh {
			return i
		}
	}
	px, py := w.toolbarPanelXY()
	if pos.X >= px && pos.X < px+tbPanelW() && pos.Y >= py && pos.Y < py+tbPanelH() {
		return -2
	}
	return -1
}

// selectMode animates the indicator to newMode and resets selection state.
func (w *regionOverlayWidget) selectMode(newMode snipMode) {
	// Compute target X before locking, using current widget size
	targetX := float32(0)
	if w.Size().Width > 0 {
		bx, _, _, _ := w.btnRect(int(newMode))
		targetX = bx
	}
	startX := w.indicX

	if w.modeAnim != nil {
		w.modeAnim.Stop()
	}
	w.modeAnim = fyne.NewAnimation(160*time.Millisecond, func(f float32) {
		w.indicX = startX + (targetX-startX)*f
		w.Refresh()
	})
	w.modeAnim.Curve = fyne.AnimationEaseInOut
	w.modeAnim.Start()

	w.mu.Lock()
	w.mode = newMode
	w.started = false
	w.mouseDown = false
	w.selComplete = false
	w.freePoints = w.freePoints[:0]
	w.mu.Unlock()
}

// ── mouse ────────────────────────────────────────────────────────────────────

func (w *regionOverlayWidget) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	w.mu.Lock()
	if w.done {
		w.mu.Unlock()
		return
	}
	hit := w.hitTest(ev.Position)
	if hit >= 0 {
		if snipMode(hit) == snipFullscreen {
			w.done = true
			sw, sh := w.screenW, w.screenH
			w.mu.Unlock()
			if w.onDone != nil {
				w.onDone(fmt.Sprintf("%dx%d+0+0", sw, sh))
			}
			return
		}
		w.mu.Unlock()
		w.selectMode(snipMode(hit))
		return
	}
	if hit == -2 {
		w.mu.Unlock()
		return
	}

	// Markup mode: delegate all canvas events to markup handler
	if w.mode == snipMarkup {
		w.mu.Unlock()
		w.handleMarkupMouseDown(ev.Position)
		w.Refresh()
		return
	}

	// Canvas click — reset any complete selection and begin a new one
	w.selComplete = false
	w.started = true
	w.mouseDown = true
	w.startX = ev.Position.X
	w.startY = ev.Position.Y
	w.curX = ev.Position.X
	w.curY = ev.Position.Y
	if w.mode == snipFreeform {
		w.freePoints = w.freePoints[:0]
		w.freePoints = append(w.freePoints, ev.Position)
	}
	w.mu.Unlock()
	w.Refresh()
}

func (w *regionOverlayWidget) MouseUp(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	w.mu.Lock()
	mode := w.mode
	w.mu.Unlock()
	if mode == snipMarkup {
		w.handleMarkupMouseUp(ev.Position)
		return
	}
	w.mu.Lock()
	if w.done || !w.started || !w.mouseDown {
		w.mu.Unlock()
		return
	}
	w.mouseDown = false
	mode = w.mode
	sx, sy := w.startX, w.startY
	cx, cy := w.curX, w.curY
	freePoints := append([]fyne.Position(nil), w.freePoints...)
	size := w.Size()

	if snipSelectionValid(mode, sx, sy, cx, cy, freePoints, size) {
		w.selComplete = true
	}
	w.mu.Unlock()
	w.Refresh()
}

func (w *regionOverlayWidget) MouseIn(_ *desktop.MouseEvent) {}
func (w *regionOverlayWidget) MouseOut()                     {}

func (w *regionOverlayWidget) MouseMoved(ev *desktop.MouseEvent) {
	w.mu.Lock()
	mode := w.mode
	w.mu.Unlock()
	if mode == snipMarkup {
		w.handleMarkupMouseMoved(ev.Position)
		return
	}

	w.mu.Lock()
	w.mouseX = ev.Position.X
	w.mouseY = ev.Position.Y

	hit := w.hitTest(ev.Position)
	if hit >= 0 {
		w.hoverBtn = hit
	} else {
		w.hoverBtn = -1
	}

	if w.started && w.mouseDown && !w.done {
		w.curX = ev.Position.X
		w.curY = ev.Position.Y
		if w.mode == snipFreeform && len(w.freePoints) > 0 {
			last := w.freePoints[len(w.freePoints)-1]
			dx := ev.Position.X - last.X
			dy := ev.Position.Y - last.Y
			if dx*dx+dy*dy >= 9 {
				w.freePoints = append(w.freePoints, ev.Position)
			}
		}
	}
	w.mu.Unlock()
	w.Refresh()
}

// ── keyboard ─────────────────────────────────────────────────────────────────

func (w *regionOverlayWidget) FocusGained()     {}
func (w *regionOverlayWidget) FocusLost()       {}
func (w *regionOverlayWidget) TypedRune(_ rune) {}

func (w *regionOverlayWidget) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyEscape:
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

	case fyne.KeyReturn, fyne.KeyEnter:
		w.mu.Lock()
		if w.done || !w.selComplete {
			w.mu.Unlock()
			return
		}
		mode := w.mode
		sx, sy := w.startX, w.startY
		cx, cy := w.curX, w.curY
		freePoints := append([]fyne.Position(nil), w.freePoints...)
		size := w.Size()
		w.done = true
		w.mu.Unlock()

		scale := float32(1.0)
		if size.Width > 0 {
			scale = float32(w.screenW) / size.Width
		}

		var region string
		if mode == snipFreeform && len(freePoints) >= 3 {
			// Polygon-mask the bgImage directly; no second x11grab needed.
			if tmpPath, err := saveFreeformTmpFile(w.bgImage, freePoints, scale); err == nil {
				region = "file:" + tmpPath
			} else {
				// Fallback to bounding-box rectangle on error.
				region = snipComputeRegion(mode, sx, sy, cx, cy, freePoints, scale)
			}
		} else {
			region = snipComputeRegion(mode, sx, sy, cx, cy, freePoints, scale)
		}

		if w.onDone != nil {
			w.onDone(region)
		}
	}
}

// ─── selection helpers ───────────────────────────────────────────────────────

func snipSelectionValid(mode snipMode, sx, sy, cx, cy float32, freePoints []fyne.Position, size fyne.Size) bool {
	switch mode {
	case snipFreeform:
		if len(freePoints) < 2 {
			return false
		}
		bMinX, bMinY := freePoints[0].X, freePoints[0].Y
		bMaxX, bMaxY := bMinX, bMinY
		for _, p := range freePoints {
			if p.X < bMinX {
				bMinX = p.X
			}
			if p.Y < bMinY {
				bMinY = p.Y
			}
			if p.X > bMaxX {
				bMaxX = p.X
			}
			if p.Y > bMaxY {
				bMaxY = p.Y
			}
		}
		return bMaxX-bMinX >= 4 && bMaxY-bMinY >= 4
	default:
		selW := float32(math.Abs(float64(cx - sx)))
		selH := float32(math.Abs(float64(cy - sy)))
		return selW >= 4 && selH >= 4
	}
}

func snipComputeRegion(mode snipMode, sx, sy, cx, cy float32, freePoints []fyne.Position, scale float32) string {
	switch mode {
	case snipFreeform:
		if len(freePoints) < 2 {
			return ""
		}
		bMinX, bMinY := freePoints[0].X, freePoints[0].Y
		bMaxX, bMaxY := bMinX, bMinY
		for _, p := range freePoints {
			if p.X < bMinX {
				bMinX = p.X
			}
			if p.Y < bMinY {
				bMinY = p.Y
			}
			if p.X > bMaxX {
				bMaxX = p.X
			}
			if p.Y > bMaxY {
				bMaxY = p.Y
			}
		}
		selW := bMaxX - bMinX
		selH := bMaxY - bMinY
		if selW < 4 || selH < 4 {
			return ""
		}
		return fmt.Sprintf("%dx%d+%d+%d",
			int(selW*scale), int(selH*scale),
			int(bMinX*scale), int(bMinY*scale))
	default:
		minX := float32(math.Min(float64(sx), float64(cx)))
		minY := float32(math.Min(float64(sy), float64(cy)))
		maxX := float32(math.Max(float64(sx), float64(cx)))
		maxY := float32(math.Max(float64(sy), float64(cy)))
		selW := maxX - minX
		selH := maxY - minY
		if selW < 4 || selH < 4 {
			return ""
		}
		return fmt.Sprintf("%dx%d+%d+%d",
			int(selW*scale), int(selH*scale),
			int(minX*scale), int(minY*scale))
	}
}

// ─── renderer ────────────────────────────────────────────────────────────────

type regionOverlayRenderer struct {
	w       *regionOverlayWidget
	bgObj   fyne.CanvasObject
	bgImage image.Image

	dimTop, dimBot, dimLeft, dimRight *canvas.Rectangle

	selRect *canvas.Rectangle
	sizeBg  *canvas.Rectangle
	sizeText *canvas.Text

	// 8 handle dots: TL TC TR  ML MR  BL BC BR
	handles [8]*canvas.Circle

	// freeform
	freeformBuf    *image.RGBA
	freeformRaster *canvas.Raster
	freeBufW       int
	freeBufH       int
	freeLastN      int

	// help text shown before any selection
	helpMain *canvas.Text
	helpSub  *canvas.Text

	instrText *canvas.Text
	crossH    *canvas.Line
	crossV    *canvas.Line

	// magnifier
	magRaster    *canvas.Raster
	magShadow    *canvas.Rectangle
	magBorder    *canvas.Rectangle
	magCrossH    *canvas.Line
	magCrossV    *canvas.Line
	coordBg      *canvas.Rectangle
	coordText    *canvas.Text
	magCX, magCY float32
	magSX, magSY float32

	// toolbar
	toolbarBg *canvas.Rectangle
	indicator *canvas.Rectangle // animated selection indicator
	btnBg     [4]*canvas.Rectangle
	btnIcon   [4]*widget.Icon
	btnLabel  [4]*canvas.Text

	// ── Markup mode objects ──────────────────────────────────────────────
	mkBufRaster    *canvas.Raster
	mkBarBg        *canvas.Rectangle
	mkToolBg       [5]*canvas.Rectangle
	mkToolLbl      [5]*canvas.Text
	mkColorLbl     *canvas.Text
	mkColorSwatch  *canvas.Rectangle
	mkSizeLbl      *canvas.Text
	mkSizeMinBg    *canvas.Rectangle
	mkSizePlusBg   *canvas.Rectangle
	mkSizeMinLbl   *canvas.Text
	mkSizePlusLbl  *canvas.Text
	mkFillTogBg    *canvas.Rectangle
	mkFillTogLbl   *canvas.Text
	mkFillLbl      *canvas.Text
	mkFillSwatch   *canvas.Rectangle
	mkBlurBg       [2]*canvas.Rectangle
	mkBlurLbl      [2]*canvas.Text
	mkUndoBg       *canvas.Rectangle
	mkUndoLbl      *canvas.Text
	mkCaptureBg    *canvas.Rectangle
	mkCaptureLbl   *canvas.Text
	mkPalBg        *canvas.Rectangle
	mkPalSwatch    [12]*canvas.Rectangle
	mkPreviewRect  *canvas.Rectangle
	mkPreviewCircle *canvas.Circle
	mkPreviewLine  *canvas.Line
	mkLastPtN      int

	objects []fyne.CanvasObject
}

func (w *regionOverlayWidget) CreateRenderer() fyne.WidgetRenderer {
	selColor := color.NRGBA{0x1a, 0x6b, 0xc4, 0xff}
	dim := color.NRGBA{0, 0, 0, 160}

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
	selRect.StrokeColor = selColor
	selRect.StrokeWidth = 3

	sizeBg := canvas.NewRectangle(color.NRGBA{0, 0, 0, 0xaa})
	sizeBg.CornerRadius = 4

	sizeText := canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	sizeText.TextSize = 12
	sizeText.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}

	var handles [8]*canvas.Circle
	for i := range handles {
		c := canvas.NewCircle(color.NRGBA{0xff, 0xff, 0xff, 0xff})
		c.StrokeColor = selColor
		c.StrokeWidth = 1.5
		handles[i] = c
	}

	// Help text shown in center before selection starts
	helpMain := canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xee})
	helpMain.TextSize = 22
	helpMain.TextStyle = fyne.TextStyle{Bold: true}
	helpMain.Alignment = fyne.TextAlignCenter

	helpSub := canvas.NewText("Press Enter to capture  ·  ESC to cancel", color.NRGBA{0xcc, 0xcc, 0xcc, 0xcc})
	helpSub.TextSize = 14
	helpSub.Alignment = fyne.TextAlignCenter

	instrText := canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xbb})
	instrText.TextSize = 13
	instrText.Alignment = fyne.TextAlignCenter

	crossH := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x28})
	crossH.StrokeWidth = 1
	crossV := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x28})
	crossV.StrokeWidth = 1

	r := &regionOverlayRenderer{
		w:         w,
		bgObj:     bgObj,
		bgImage:   w.bgImage,
		dimTop:    dimTop,
		dimBot:    dimBot,
		dimLeft:   dimLeft,
		dimRight:  dimRight,
		selRect:   selRect,
		sizeBg:    sizeBg,
		sizeText:  sizeText,
		handles:   handles,
		helpMain:  helpMain,
		helpSub:   helpSub,
		instrText: instrText,
		crossH:    crossH,
		crossV:    crossV,
	}

	// Freeform raster
	r.freeformRaster = canvas.NewRaster(func(rw, rh int) image.Image {
		if r.freeformBuf == nil {
			return image.NewRGBA(image.Rect(0, 0, 1, 1))
		}
		return r.freeformBuf
	})

	// Magnifier raster
	r.magRaster = canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		if pw == 0 || ph == 0 {
			return color.Black
		}
		const regionW = 32.0
		const regionH = 24.0
		cx := r.magCX
		cy := r.magCY
		sx := r.magSX
		sy := r.magSY
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
		if (px/8+py/8)%2 == 0 {
			return color.NRGBA{0x44, 0x44, 0x44, 0xff}
		}
		return color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
	})
	r.magBorder = canvas.NewRectangle(color.Transparent)
	r.magBorder.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0xcc}
	r.magBorder.StrokeWidth = 1
	r.magShadow = canvas.NewRectangle(color.NRGBA{0, 0, 0, 0x99})
	r.magCrossH = canvas.NewLine(color.NRGBA{0xff, 0x44, 0x44, 0xdd})
	r.magCrossH.StrokeWidth = 1
	r.magCrossV = canvas.NewLine(color.NRGBA{0xff, 0x44, 0x44, 0xdd})
	r.magCrossV.StrokeWidth = 1
	r.coordBg = canvas.NewRectangle(color.NRGBA{0, 0, 0, 0xbb})
	r.coordText = canvas.NewText("", color.NRGBA{0x4d, 0xc1, 0xf5, 0xff})
	r.coordText.TextSize = 11
	r.coordText.TextStyle = fyne.TextStyle{Monospace: true}

	// Toolbar
	r.toolbarBg = canvas.NewRectangle(color.NRGBA{0x1c, 0x1c, 0x1c, 0xf2})
	r.toolbarBg.CornerRadius = 10
	r.toolbarBg.StrokeColor = color.NRGBA{0x44, 0x44, 0x44, 0xff}
	r.toolbarBg.StrokeWidth = 1

	// Animated indicator (slides behind button content)
	r.indicator = canvas.NewRectangle(color.NRGBA{0x2a, 0x5e, 0xc8, 0xff})
	r.indicator.CornerRadius = 7

	for i := 0; i < 4; i++ {
		bg := canvas.NewRectangle(color.Transparent)
		bg.CornerRadius = 7
		r.btnBg[i] = bg

		r.btnIcon[i] = widget.NewIcon(snipIcon(snipMode(i)))

		lbl := canvas.NewText(snipLabels[i], color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
		lbl.TextSize = 11
		lbl.Alignment = fyne.TextAlignCenter
		r.btnLabel[i] = lbl
	}

	// Init markup objects
	r.initMarkupObjects()

	// Draw order: bottom → top
	objs := []fyne.CanvasObject{bgObj}
	objs = append(objs, dimTop, dimBot, dimLeft, dimRight)
	objs = append(objs, r.freeformRaster)
	objs = append(objs, r.mkBufRaster)
	objs = append(objs, selRect)
	for _, h := range handles {
		objs = append(objs, h)
	}
	objs = append(objs, sizeBg, sizeText)
	objs = append(objs, crossH, crossV)
	objs = append(objs, helpMain, helpSub)
	objs = append(objs, instrText)
	objs = append(objs, r.magShadow, r.magRaster, r.magBorder)
	objs = append(objs, r.magCrossH, r.magCrossV)
	objs = append(objs, r.coordBg, r.coordText)
	// markup bottom toolbar
	objs = append(objs, r.mkBarBg)
	for _, b := range r.mkToolBg {
		objs = append(objs, b)
	}
	for _, l := range r.mkToolLbl {
		objs = append(objs, l)
	}
	objs = append(objs, r.mkColorLbl, r.mkColorSwatch)
	objs = append(objs, r.mkSizeMinBg, r.mkSizeMinLbl, r.mkSizeLbl, r.mkSizePlusBg, r.mkSizePlusLbl)
	objs = append(objs, r.mkFillTogBg, r.mkFillTogLbl, r.mkFillLbl, r.mkFillSwatch)
	for _, b := range r.mkBlurBg {
		objs = append(objs, b)
	}
	for _, l := range r.mkBlurLbl {
		objs = append(objs, l)
	}
	objs = append(objs, r.mkUndoBg, r.mkUndoLbl, r.mkCaptureBg, r.mkCaptureLbl)
	objs = append(objs, r.mkPalBg)
	for _, sw := range r.mkPalSwatch {
		objs = append(objs, sw)
	}
	objs = append(objs, r.mkPreviewRect, r.mkPreviewCircle, r.mkPreviewLine)
	// top toolbar (on top of everything)
	objs = append(objs, r.toolbarBg, r.indicator)
	for _, b := range r.btnBg {
		objs = append(objs, b)
	}
	for _, ic := range r.btnIcon {
		objs = append(objs, ic)
	}
	for _, l := range r.btnLabel {
		objs = append(objs, l)
	}
	r.objects = objs
	return r
}

func (r *regionOverlayRenderer) MinSize() fyne.Size           { return fyne.NewSize(320, 200) }
func (r *regionOverlayRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *regionOverlayRenderer) Destroy() {
	if r.w.modeAnim != nil {
		r.w.modeAnim.Stop()
	}
}

func (r *regionOverlayRenderer) Layout(size fyne.Size) {
	r.bgObj.Resize(size)
	r.bgObj.Move(fyne.NewPos(0, 0))

	bw := intMax(int(size.Width), 1)
	bh := intMax(int(size.Height), 1)
	if r.freeformBuf == nil || r.freeBufW != bw || r.freeBufH != bh {
		r.freeformBuf = image.NewRGBA(image.Rect(0, 0, bw, bh))
		r.freeBufW = bw
		r.freeBufH = bh
		r.freeLastN = 0
	}
	r.freeformRaster.Resize(size)
	r.freeformRaster.Move(fyne.NewPos(0, 0))

	// Initialize indicator position on first layout
	if r.w.indicX == 0 && size.Width > 0 {
		bx, _, _, _ := r.w.btnRect(int(r.w.mode))
		r.w.indicX = bx
	}

	r.paint(size)
}

func (r *regionOverlayRenderer) Refresh() {
	r.paint(r.w.Size())
}

func (r *regionOverlayRenderer) paint(size fyne.Size) {
	w := r.w
	w.mu.Lock()
	mode := w.mode
	w.mu.Unlock()

	// In markup mode: delegate to markup renderer, still draw the top toolbar.
	if mode == snipMarkup {
		r.crossH.Position1 = fyne.NewPos(0, 0)
		r.crossH.Position2 = fyne.NewPos(0, 0)
		r.crossV.Position1 = fyne.NewPos(0, 0)
		r.crossV.Position2 = fyne.NewPos(0, 0)
		r.paintMarkupMode(size)
		indicX := w.indicX
		w.mu.Lock()
		hoverBtn := w.hoverBtn
		w.mu.Unlock()
		r.paintTopToolbar(size, mode, hoverBtn, indicX)
		return
	}

	// Hide all markup-mode objects when not in markup mode
	zero := fyne.NewSize(0, 0)
	r.mkBufRaster.Resize(zero)
	r.mkBarBg.Resize(zero)
	for _, b := range r.mkToolBg {
		b.Resize(zero)
	}
	for _, l := range r.mkToolLbl {
		l.Resize(zero)
	}
	r.mkColorLbl.Resize(zero)
	r.mkColorSwatch.Resize(zero)
	r.mkSizeLbl.Resize(zero)
	r.mkSizeMinBg.Resize(zero)
	r.mkSizePlusBg.Resize(zero)
	r.mkSizeMinLbl.Resize(zero)
	r.mkSizePlusLbl.Resize(zero)
	r.mkFillTogBg.Resize(zero)
	r.mkFillTogLbl.Resize(zero)
	r.mkFillLbl.Resize(zero)
	r.mkFillSwatch.Resize(zero)
	for _, b := range r.mkBlurBg {
		b.Resize(zero)
	}
	for _, l := range r.mkBlurLbl {
		l.Resize(zero)
	}
	r.mkUndoBg.Resize(zero)
	r.mkUndoLbl.Resize(zero)
	r.mkCaptureBg.Resize(zero)
	r.mkCaptureLbl.Resize(zero)
	r.mkPalBg.Resize(zero)
	for _, sw := range r.mkPalSwatch {
		sw.Resize(zero)
	}
	r.mkPreviewRect.Resize(zero)
	r.mkPreviewCircle.Resize(zero)
	r.mkPreviewLine.Position1 = fyne.NewPos(0, 0)
	r.mkPreviewLine.Position2 = fyne.NewPos(0, 0)

	w.mu.Lock()
	started := w.started
	mouseDown := w.mouseDown
	selComplete := w.selComplete
	sx, sy := w.startX, w.startY
	cx, cy := w.curX, w.curY
	mx, my := w.mouseX, w.mouseY
	hoverBtn := w.hoverBtn
	freePoints := append([]fyne.Position(nil), w.freePoints...)
	w.mu.Unlock()

	indicX := w.indicX // safe: only written on main thread by animation

	dim := color.NRGBA{0, 0, 0, 160}
	selColor := color.NRGBA{0x1a, 0x6b, 0xc4, 0xff}

	scale := float32(1.0)
	if size.Width > 0 {
		scale = float32(w.screenW) / size.Width
	}

	// Screen crosshair
	r.crossH.Position1 = fyne.NewPos(0, my)
	r.crossH.Position2 = fyne.NewPos(size.Width, my)
	r.crossV.Position1 = fyne.NewPos(mx, 0)
	r.crossV.Position2 = fyne.NewPos(mx, size.Height)

	// ── Instruction bar and help text ─────────────────────────────────────────
	tbBottom := tbTopOff + tbPanelH() + 4

	switch {
	case !started:
		// Pre-selection: show large centered instructions
		switch mode {
		case snipFreeform:
			r.helpMain.Text = "Click and drag to draw your selection"
		default:
			r.helpMain.Text = "Click and drag to select an area"
		}
		r.helpMain.Move(fyne.NewPos(0, size.Height/2-36))
		r.helpMain.Resize(fyne.NewSize(size.Width, 28))
		r.helpSub.Move(fyne.NewPos(0, size.Height/2))
		r.helpSub.Resize(fyne.NewSize(size.Width, 20))
		canvas.Refresh(r.helpMain)
		canvas.Refresh(r.helpSub)
		r.helpMain.Show()
		r.helpSub.Show()
		r.instrText.Text = "ESC to cancel"
	case selComplete:
		r.helpMain.Hide()
		r.helpSub.Hide()
		r.instrText.Text = "Press Enter to capture  ·  Click to redo selection  ·  ESC to cancel"
	default:
		r.helpMain.Hide()
		r.helpSub.Hide()
		if mode == snipFreeform {
			r.instrText.Text = "Release to finish drawing  ·  ESC to cancel"
		} else {
			r.instrText.Text = "Drag to select  ·  ESC to cancel"
		}
	}
	r.instrText.Move(fyne.NewPos(0, size.Height-34))
	r.instrText.Resize(fyne.NewSize(size.Width, 22))

	// ── Rectangular selection ─────────────────────────────────────────────────
	r.freeformRaster.Hide()

	if mode == snipRect {
		minX := float32(math.Min(float64(sx), float64(cx)))
		minY := float32(math.Min(float64(sy), float64(cy)))
		maxX := float32(math.Max(float64(sx), float64(cx)))
		maxY := float32(math.Max(float64(sy), float64(cy)))
		selW := maxX - minX
		selH := maxY - minY

		hasSelection := started && (selW >= 1 || selH >= 1)

		if !hasSelection {
			r.dimTop.FillColor = dim
			r.dimTop.Move(fyne.NewPos(0, 0))
			r.dimTop.Resize(size)
			r.dimBot.Resize(zero)
			r.dimLeft.Resize(zero)
			r.dimRight.Resize(zero)
			r.selRect.Resize(zero)
			r.sizeBg.Resize(zero)
			r.sizeText.Text = ""
			for i := range r.handles {
				r.handles[i].Resize(zero)
			}
		} else {
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

			r.selRect.Move(fyne.NewPos(minX, minY))
			r.selRect.Resize(fyne.NewSize(selW, selH))

			// Dimension label with dark pill background
			dimStr := fmt.Sprintf(" %d × %d ", int(selW*scale), int(selH*scale))
			r.sizeText.Text = dimStr
			const lblH = float32(20)
			const lblW = float32(120)
			lblY := minY - lblH - 4
			if lblY < tbBottom {
				lblY = maxY + 4
			}
			lblX := minX
			if lblX+lblW > size.Width {
				lblX = size.Width - lblW
			}
			r.sizeBg.Move(fyne.NewPos(lblX, lblY))
			r.sizeBg.Resize(fyne.NewSize(lblW, lblH))
			r.sizeText.Move(fyne.NewPos(lblX, lblY+2))
			r.sizeText.Resize(fyne.NewSize(lblW, lblH-2))
			canvas.Refresh(r.sizeText)

			// Handle dots: TL TC TR  ML MR  BL BC BR
			const hR = float32(5)
			const hD = hR * 2
			midX := minX + selW/2
			midY := minY + selH/2
			positions := [8][2]float32{
				{minX, minY}, {midX, minY}, {maxX, minY},
				{minX, midY}, {maxX, midY},
				{minX, maxY}, {midX, maxY}, {maxX, maxY},
			}
			for i, pos := range positions {
				r.handles[i].FillColor = color.NRGBA{0xff, 0xff, 0xff, 0xff}
				r.handles[i].StrokeColor = selColor
				r.handles[i].Move(fyne.NewPos(pos[0]-hR, pos[1]-hR))
				r.handles[i].Resize(fyne.NewSize(hD, hD))
			}
		}
	} else {
		// Other modes: dim full screen, no rect/handles
		r.dimTop.FillColor = dim
		r.dimTop.Move(fyne.NewPos(0, 0))
		r.dimTop.Resize(size)
		r.dimBot.Resize(zero)
		r.dimLeft.Resize(zero)
		r.dimRight.Resize(zero)
		r.selRect.Resize(zero)
		r.sizeBg.Resize(zero)
		r.sizeText.Text = ""
		for i := range r.handles {
			r.handles[i].Resize(zero)
		}
	}

	// ── Freeform selection ────────────────────────────────────────────────────
	if mode == snipFreeform {
		r.freeformRaster.Show()

		// Detect path reset
		if len(freePoints) < r.freeLastN && r.freeformBuf != nil {
			clearRGBA(r.freeformBuf)
			r.freeLastN = 0
		}
		if !started && r.freeLastN > 0 && r.freeformBuf != nil {
			clearRGBA(r.freeformBuf)
			r.freeLastN = 0
		}

		freeCol := color.NRGBA{0x2a, 0x7f, 0xff, 0xee}
		if r.freeformBuf != nil {
			for r.freeLastN+1 < len(freePoints) {
				p1 := freePoints[r.freeLastN]
				p2 := freePoints[r.freeLastN+1]
				drawThickLine(r.freeformBuf,
					int(p1.X), int(p1.Y), int(p2.X), int(p2.Y), freeCol, 3)
				r.freeLastN++
			}
		}
		_ = mouseDown
		r.freeformRaster.Refresh()

		// Bounding box dimensions
		if len(freePoints) >= 2 {
			bMinX, bMinY := freePoints[0].X, freePoints[0].Y
			bMaxX, bMaxY := bMinX, bMinY
			for _, p := range freePoints {
				if p.X < bMinX {
					bMinX = p.X
				}
				if p.Y < bMinY {
					bMinY = p.Y
				}
				if p.X > bMaxX {
					bMaxX = p.X
				}
				if p.Y > bMaxY {
					bMaxY = p.Y
				}
			}
			selW := bMaxX - bMinX
			selH := bMaxY - bMinY
			dimStr := fmt.Sprintf(" %d × %d ", int(selW*scale), int(selH*scale))
			r.sizeText.Text = dimStr
			const lblH = float32(20)
			const lblW = float32(120)
			lblY := bMinY - lblH - 4
			if lblY < tbBottom {
				lblY = bMaxY + 4
			}
			lblX := bMinX
			if lblX+lblW > size.Width {
				lblX = size.Width - lblW
			}
			r.sizeBg.Move(fyne.NewPos(lblX, lblY))
			r.sizeBg.Resize(fyne.NewSize(lblW, lblH))
			r.sizeText.Move(fyne.NewPos(lblX, lblY+2))
			r.sizeText.Resize(fyne.NewSize(lblW, lblH-2))
			canvas.Refresh(r.sizeText)
		}
	} else if r.freeLastN > 0 && r.freeformBuf != nil {
		clearRGBA(r.freeformBuf)
		r.freeLastN = 0
	}

	// ── Magnifier ─────────────────────────────────────────────────────────────
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
	const magOff = float32(22)

	magX := mx + magOff
	magY := my + magOff
	tbBot := tbTopOff + tbPanelH() + 8
	if magY < tbBot {
		magY = tbBot
	}
	if magX+magW > size.Width-4 {
		magX = mx - magOff - magW
	}
	if magY+magH+coordH+4 > size.Height {
		magY = my - magOff - magH - coordH - 4
	}
	if magX < 0 {
		magX = 0
	}
	if magY < 0 {
		magY = 0
	}

	r.magShadow.Move(fyne.NewPos(magX-2, magY-2))
	r.magShadow.Resize(fyne.NewSize(magW+4, magH+4))
	r.magRaster.Move(fyne.NewPos(magX, magY))
	r.magRaster.Resize(fyne.NewSize(magW, magH))
	r.magRaster.Refresh()
	r.magBorder.Move(fyne.NewPos(magX-1, magY-1))
	r.magBorder.Resize(fyne.NewSize(magW+2, magH+2))
	r.magCrossH.Position1 = fyne.NewPos(magX, magY+magH/2)
	r.magCrossH.Position2 = fyne.NewPos(magX+magW, magY+magH/2)
	r.magCrossV.Position1 = fyne.NewPos(magX+magW/2, magY)
	r.magCrossV.Position2 = fyne.NewPos(magX+magW/2, magY+magH)
	r.coordText.Text = fmt.Sprintf("  X:%-6dY:%-6d", int(mx*scale), int(my*scale))
	coordY := magY + magH + 2
	r.coordBg.Move(fyne.NewPos(magX-1, coordY-1))
	r.coordBg.Resize(fyne.NewSize(magW+2, coordH+2))
	r.coordText.Move(fyne.NewPos(magX+2, coordY))
	r.coordText.Resize(fyne.NewSize(magW, coordH))

	// ── Toolbar ───────────────────────────────────────────────────────────────
	r.paintTopToolbar(size, mode, hoverBtn, indicX)
}

func (r *regionOverlayRenderer) paintTopToolbar(size fyne.Size, mode snipMode, hoverBtn int, indicX float32) {
	w := r.w
	panelX, panelY := w.toolbarPanelXY()
	r.toolbarBg.Move(fyne.NewPos(panelX, panelY))
	r.toolbarBg.Resize(fyne.NewSize(tbPanelW(), tbPanelH()))

	// Sliding indicator
	_, by, bw2, bh2 := w.btnRect(0) // y/size same for all buttons
	r.indicator.Move(fyne.NewPos(indicX, by))
	r.indicator.Resize(fyne.NewSize(bw2, bh2))

	// Icon + label layout inside each button
	const iconSz = float32(20)
	const lblH2 = float32(14)
	const gap = float32(3)
	totalContent := iconSz + gap + lblH2
	iconTopOff := (bh2 - totalContent) / 2

	for i := 0; i < 4; i++ {
		bx, by2, bw3, bh3 := w.btnRect(i)
		r.btnBg[i].Move(fyne.NewPos(bx, by2))
		r.btnBg[i].Resize(fyne.NewSize(bw3, bh3))

		isSelected := mode == snipMode(i)
		isHover := hoverBtn == i

		if isHover && !isSelected {
			r.btnBg[i].FillColor = color.NRGBA{0x38, 0x38, 0x38, 0xff}
		} else {
			r.btnBg[i].FillColor = color.Transparent
		}
		r.btnBg[i].StrokeWidth = 0

		// Icon
		r.btnIcon[i].Move(fyne.NewPos(bx+bw3/2-iconSz/2, by2+iconTopOff))
		r.btnIcon[i].Resize(fyne.NewSize(iconSz, iconSz))

		// Label color
		switch {
		case isSelected:
			r.btnLabel[i].Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			r.btnLabel[i].TextStyle = fyne.TextStyle{Bold: true}
		default:
			r.btnLabel[i].Color = color.NRGBA{0xcc, 0xcc, 0xcc, 0xff}
			r.btnLabel[i].TextStyle = fyne.TextStyle{}
		}

		lblY := by2 + iconTopOff + iconSz + gap
		r.btnLabel[i].Move(fyne.NewPos(bx, lblY))
		r.btnLabel[i].Resize(fyne.NewSize(bw3, lblH2))
	}
}

// ─── image helpers ───────────────────────────────────────────────────────────

func clearRGBA(img *image.RGBA) {
	for i := range img.Pix {
		img.Pix[i] = 0
	}
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func drawThickLine(img *image.RGBA, x0, y0, x1, y1 int, c color.NRGBA, thickness int) {
	dx := x1 - x0
	dy := y1 - y0
	adx := dx
	if adx < 0 {
		adx = -adx
	}
	ady := dy
	if ady < 0 {
		ady = -ady
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}
	err := adx - ady
	r := thickness / 2
	bounds := img.Bounds()

	for {
		for py := y0 - r; py <= y0+r; py++ {
			for px := x0 - r; px <= x0+r; px++ {
				ddx := px - x0
				ddy := py - y0
				if ddx*ddx+ddy*ddy <= r*r+r {
					if px >= bounds.Min.X && px < bounds.Max.X &&
						py >= bounds.Min.Y && py < bounds.Max.Y {
						img.Set(px, py, c)
					}
				}
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -ady {
			err -= ady
			x0 += sx
		}
		if e2 < adx {
			err += adx
			y0 += sy
		}
	}
}

// ─── process helpers ─────────────────────────────────────────────────────────

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
			return "", nil
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

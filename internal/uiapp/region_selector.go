package uiapp

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type regionSelector struct {
	app        fyne.App
	win        fyne.Window
	onSelected func(region string) // WxH+X+Y format
	onCancel   func()

	startX, startY     float32
	currentX, currentY float32
	dragging           bool
	label              *widget.Label
	selectionRect      *canvas.Rectangle
	bgImage            *canvas.Image
	screenshotPath     string
}

func getScreenSize() (int, int) {
	if runtime.GOOS == "linux" {
		// Try xdpyinfo first
		if out, err := exec.Command("xdpyinfo").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				var w, h int
				if n, _ := fmt.Sscanf(strings.TrimSpace(line), "dimensions: %dx%d", &w, &h); n == 2 {
					if w > 0 && h > 0 {
						return w, h
					}
				}
			}
		}
		// Fallback to xrandr
		if out, err := exec.Command("xrandr").Output(); err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				if strings.Contains(line, " connected ") && strings.Contains(line, "+0+0") {
					var w, h int
					if n, _ := fmt.Sscanf(line, " %dx%d", &w, &h); n == 2 {
						if w > 0 && h > 0 {
							return w, h
						}
					}
				}
			}
		}
	}
	return 1920, 1080 // fallback
}

func takeScreenshot() (string, error) {
	tmpDir := os.TempDir()
	screenshotPath := filepath.Join(tmpDir, fmt.Sprintf("swiftcap_selector_%d.png", time.Now().UnixNano()))

	w, h := getScreenSize()
	videoSize := fmt.Sprintf("%dx%d", w, h)

	// Use import (ImageMagick) or maim/scrot if available, otherwise ffmpeg
	var cmd *exec.Cmd
	if _, err := exec.LookPath("import"); err == nil {
		// ImageMagick import
		cmd = exec.Command("import", "-window", "root", screenshotPath)
	} else if _, err := exec.LookPath("maim"); err == nil {
		// maim
		cmd = exec.Command("maim", screenshotPath)
	} else if _, err := exec.LookPath("scrot"); err == nil {
		// scrot
		cmd = exec.Command("scrot", screenshotPath)
	} else {
		// Fallback to ffmpeg
		display := os.Getenv("DISPLAY")
		if display == "" {
			display = ":0"
		}
		cmd = exec.Command("ffmpeg", "-y", "-f", "x11grab", "-video_size", videoSize, "-i", display, "-vframes", "1", screenshotPath)
	}

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to take screenshot: %w", err)
	}

	return screenshotPath, nil
}

func newRegionSelector(a fyne.App, onSelected func(string), onCancel func()) *regionSelector {
	rs := &regionSelector{
		app:        a,
		onSelected: onSelected,
		onCancel:   onCancel,
	}

	win := a.NewWindow("Select Recording Region")

	screenWidth, screenHeight := getScreenSize()
	winSize := fyne.NewSize(float32(screenWidth), float32(screenHeight))

	win.Resize(winSize)
	win.SetFixedSize(false)
	win.SetFullScreen(true)
	win.CenterOnScreen()

	// Take screenshot for background
	screenshotPath, err := takeScreenshot()
	if err == nil {
		rs.screenshotPath = screenshotPath
		rs.bgImage = canvas.NewImageFromFile(screenshotPath)
		rs.bgImage.FillMode = canvas.ImageFillContain
		rs.bgImage.Resize(winSize)
		rs.bgImage.Move(fyne.NewPos(0, 0))
	} else {
		rs.bgImage = nil
	}

	// Semi-transparent dark overlay on top of screenshot
	overlayRect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 80})
	overlayRect.Resize(winSize)
	overlayRect.Move(fyne.NewPos(0, 0))

	// Selection rectangle (initially hidden)
	rs.selectionRect = canvas.NewRectangle(color.NRGBA{255, 255, 255, 0})
	rs.selectionRect.StrokeColor = color.NRGBA{0, 150, 255, 255}
	rs.selectionRect.StrokeWidth = 3
	rs.selectionRect.FillColor = color.NRGBA{0, 150, 255, 30}
	rs.selectionRect.Hide()

	label := widget.NewLabel("Click and drag to select region, or close window to cancel")
	label.Alignment = fyne.TextAlignCenter
	label.Move(fyne.NewPos(float32(screenWidth)/2-200, 20))
	rs.label = label

	// Create a simple transparent widget that handles all mouse events
	transparentOverlay := newMouseCaptureWidget(rs, winSize)
	
	// Create base content
	var baseContent fyne.CanvasObject
	if rs.bgImage != nil {
		baseContent = container.NewWithoutLayout(
			rs.bgImage,
			overlayRect,
			rs.selectionRect,
			label,
		)
	} else {
		baseContent = container.NewWithoutLayout(
			overlayRect,
			rs.selectionRect,
			label,
		)
	}
	
	// Use Max container to ensure overlay widget covers everything and is on top
	content := container.NewMax(
		baseContent,
		transparentOverlay, // This will cover everything and receive all mouse events
	)
	
	win.SetContent(content)

	win.SetOnClosed(func() {
		if rs.screenshotPath != "" {
			os.Remove(rs.screenshotPath)
		}
		if rs.onCancel != nil {
			rs.onCancel()
		}
	})

	rs.win = win
	return rs
}

// mouseCaptureWidget is a simple widget that captures all mouse events
type mouseCaptureWidget struct {
	widget.BaseWidget
	selector *regionSelector
	rect     *canvas.Rectangle
}

func newMouseCaptureWidget(rs *regionSelector, size fyne.Size) *mouseCaptureWidget {
	w := &mouseCaptureWidget{
		selector: rs,
		rect:     canvas.NewRectangle(color.NRGBA{0, 0, 0, 1}), // Nearly transparent
	}
	w.ExtendBaseWidget(w)
	w.rect.Resize(size)
	w.rect.Move(fyne.NewPos(0, 0))
	return w
}

func (w *mouseCaptureWidget) CreateRenderer() fyne.WidgetRenderer {
	return &mouseCaptureRenderer{
		widget: w,
		rect:   w.rect,
	}
}

type mouseCaptureRenderer struct {
	widget *mouseCaptureWidget
	rect   *canvas.Rectangle
}

func (r *mouseCaptureRenderer) Layout(size fyne.Size) {
	r.rect.Resize(size)
}

func (r *mouseCaptureRenderer) MinSize() fyne.Size {
	return fyne.NewSize(1, 1)
}

func (r *mouseCaptureRenderer) Refresh() {
	r.rect.Refresh()
}

func (r *mouseCaptureRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rect}
}

func (r *mouseCaptureRenderer) Destroy() {}

func (w *mouseCaptureWidget) MouseDown(ev *fyne.PointEvent) {
	w.selector.startX = ev.Position.X
	w.selector.startY = ev.Position.Y
	w.selector.currentX = ev.Position.X
	w.selector.currentY = ev.Position.Y
	w.selector.dragging = true
	w.selector.updateSelection()
}

func (w *mouseCaptureWidget) MouseDrag(ev *fyne.DragEvent) {
	if w.selector.dragging {
		w.selector.currentX = ev.Position.X
		w.selector.currentY = ev.Position.Y
		w.selector.updateSelection()
	}
}

func (w *mouseCaptureWidget) MouseUp(ev *fyne.PointEvent) {
	if w.selector.dragging {
		w.selector.dragging = false
		w.selector.finishSelection()
	}
}

func (rs *regionSelector) Show() {
	rs.win.Show()
	rs.win.RequestFocus()
}

func (rs *regionSelector) updateSelection() {
	if !rs.dragging {
		return
	}

	winSize := rs.win.Canvas().Size()
	x1 := math.Min(float64(rs.startX), float64(rs.currentX))
	y1 := math.Min(float64(rs.startY), float64(rs.currentY))
	x2 := math.Max(float64(rs.startX), float64(rs.currentX))
	y2 := math.Max(float64(rs.startY), float64(rs.currentY))

	// Clamp to window bounds
	x1 = math.Max(0, math.Min(x1, float64(winSize.Width)))
	y1 = math.Max(0, math.Min(y1, float64(winSize.Height)))
	x2 = math.Max(0, math.Min(x2, float64(winSize.Width)))
	y2 = math.Max(0, math.Min(y2, float64(winSize.Height)))

	w := x2 - x1
	h := y2 - y1

	if w < 10 || h < 10 {
		rs.label.SetText("Drag to select a larger region")
		rs.selectionRect.Hide()
		rs.selectionRect.Refresh()
		return
	}

	// Update selection rectangle position and size
	rs.selectionRect.Move(fyne.NewPos(float32(x1), float32(y1)))
	rs.selectionRect.Resize(fyne.NewSize(float32(w), float32(h)))
	rs.selectionRect.Show()
	rs.selectionRect.Refresh()

	rs.label.SetText(fmt.Sprintf("Region: %.0fx%.0f at (%.0f, %.0f) - Release to confirm", w, h, x1, y1))
}

func (rs *regionSelector) finishSelection() {
	winSize := rs.win.Canvas().Size()
	x1 := math.Min(float64(rs.startX), float64(rs.currentX))
	y1 := math.Min(float64(rs.startY), float64(rs.currentY))
	x2 := math.Max(float64(rs.startX), float64(rs.currentX))
	y2 := math.Max(float64(rs.startY), float64(rs.currentY))

	// Clamp to window bounds
	x1 = math.Max(0, math.Min(x1, float64(winSize.Width)))
	y1 = math.Max(0, math.Min(y1, float64(winSize.Height)))
	x2 = math.Max(0, math.Min(x2, float64(winSize.Width)))
	y2 = math.Max(0, math.Min(y2, float64(winSize.Height)))

	w := int(x2 - x1)
	h := int(y2 - y1)

	if w < 10 || h < 10 {
		rs.label.SetText("Selection too small, try again")
		return
	}

	region := fmt.Sprintf("%dx%d+%d+%d", w, h, int(x1), int(y1))

	if rs.screenshotPath != "" {
		os.Remove(rs.screenshotPath)
	}

	rs.win.Close()
	if rs.onSelected != nil {
		rs.onSelected(region)
	}
}

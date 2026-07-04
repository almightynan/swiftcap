package uiapp

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
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

// ─── public handle ────────────────────────────────────────────────────────────

type toastHandle struct {
	modal *previewModal
}

func (t *toastHandle) close() {
	if t != nil && t.modal != nil {
		t.modal.close()
	}
}

// showPreviewModal opens the preview modal immediately, then loads the preview
// image asynchronously so there is no visible delay before the card appears.
func (ui *RecordingUI) showPreviewModal(path string, isScreenshot bool) {
	title := "Screenshot saved"
	if !isScreenshot {
		title = "Recording saved"
	}

	ui.runOnMain(func() {
		if ui.mainWin == nil {
			return
		}
		if ui.toast != nil {
			ui.toast.close()
			ui.toast = nil
		}

		bw := &previewModalWidget{
			title: title,
			path:  path,
		}
		bw.onOpenFile = func() {
			if err := openFile(path); err != nil {
				ui.showError("Open File", err.Error())
			}
		}
		bw.onOpenFolder = func() {
			if err := openFolder(path); err != nil {
				ui.showError("Open Folder", err.Error())
			}
		}
		bw.ExtendBaseWidget(bw)

		// A true in-app modal: rendered into the main window's canvas overlay
		// stack, not a separate OS window. The overlay stack sizes the widget to
		// fill the canvas, so the widget draws its own full-canvas dim layer and
		// centers the card within it — meaning the modal is clipped to the app and
		// moves with it, instead of floating free on the screen.
		cv := ui.mainWin.Canvas()
		m := &previewModal{
			canvas: cv,
			bw:     bw,
		}
		m.onClosed = func() {
			ui.runOnMain(func() {
				if ui.toast != nil && ui.toast.modal == m {
					ui.toast = nil
				}
			})
		}
		bw.modal = m
		ui.toast = &toastHandle{modal: m}

		// "Take another screenshot" is only wired up in screenshot mode.
		if isScreenshot {
			bw.onRetake = func() {
				m.afterClose = func() {
					go ui.handleScreenshot()
				}
				m.close()
			}
		}

		cv.Overlays().Add(bw)
		// Overlays are auto-resized to the canvas on window resize, but NOT at Add
		// time (only internal PopUp containers get that) — so size it to fill the
		// canvas now. Layout centers the card within this size.
		bw.Resize(cv.Size())
		cv.Focus(bw)

		openAnim := fyne.NewAnimation(220*time.Millisecond, func(t float32) {
			bw.animT = t
			bw.Refresh()
		})
		openAnim.Curve = fyne.AnimationEaseOut
		m.anim = openAnim
		openAnim.Start()

		// Load the preview image in the background so the card appears instantly.
		go func() {
			var img image.Image
			if isScreenshot {
				img = loadAnyImage(path)
			} else {
				img = extractVideoThumb(path)
			}
			if img == nil {
				img = image.NewRGBA(image.Rect(0, 0, 1, 1))
			}
			ui.runOnMain(func() {
				bw.mu.Lock()
				bw.previewImg = img
				bw.previewLoaded = true
				bw.mu.Unlock()
				bw.Refresh()
			})
		}()
	})
}

// ─── previewModal controller ──────────────────────────────────────────────────

type previewModal struct {
	canvas     fyne.Canvas
	bw         *previewModalWidget
	once       sync.Once
	anim       *fyne.Animation
	onClosed   func()
	afterClose func() // called after the close animation; used by the retake flow
}

func (m *previewModal) close() {
	m.once.Do(func() {
		if m.anim != nil {
			m.anim.Stop()
		}
		startT := m.bw.animT
		closeAnim := fyne.NewAnimation(180*time.Millisecond, func(t float32) {
			m.bw.animT = startT * (1 - t)
			m.bw.Refresh()
			if t >= 1.0 {
				m.canvas.Overlays().Remove(m.bw)
				if m.onClosed != nil {
					m.onClosed()
				}
				if m.afterClose != nil {
					m.afterClose()
				}
			}
		})
		closeAnim.Curve = fyne.AnimationEaseIn
		m.anim = closeAnim
		closeAnim.Start()
	})
}

// ─── previewModalWidget ───────────────────────────────────────────────────────

type previewModalWidget struct {
	widget.BaseWidget

	modal        *previewModal
	title        string
	path         string
	onOpenFile   func()
	onOpenFolder func()
	onRetake     func() // nil for recording mode

	animT float32 // 0=hidden → 1=fully visible

	mu            sync.Mutex
	previewImg    image.Image
	previewLoaded bool
	hoverClose    bool
	hoverRetake   bool
	hoverFileMain bool
	hoverCaret    bool
	dropdownOpen  bool
	hoverDropdown bool

	// Hit regions in canvas-local coordinates (set by renderer Layout).
	cardX1, cardY1, cardX2, cardY2                 float32 // whole card, for click-outside detection
	closeX1, closeY1, closeX2, closeY2             float32
	retakeX1, retakeY1, retakeX2, retakeY2         float32
	fileMainX1, fileMainY1, fileMainX2, fileMainY2 float32
	caretX1, caretY1, caretX2, caretY2             float32
	dropX1, dropY1, dropX2, dropY2                 float32
}

func (w *previewModalWidget) CreateRenderer() fyne.WidgetRenderer {
	// Full-canvas dim layer behind the card — this is what visually separates the
	// modal from the app underneath and captures clicks outside the card.
	dim := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0x00})

	card := canvas.NewRectangle(color.NRGBA{0x22, 0x22, 0x22, 0xff})
	card.CornerRadius = 14
	card.StrokeColor = color.NRGBA{0x3e, 0x3e, 0x3e, 0xff}
	card.StrokeWidth = 1

	titleText := canvas.NewText("", color.NRGBA{0xee, 0xee, 0xee, 0xff})
	titleText.TextSize = 15
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	// Close button — plain circle, no countdown arc.
	closeBg := canvas.NewRectangle(color.NRGBA{0x44, 0x44, 0x44, 0xaa})
	closeBg.CornerRadius = pmCloseR
	closeXLbl := canvas.NewText("×", color.NRGBA{0xff, 0xff, 0xff, 0x99})
	closeXLbl.TextSize = 18
	closeXLbl.TextStyle = fyne.TextStyle{Bold: true}
	closeXLbl.Alignment = fyne.TextAlignCenter

	prevImg := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	prevImg.FillMode = canvas.ImageFillContain

	loadingLbl := canvas.NewText("Loading preview…", color.NRGBA{0x66, 0x66, 0x66, 0xff})
	loadingLbl.TextSize = 13
	loadingLbl.Alignment = fyne.TextAlignCenter

	pathBg := canvas.NewRectangle(color.NRGBA{0x14, 0x14, 0x14, 0xff})
	pathText := canvas.NewText("", color.NRGBA{0x88, 0x88, 0x88, 0xff})
	pathText.TextSize = 11
	pathText.TextStyle = fyne.TextStyle{Monospace: true}
	pathText.Alignment = fyne.TextAlignCenter

	// Path bar folder icon (leads the path text).
	pathIcon := widget.NewIcon(theme.FolderIcon())

	// "Take another screenshot" button (hidden when onRetake is nil).
	retakeBg := canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	retakeBg.CornerRadius = 8
	retakeBg.StrokeColor = color.NRGBA{0x48, 0x48, 0x48, 0xff}
	retakeBg.StrokeWidth = 1
	retakeIcon := widget.NewIcon(theme.ViewRefreshIcon())
	retakeLbl := canvas.NewText("Take another screenshot", color.NRGBA{0xdd, 0xdd, 0xdd, 0xff})
	retakeLbl.TextSize = 13
	retakeLbl.TextStyle = fyne.TextStyle{Bold: true}

	// "Open File" button — shared background for main area + caret section.
	fileBg := canvas.NewRectangle(color.NRGBA{0x16, 0x70, 0xe8, 0xff})
	fileBg.CornerRadius = 8
	fileLbl := canvas.NewText("Open File", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	fileLbl.TextSize = 13
	fileLbl.TextStyle = fyne.TextStyle{Bold: true}
	fileIcon := widget.NewIcon(theme.FileIcon())

	// Thin separator + down-caret icon for the dropdown trigger.
	caretSep := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x38})
	caretSep.StrokeWidth = 1
	caretIcon := widget.NewIcon(theme.MenuDropDownIcon())

	// Dropdown panel for "Open Folder".
	dropBg := canvas.NewRectangle(color.NRGBA{0x2c, 0x2c, 0x2c, 0xff})
	dropBg.CornerRadius = 8
	dropBg.StrokeColor = color.NRGBA{0x50, 0x50, 0x50, 0xff}
	dropBg.StrokeWidth = 1
	dropFolderIcon := widget.NewIcon(theme.FolderOpenIcon())
	dropFolderLbl := canvas.NewText("Open Folder", color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
	dropFolderLbl.TextSize = 13

	r := &previewModalRenderer{
		w:              w,
		dim:            dim,
		card:           card,
		titleText:      titleText,
		closeBg:        closeBg,
		closeXLbl:      closeXLbl,
		prevImg:        prevImg,
		loadingLbl:     loadingLbl,
		pathBg:         pathBg,
		pathIcon:       pathIcon,
		pathText:       pathText,
		retakeBg:       retakeBg,
		retakeIcon:     retakeIcon,
		retakeLbl:      retakeLbl,
		fileBg:         fileBg,
		fileLbl:        fileLbl,
		fileIcon:       fileIcon,
		caretSep:       caretSep,
		caretIcon:      caretIcon,
		dropBg:         dropBg,
		dropFolderIcon: dropFolderIcon,
		dropFolderLbl:  dropFolderLbl,
	}
	r.objs = []fyne.CanvasObject{
		dim,
		card,
		prevImg, loadingLbl,
		pathBg, pathIcon, pathText,
		retakeBg, retakeIcon, retakeLbl,
		fileBg, fileIcon, fileLbl,
		caretSep, caretIcon,
		// Dropdown renders above buttons and path bar.
		dropBg, dropFolderIcon, dropFolderLbl,
		titleText,
		closeBg, closeXLbl,
	}
	return r
}

// MinSize is a floor only — the overlay stack stretches this widget to fill the
// canvas, and Layout centers the card within whatever size it's given.
func (w *previewModalWidget) MinSize() fyne.Size { return fyne.NewSize(pmCardW, pmCardH) }

func (w *previewModalWidget) Tapped(ev *fyne.PointEvent) {
	p := ev.Position

	// Dropdown intercepts all taps: folder item → open, anything else → dismiss.
	if w.dropdownOpen {
		if p.X >= w.dropX1 && p.X <= w.dropX2 && p.Y >= w.dropY1 && p.Y <= w.dropY2 {
			if w.onOpenFolder != nil {
				w.onOpenFolder()
			}
		}
		w.dropdownOpen = false
		w.Refresh()
		return
	}

	// Click outside the card dismisses the modal (standard modal behavior).
	if p.X < w.cardX1 || p.X > w.cardX2 || p.Y < w.cardY1 || p.Y > w.cardY2 {
		if w.modal != nil {
			w.modal.close()
		}
		return
	}

	if p.X >= w.closeX1 && p.X <= w.closeX2 && p.Y >= w.closeY1 && p.Y <= w.closeY2 {
		if w.modal != nil {
			w.modal.close()
		}
		return
	}
	if w.onRetake != nil &&
		p.X >= w.retakeX1 && p.X <= w.retakeX2 && p.Y >= w.retakeY1 && p.Y <= w.retakeY2 {
		w.onRetake()
		return
	}
	if p.X >= w.fileMainX1 && p.X <= w.fileMainX2 && p.Y >= w.fileMainY1 && p.Y <= w.fileMainY2 {
		if w.onOpenFile != nil {
			w.onOpenFile()
		}
		return
	}
	if p.X >= w.caretX1 && p.X <= w.caretX2 && p.Y >= w.caretY1 && p.Y <= w.caretY2 {
		w.dropdownOpen = !w.dropdownOpen
		w.Refresh()
	}
}
func (w *previewModalWidget) TappedSecondary(*fyne.PointEvent) {}

func (w *previewModalWidget) MouseMoved(ev *desktop.MouseEvent) {
	p := ev.Position
	hc   := p.X >= w.closeX1 && p.X <= w.closeX2 && p.Y >= w.closeY1 && p.Y <= w.closeY2
	hr   := w.onRetake != nil && p.X >= w.retakeX1 && p.X <= w.retakeX2 && p.Y >= w.retakeY1 && p.Y <= w.retakeY2
	hfm  := p.X >= w.fileMainX1 && p.X <= w.fileMainX2 && p.Y >= w.fileMainY1 && p.Y <= w.fileMainY2
	hcar := p.X >= w.caretX1 && p.X <= w.caretX2 && p.Y >= w.caretY1 && p.Y <= w.caretY2
	hdrop := w.dropdownOpen && p.X >= w.dropX1 && p.X <= w.dropX2 && p.Y >= w.dropY1 && p.Y <= w.dropY2

	w.mu.Lock()
	changed := hc != w.hoverClose || hr != w.hoverRetake || hfm != w.hoverFileMain ||
		hcar != w.hoverCaret || hdrop != w.hoverDropdown
	w.hoverClose, w.hoverRetake, w.hoverFileMain, w.hoverCaret, w.hoverDropdown = hc, hr, hfm, hcar, hdrop
	w.mu.Unlock()
	if changed {
		w.Refresh()
	}
}
func (w *previewModalWidget) MouseIn(_ *desktop.MouseEvent) {}
func (w *previewModalWidget) MouseOut()                     {}

// Cursor shows a pointer while hovering any of the modal's interactive regions
// (its buttons are custom-drawn, so there's no widget.Button to do this).
func (w *previewModalWidget) Cursor() desktop.Cursor {
	w.mu.Lock()
	over := w.hoverClose || w.hoverRetake || w.hoverFileMain || w.hoverCaret || w.hoverDropdown
	w.mu.Unlock()
	if over {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (w *previewModalWidget) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape {
		if w.dropdownOpen {
			w.dropdownOpen = false
			w.Refresh()
			return
		}
		if w.modal != nil {
			w.modal.close()
		}
	}
}
func (w *previewModalWidget) FocusGained()     {}
func (w *previewModalWidget) FocusLost()       {}
func (w *previewModalWidget) TypedRune(_ rune) {}

// ─── renderer ─────────────────────────────────────────────────────────────────

type previewModalRenderer struct {
	w              *previewModalWidget
	dim            *canvas.Rectangle
	card           *canvas.Rectangle
	titleText      *canvas.Text
	closeBg        *canvas.Rectangle
	closeXLbl      *canvas.Text
	prevImg        *canvas.Image
	loadingLbl     *canvas.Text
	pathBg         *canvas.Rectangle
	pathIcon       *widget.Icon
	pathText       *canvas.Text
	retakeBg       *canvas.Rectangle
	retakeIcon     *widget.Icon
	retakeLbl      *canvas.Text
	fileBg         *canvas.Rectangle
	fileLbl        *canvas.Text
	fileIcon       *widget.Icon
	caretSep       *canvas.Line
	caretIcon      *widget.Icon
	dropBg         *canvas.Rectangle
	dropFolderIcon *widget.Icon
	dropFolderLbl  *canvas.Text
	objs           []fyne.CanvasObject
}

const (
	pmCardW   = float32(660)
	pmCardH   = float32(500)
	pmCloseR  = float32(14)
	pmBtnH    = float32(42)
	pmRetakeW = float32(220) // "Take another screenshot" button
	pmFileW   = float32(178) // Open File + caret (screenshot mode)
	pmFileMW  = float32(150) // Open File main area (screenshot mode)
	pmCaretW  = float32(28)  // caret section width (both modes)
	pmSoloW   = float32(260) // Open File + caret (recording mode, wider since it stands alone)
	pmSoloMW  = float32(232) // Open File main area (recording mode)
	pmPrevH   = float32(320)
	pmDropH   = float32(38)
)

func (r *previewModalRenderer) Layout(size fyne.Size) {
	w := r.w
	t := w.animT

	w.mu.Lock()
	hoverClose    := w.hoverClose
	hoverRetake   := w.hoverRetake
	hoverFileMain := w.hoverFileMain
	hoverCaret    := w.hoverCaret
	dropdownOpen  := w.dropdownOpen
	hoverDropdown := w.hoverDropdown
	previewLoaded := w.previewLoaded
	previewImg    := w.previewImg
	w.mu.Unlock()

	hasRetake := w.onRetake != nil

	// Full-canvas dim layer, fading in with the animation.
	r.dim.Resize(size)
	r.dim.Move(fyne.NewPos(0, 0))
	r.dim.FillColor = color.NRGBA{0x00, 0x00, 0x00, uint8(float32(0x99) * t)}
	canvas.Refresh(r.dim)

	// Card is centered in the canvas; ox/oy is its top-left. It slides up 24 px
	// on open (down on close) — centered, so there's always room, no clipping.
	ox := (size.Width - pmCardW) / 2
	slideOff := float32(24) * (1 - t)
	oy := (size.Height-pmCardH)/2 + slideOff
	cardY := oy

	r.card.Move(fyne.NewPos(ox, cardY))
	r.card.Resize(fyne.NewSize(pmCardW, pmCardH))
	w.cardX1, w.cardY1 = ox, cardY
	w.cardX2, w.cardY2 = ox+pmCardW, cardY+pmCardH

	// ── Title ─────────────────────────────────────────────────────────────────
	r.titleText.Text = w.title
	r.titleText.Move(fyne.NewPos(ox+20, cardY+18))
	r.titleText.Resize(fyne.NewSize(pmCardW-20-pmCloseR*2-20, 22))
	canvas.Refresh(r.titleText)

	// ── Close button (plain circle, no countdown arc) ──────────────────────────
	closeCX := ox + pmCardW - pmCloseR - 14
	closeCY := cardY + pmCloseR + 12
	if hoverClose {
		r.closeBg.FillColor = color.NRGBA{0x66, 0x66, 0x66, 0xee}
	} else {
		r.closeBg.FillColor = color.NRGBA{0x44, 0x44, 0x44, 0xaa}
	}
	r.closeBg.Move(fyne.NewPos(closeCX-pmCloseR, closeCY-pmCloseR))
	r.closeBg.Resize(fyne.NewSize(pmCloseR*2, pmCloseR*2))
	canvas.Refresh(r.closeBg)

	closeXAlpha := uint8(0x99)
	if hoverClose {
		closeXAlpha = 0xff
	}
	r.closeXLbl.Color = color.NRGBA{0xff, 0xff, 0xff, closeXAlpha}
	r.closeXLbl.Move(fyne.NewPos(closeCX-pmCloseR, closeCY-11))
	r.closeXLbl.Resize(fyne.NewSize(pmCloseR*2, 22))
	canvas.Refresh(r.closeXLbl)
	w.closeX1, w.closeY1 = closeCX-pmCloseR, closeCY-pmCloseR
	w.closeX2, w.closeY2 = closeCX+pmCloseR, closeCY+pmCloseR

	// ── Preview image ─────────────────────────────────────────────────────────
	prevX := ox + 20
	prevY := cardY + 52
	prevW := pmCardW - 40
	if previewLoaded && previewImg != nil {
		r.prevImg.Image = previewImg
		r.prevImg.Move(fyne.NewPos(prevX, prevY))
		r.prevImg.Resize(fyne.NewSize(prevW, pmPrevH))
		r.prevImg.Show()
		canvas.Refresh(r.prevImg)
		r.loadingLbl.Hide()
	} else {
		r.prevImg.Hide()
		r.loadingLbl.Move(fyne.NewPos(prevX, prevY+pmPrevH/2-10))
		r.loadingLbl.Resize(fyne.NewSize(prevW, 20))
		r.loadingLbl.Show()
		canvas.Refresh(r.loadingLbl)
	}

	// ── Path bar ──────────────────────────────────────────────────────────────
	pathY := prevY + pmPrevH + 6
	r.pathText.Text = prettyPath(w.path)
	canvas.Refresh(r.pathText)
	r.pathBg.Move(fyne.NewPos(ox, pathY))
	r.pathBg.Resize(fyne.NewSize(pmCardW, 30))
	// Folder icon + path text, centered as a group.
	const pathIconSz = float32(15)
	textW := fyne.MeasureText(r.pathText.Text, r.pathText.TextSize, r.pathText.TextStyle).Width
	groupW := pathIconSz + 7 + textW
	groupX := ox + (pmCardW-groupW)/2
	if groupX < ox+12 {
		groupX = ox + 12
	}
	r.pathIcon.Move(fyne.NewPos(groupX, pathY+(30-pathIconSz)/2))
	r.pathIcon.Resize(fyne.NewSize(pathIconSz, pathIconSz))
	r.pathText.Alignment = fyne.TextAlignLeading
	r.pathText.Move(fyne.NewPos(groupX+pathIconSz+7, pathY+7))
	r.pathText.Resize(fyne.NewSize((ox+pmCardW)-(groupX+pathIconSz+7)-12, 18))

	// ── Buttons ───────────────────────────────────────────────────────────────
	btnY := pathY + 30 + 10
	const btnIconSz = float32(18)
	const btnIconGap = float32(9)

	var fileX, fileFullW, fileMainW float32
	if hasRetake {
		fileFullW = pmFileW
		fileMainW = pmFileMW
		const gap = float32(12)
		startX := ox + (pmCardW-pmRetakeW-gap-pmFileW)/2

		retakeX := startX
		if hoverRetake {
			r.retakeBg.FillColor = color.NRGBA{0x3a, 0x3a, 0x3a, 0xff}
		} else {
			r.retakeBg.FillColor = color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
		}
		r.retakeBg.Move(fyne.NewPos(retakeX, btnY))
		r.retakeBg.Resize(fyne.NewSize(pmRetakeW, pmBtnH))
		// Icon + label centered as a group.
		rLblW := fyne.MeasureText(r.retakeLbl.Text, r.retakeLbl.TextSize, r.retakeLbl.TextStyle).Width
		rGroupW := btnIconSz + btnIconGap + rLblW
		rGroupX := retakeX + (pmRetakeW-rGroupW)/2
		r.retakeIcon.Move(fyne.NewPos(rGroupX, btnY+(pmBtnH-btnIconSz)/2))
		r.retakeIcon.Resize(fyne.NewSize(btnIconSz, btnIconSz))
		r.retakeLbl.Alignment = fyne.TextAlignLeading
		r.retakeLbl.Move(fyne.NewPos(rGroupX+btnIconSz+btnIconGap, btnY+(pmBtnH-16)/2))
		r.retakeLbl.Resize(fyne.NewSize(rLblW+4, 16))
		r.retakeBg.Show()
		r.retakeIcon.Show()
		r.retakeLbl.Show()
		canvas.Refresh(r.retakeBg)
		canvas.Refresh(r.retakeLbl)
		w.retakeX1, w.retakeY1 = retakeX, btnY
		w.retakeX2, w.retakeY2 = retakeX+pmRetakeW, btnY+pmBtnH

		fileX = startX + pmRetakeW + gap
	} else {
		fileFullW = pmSoloW
		fileMainW = pmSoloMW
		fileX = ox + (pmCardW-pmSoloW)/2
		r.retakeBg.Hide()
		r.retakeIcon.Hide()
		r.retakeLbl.Hide()
		w.retakeX1, w.retakeY1, w.retakeX2, w.retakeY2 = 0, 0, 0, 0
	}

	// Open File button background (covers both the main area and the caret section).
	if hoverFileMain || hoverCaret {
		r.fileBg.FillColor = color.NRGBA{0x1e, 0x85, 0xff, 0xff}
	} else {
		r.fileBg.FillColor = color.NRGBA{0x16, 0x70, 0xe8, 0xff}
	}
	r.fileBg.Move(fyne.NewPos(fileX, btnY))
	r.fileBg.Resize(fyne.NewSize(fileFullW, pmBtnH))
	canvas.Refresh(r.fileBg)

	// File icon + label centered within the main (non-caret) area.
	fLblW := fyne.MeasureText(r.fileLbl.Text, r.fileLbl.TextSize, r.fileLbl.TextStyle).Width
	fGroupW := btnIconSz + btnIconGap + fLblW
	fGroupX := fileX + (fileMainW-fGroupW)/2
	r.fileIcon.Move(fyne.NewPos(fGroupX, btnY+(pmBtnH-btnIconSz)/2))
	r.fileIcon.Resize(fyne.NewSize(btnIconSz, btnIconSz))
	r.fileLbl.Alignment = fyne.TextAlignLeading
	r.fileLbl.Move(fyne.NewPos(fGroupX+btnIconSz+btnIconGap, btnY+(pmBtnH-16)/2))
	r.fileLbl.Resize(fyne.NewSize(fLblW+4, 16))
	canvas.Refresh(r.fileLbl)

	// Thin separator + caret icon between the main area and the dropdown trigger.
	sepX := fileX + fileMainW
	r.caretSep.Position1 = fyne.NewPos(sepX, btnY+8)
	r.caretSep.Position2 = fyne.NewPos(sepX, btnY+pmBtnH-8)
	canvas.Refresh(r.caretSep)

	const caretSz = float32(18)
	r.caretIcon.Move(fyne.NewPos(sepX+(pmCaretW-caretSz)/2, btnY+(pmBtnH-caretSz)/2))
	r.caretIcon.Resize(fyne.NewSize(caretSz, caretSz))

	w.fileMainX1, w.fileMainY1 = fileX, btnY
	w.fileMainX2, w.fileMainY2 = fileX+fileMainW, btnY+pmBtnH
	w.caretX1, w.caretY1 = sepX, btnY
	w.caretX2, w.caretY2 = fileX+fileFullW, btnY+pmBtnH

	// Dropdown panel — floats above the button row when open.
	dropX := fileX
	dropY := btnY - pmDropH - 4
	w.dropX1, w.dropY1 = dropX, dropY
	w.dropX2, w.dropY2 = dropX+fileFullW, dropY+pmDropH
	if dropdownOpen {
		if hoverDropdown {
			r.dropBg.FillColor = color.NRGBA{0x3c, 0x3c, 0x3c, 0xff}
		} else {
			r.dropBg.FillColor = color.NRGBA{0x2c, 0x2c, 0x2c, 0xff}
		}
		r.dropBg.Move(fyne.NewPos(dropX, dropY))
		r.dropBg.Resize(fyne.NewSize(fileFullW, pmDropH))
		r.dropFolderIcon.Move(fyne.NewPos(dropX+12, dropY+(pmDropH-20)/2))
		r.dropFolderIcon.Resize(fyne.NewSize(20, 20))
		r.dropFolderLbl.Move(fyne.NewPos(dropX+36, dropY+(pmDropH-14)/2))
		r.dropFolderLbl.Resize(fyne.NewSize(fileFullW-48, 14))
		r.dropBg.Show()
		r.dropFolderIcon.Show()
		r.dropFolderLbl.Show()
		canvas.Refresh(r.dropBg)
	} else {
		r.dropBg.Hide()
		r.dropFolderIcon.Hide()
		r.dropFolderLbl.Hide()
	}
}

func (r *previewModalRenderer) MinSize() fyne.Size { return fyne.NewSize(pmCardW, pmCardH) }
func (r *previewModalRenderer) Refresh() {
	// Lay out at the widget's real (canvas-filling) size, not the card size —
	// the card is centered within it.
	sz := r.w.Size()
	if sz.Width < pmCardW || sz.Height < pmCardH {
		sz = fyne.NewSize(pmCardW, pmCardH)
	}
	r.Layout(sz)
	canvas.Refresh(r.w)
}
func (r *previewModalRenderer) Destroy()                     {}
func (r *previewModalRenderer) Objects() []fyne.CanvasObject { return r.objs }

// ─── image helpers ────────────────────────────────────────────────────────────

func loadAnyImage(path string) image.Image {
	if f, err := os.Open(path); err == nil {
		img, _, err2 := image.Decode(f)
		f.Close()
		if err2 == nil {
			return img
		}
	}
	tmp := fmt.Sprintf("/tmp/swiftcap_prev_%d.jpg", time.Now().UnixNano())
	defer os.Remove(tmp)
	if err := exec.Command("ffmpeg", "-y", "-i", path, tmp).Run(); err != nil {
		return nil
	}
	f, err := os.Open(tmp)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, _ := image.Decode(f)
	return img
}

func extractVideoThumb(path string) image.Image {
	tmp := fmt.Sprintf("/tmp/swiftcap_thumb_%d.jpg", time.Now().UnixNano())
	defer os.Remove(tmp)
	_ = exec.Command("ffmpeg", "-y", "-i", path,
		"-ss", "00:00:00.5", "-vframes", "1", "-q:v", "3", tmp).Run()
	f, err := os.Open(tmp)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, _ := image.Decode(f)
	return img
}

// prettyPath renders a filesystem path for the modal's path bar: the home
// directory is abbreviated to "~", and overly long paths are elided in the
// middle so the filename always stays visible.
func prettyPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}
	const max = 62
	if len(path) <= max {
		return path
	}
	base := filepath.Base(path)
	keep := max - len(base) - 2 // room for the "…/" join
	if keep < 4 {
		return "…/" + base
	}
	return path[:keep] + "…/" + base
}

// ─── file helpers ─────────────────────────────────────────────────────────────

func openFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	target := path
	if !info.IsDir() {
		target = filepath.Dir(path)
	}
	return openPath(target)
}

func openFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return openPath(path)
}

func openPath(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

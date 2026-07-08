package uiapp

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"time"

	xdraw "golang.org/x/image/draw"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Editor-only tools (the snip markup toolbar has 5; the editor adds these).
const (
	mkToolArrow markupTool = 5
	mkToolCrop  markupTool = 6
)

// ─── markup editor ────────────────────────────────────────────────────────────
//
// A fullscreen image editor for annotating a saved screenshot. It shows the
// image contain-fit on a dark backdrop, draws into a display-resolution buffer
// (scaled up to the image on save via saveComposite), and reuses the drawing
// primitives from markup_tools.go. Nothing is written until Save; the original
// is backed up to "<path>.orig" so edits can be reverted later.

type markupEditor struct {
	ui     *RecordingUI
	win    fyne.Window
	path   string
	full   image.Image // original image at native resolution
	onDone func(saved bool)

	cv *markupCanvas

	dirty bool // any edit made since opening (drives the Cancel confirmation)

	// Discard-confirmation modal state (custom animated modal, not a stock dialog).
	confirmOpen    bool
	confirmAccept  func()
	confirmDismiss func()

	// Shared tool state (read by the canvas while drawing).
	tool      markupTool
	col       color.NRGBA // stroke / border colour
	fillCol   color.NRGBA // fill colour (separate from the border)
	size      int
	fill      bool
	blurStyle int // 0 = pixelate, 1 = smooth

	toolBtns map[markupTool]*iconButton

	// Options popout (floats below the toolbar) and its contextual controls.
	palette     *fyne.Container
	paletteRow  *fyne.Container
	fillPalette *fyne.Container
	fillRow     *fyne.Container
	sizeRow     *fyne.Container
	fillCheck   *widget.Check
	blurRow     *fyne.Container
	cropRow     *fyne.Container
	floatLayer *fyne.Container
	tipLayer   *fyne.Container
	popoutWrap *fyne.Container
	popoutOpen bool
	popBg      *canvas.Rectangle
	popTri     *canvas.Image
	popSlide   *slideReveal
	popAnim    *fyne.Animation

	// Real-time brush-size preview (a dot sized to the stroke, auto-hides after 2s).
	sizeLayer *fyne.Container
	sizeDot   *canvas.Circle
	sizeLbl   *canvas.Text
	sizeTimer *time.Timer
}

func showMarkupEditor(ui *RecordingUI, path string, onDone func(saved bool)) {
	full := loadAnyImage(path)
	if full == nil {
		ui.showError("Edit", "Could not open this image for editing.")
		if onDone != nil {
			onDone(false)
		}
		return
	}

	ed := &markupEditor{
		ui:     ui,
		path:   path,
		full:   full,
		onDone:  onDone,
		tool:    mkToolBrush,
		col:     mkPaletteColors[0],
		fillCol: mkPaletteColors[0],
		size:    6,
	}

	ed.cv = newMarkupCanvas(ed)

	// A normal decorated window (title bar with minimise/maximise/close),
	// opened large and centred. The canvas fills the window; the toolbar floats
	// on top of it, so it stays responsive to any resize/maximise.
	win := ui.app.NewWindow("Edit Screenshot")
	// buildToolbar creates ed.tipLayer; it sits on top so tooltips draw above all.
	toolbar := ed.buildToolbar()
	win.SetContent(container.NewStack(ed.cv, toolbar, ed.sizeLayer, ed.tipLayer))
	win.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		// While the discard-confirmation modal is up it owns the keyboard.
		if ed.confirmOpen {
			switch k.Name {
			case fyne.KeyEscape:
				if ed.confirmDismiss != nil {
					ed.confirmDismiss()
				}
			case fyne.KeyReturn, fyne.KeyEnter:
				if ed.confirmAccept != nil {
					ed.confirmAccept()
				}
			}
			return
		}
		switch k.Name {
		case fyne.KeyEscape:
			if ed.cv.cropping {
				ed.closePopout() // cancels the crop frame
			} else if ed.cv.active != nil {
				ed.cv.discardActive() // drop the in-progress shape
			} else {
				ed.cancel()
			}
		case fyne.KeyReturn, fyne.KeyEnter:
			if ed.cv.cropping {
				ed.cv.confirmCrop()
				ed.closePopout()
			}
		case fyne.KeyZ:
			ed.cv.undoLast()
		case fyne.KeyY:
			ed.cv.redoLast()
		}
	})
	// Modifier shortcuts: Ctrl+Z undo, Ctrl+Y / Ctrl+Shift+Z redo, Ctrl+S save.
	addShortcut := func(key fyne.KeyName, mod fyne.KeyModifier, fn func()) {
		win.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: key, Modifier: mod},
			func(fyne.Shortcut) { fn() })
	}
	addShortcut(fyne.KeyZ, fyne.KeyModifierControl, func() { ed.cv.undoLast() })
	addShortcut(fyne.KeyY, fyne.KeyModifierControl, func() { ed.cv.redoLast() })
	addShortcut(fyne.KeyZ, fyne.KeyModifierControl|fyne.KeyModifierShift, func() { ed.cv.redoLast() })
	addShortcut(fyne.KeyS, fyne.KeyModifierControl, func() { ed.save() })
	// Closing the window (title-bar ✕) goes through the same unsaved-changes guard.
	win.SetCloseIntercept(func() { ed.cancel() })
	screenW, screenH := getScreenSize()
	win.Resize(fyne.NewSize(float32(screenW)*0.9, float32(screenH)*0.88))
	win.CenterOnScreen()
	ed.win = win
	win.Show()
}

func (ed *markupEditor) finish(saved bool) {
	if ed.win != nil {
		ed.win.Close()
		ed.win = nil
	}
	if ed.onDone != nil {
		ed.onDone(saved)
	}
}

// cancel closes the editor, confirming first if there are unsaved edits.
func (ed *markupEditor) cancel() {
	if ed.confirmOpen {
		return // already asking
	}
	if ed.dirty && ed.win != nil {
		ed.showDiscardConfirm()
		return
	}
	ed.finish(false)
}

// showDiscardConfirm presents a custom animated modal (settings-modal style, but
// with a lighter frosted backdrop) asking whether to discard unsaved edits.
func (ed *markupEditor) showDiscardConfirm() {
	if ed.win == nil {
		return
	}
	cv := ed.win.Canvas()
	ed.confirmOpen = true

	var startClose func()
	closing := false
	dismiss := func() {
		if closing {
			return
		}
		closing = true
		if startClose != nil {
			startClose()
		}
	}

	// ── card ──
	title := canvas.NewText("Discard changes?", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.TextSize = 19
	msg := canvas.NewText("Your unsaved edits will be lost if you close now.", color.NRGBA{0xac, 0xac, 0xb8, 0xff})
	msg.TextSize = 14

	keepBtn := newSolidButton("Keep editing",
		color.NRGBA{0x36, 0x37, 0x40, 0xff}, color.NRGBA{0x44, 0x45, 0x50, 0xff},
		color.NRGBA{0xff, 0xff, 0xff, 0xff}, func() { dismiss() })
	discardBtn := newSolidButton("Discard",
		color.NRGBA{0xe2, 0x3b, 0x3b, 0xff}, color.NRGBA{0xf1, 0x4b, 0x4b, 0xff},
		color.NRGBA{0xff, 0xff, 0xff, 0xff}, func() {
			dismiss()
			ed.finish(false)
		})
	btnRow := container.NewHBox(layout.NewSpacer(), keepBtn, newWidthSpacer(4), discardBtn)

	// A zero-height spacer sets a comfortable minimum card width.
	widthSetter := container.NewGridWrap(fyne.NewSize(404, 0), canvas.NewRectangle(color.Transparent))
	inner := container.NewVBox(
		widthSetter,
		container.NewHBox(widget.NewIcon(theme.WarningIcon()), title),
		newHeightSpacer(4),
		msg,
		newHeightSpacer(22),
		btnRow,
	)

	// Solid opaque card with a clean, slightly bold border.
	cardBg := canvas.NewRectangle(color.NRGBA{0x1e, 0x1f, 0x24, 0xff})
	cardBg.CornerRadius = 16
	cardBg.StrokeColor = color.NRGBA{0x5a, 0x5b, 0x66, 0xff}
	cardBg.StrokeWidth = 2
	card := newTapableContainer(container.NewStack(cardBg, insetBy(inner, 24)), func() {})
	cardLayer := container.NewWithoutLayout(card)

	// ── lighter frosted backdrop (small radius so you can almost see behind) ──
	var bgFade func(vis float32)
	var bgObj fyne.CanvasObject
	if shot := cv.Capture(); shot != nil {
		blur := canvas.NewImageFromImage(blurredBackdrop(shot, 3))
		blur.FillMode = canvas.ImageFillStretch
		bgObj = blur
		// A gentle frost — enough blur to separate the modal, but you can still
		// make out the editor behind it.
		bgFade = func(vis float32) {
			blur.Translucency = float64(1 - 0.62*vis)
			canvas.Refresh(blur)
		}
	} else {
		rect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 0x66})
		bgObj = rect
		bgFade = func(vis float32) {
			rect.FillColor = color.NRGBA{0, 0, 0, uint8(0x66 * vis)}
			canvas.Refresh(rect)
		}
	}
	backdrop := newTapableContainer(bgObj, func() {}) // block clicks behind
	overlay := container.NewStack(backdrop, cardLayer)

	place := func(yOff float32) {
		sz := card.MinSize()
		cs := cv.Size()
		card.Resize(sz)
		card.Move(fyne.NewPos((cs.Width-sz.Width)/2, (cs.Height-sz.Height)/2+yOff))
	}

	startClose = func() {
		anim := fyne.NewAnimation(150*time.Millisecond, func(f float32) {
			bgFade(1 - f)
			place(24 * f)
			if f >= 1 {
				cv.Overlays().Remove(overlay)
				ed.confirmOpen = false
				ed.confirmAccept = nil
				ed.confirmDismiss = nil
			}
		})
		anim.Curve = fyne.AnimationEaseIn
		anim.Start()
	}

	ed.confirmDismiss = dismiss
	ed.confirmAccept = func() {
		dismiss()
		ed.finish(false)
	}

	bgFade(0)
	place(24)
	cv.Overlays().Add(overlay)
	overlay.Resize(cv.Size())
	place(24)

	openAnim := fyne.NewAnimation(190*time.Millisecond, func(f float32) {
		bgFade(f)
		place(24 * (1 - f))
	})
	openAnim.Curve = fyne.AnimationEaseOut
	openAnim.Start()
}

func (ed *markupEditor) save() {
	ed.cv.commitActive() // bake any in-progress shape into the buffer first
	out, err := backupAndCompose(ed.path, ed.full, ed.cv.buf)
	if err != nil {
		ed.ui.showError("Save", err.Error())
		return
	}
	_ = out
	ed.finish(true)
}

// ─── toolbar ──────────────────────────────────────────────────────────────────

func (ed *markupEditor) buildToolbar() fyne.CanvasObject {
	// Tooltips live on this non-interactive content layer (see iconButton).
	ed.tipLayer = container.NewWithoutLayout()
	tipHost := ed.tipLayer

	// ── tool + transform + action icons (icon-only, tooltip on hover) ──
	ed.toolBtns = map[markupTool]*iconButton{}
	toolDef := []struct {
		t    markupTool
		icon fyne.Resource
		tip  string
	}{
		{mkToolBrush, theme.ColorChromaticIcon(), "Brush"},
		{mkToolHighlight, theme.ColorPaletteIcon(), "Highlight"},
		{mkToolRect, theme.CheckButtonIcon(), "Rectangle"},
		{mkToolCircle, theme.RadioButtonIcon(), "Circle"},
		{mkToolArrow, theme.MailForwardIcon(), "Arrow"},
		{mkToolBlur, theme.VisibilityOffIcon(), "Blur / pixelate"},
		{mkToolCrop, mkIconCrop, "Crop"},
	}
	var toolObjs []fyne.CanvasObject
	for _, d := range toolDef {
		d := d
		b := newIconButton(d.icon, d.tip, func() { ed.onToolClicked(d.t) }, tipHost)
		ed.toolBtns[d.t] = b
		toolObjs = append(toolObjs, b)
	}

	rotL := newIconButton(mkIconRotateLeft, "Rotate left", func() { ed.cv.applyTransform(func(im image.Image) *image.RGBA { return rotate90(im, false) }) }, tipHost)
	rotR := newIconButton(mkIconRotateRight, "Rotate right", func() { ed.cv.applyTransform(func(im image.Image) *image.RGBA { return rotate90(im, true) }) }, tipHost)
	flipH := newIconButton(mkIconFlipH, "Mirror horizontally", func() { ed.cv.applyTransform(flipHoriz) }, tipHost)
	flipV := newIconButton(mkIconFlipV, "Flip vertically", func() { ed.cv.applyTransform(flipVert) }, tipHost)

	undoBtn := newIconButton(theme.ContentUndoIcon(), "Undo", func() { ed.cv.undoLast() }, tipHost)
	cancelBtn := newIconButton(theme.CancelIcon(), "Cancel", func() { ed.cancel() }, tipHost)
	saveBtn := newIconButton(theme.DocumentSaveIcon(), "Save", func() { ed.save() }, tipHost)
	saveBtn.Importance = widget.HighImportance

	sep := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.NRGBA{0xff, 0xff, 0xff, 0x22})
		return container.NewGridWrap(fyne.NewSize(1, 24), r)
	}
	row := container.NewHBox(
		container.NewHBox(toolObjs...), sep(),
		rotL, rotR, flipH, flipV, sep(),
		undoBtn, cancelBtn, saveBtn,
	)
	toolbar := newTapableContainer(glassBox(row), func() {})

	// ── options popout (built once, shown contextually below the toolbar) ──
	mkPalette := func(onPick func(color.NRGBA)) *fyne.Container {
		var sw []fyne.CanvasObject
		for _, c := range mkPaletteColors {
			c := c
			sw = append(sw, newColorSwatch(c, onPick))
		}
		return container.NewHBox(sw...)
	}
	ed.palette = mkPalette(func(col color.NRGBA) { ed.setColor(col) })
	ed.fillPalette = mkPalette(func(col color.NRGBA) { ed.setFillColor(col) })
	ed.paletteRow = container.NewBorder(nil, nil, widget.NewLabel("Border"), nil, ed.palette)

	sizeSlider := widget.NewSlider(1, 40)
	sizeSlider.Value = float64(ed.size)
	sizeSlider.OnChanged = func(v float64) { ed.setSize(int(v)) }
	ed.sizeRow = container.NewBorder(nil, nil, widget.NewLabel("Size"), nil,
		container.NewGridWrap(fyne.NewSize(190, 30), sizeSlider))

	ed.fillCheck = widget.NewCheck("Fill", func(b bool) { ed.setFilled(b) })
	ed.fillPalette.Hide() // only shown once Fill is enabled
	ed.fillRow = container.NewBorder(nil, nil, ed.fillCheck, nil, ed.fillPalette)

	// Blur style: a clear two-option toggle instead of a dropdown.
	blurRadio := widget.NewRadioGroup([]string{"Pixelate", "Blur"}, func(s string) {
		if s == "Blur" {
			ed.blurStyle = 1
		} else {
			ed.blurStyle = 0
		}
	})
	blurRadio.Horizontal = true
	blurRadio.SetSelected("Pixelate")
	ed.blurRow = container.NewBorder(nil, nil, widget.NewLabel("Style"), nil,
		container.NewHBox(blurRadio, layout.NewSpacer()))

	applyCrop := newButton("Apply", func() { ed.cv.confirmCrop(); ed.closePopout() })
	applyCrop.Importance = widget.HighImportance
	resetCrop := newButton("Reset", func() { ed.cv.enterCrop() })
	ed.cropRow = container.NewHBox(
		widget.NewLabel("Drag the handles, then Apply (Enter)"),
		layout.NewSpacer(), resetCrop, applyCrop,
	)

	popClose := newIconButton(theme.CancelIcon(), "Close", func() { ed.closePopout() }, tipHost)
	popBody := container.NewVBox(
		container.NewBorder(nil, nil,
			widget.NewLabelWithStyle("Options", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			popClose, nil),
		ed.paletteRow, ed.sizeRow, ed.fillRow, ed.blurRow, ed.cropRow,
	)

	ed.popBg = canvas.NewRectangle(color.NRGBA{0x14, 0x14, 0x18, 0xbe})
	ed.popBg.CornerRadius = 12
	ed.popBg.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0x20}
	ed.popBg.StrokeWidth = 1
	popBox := newTapableContainer(container.NewStack(ed.popBg, container.NewPadded(popBody)), func() {})

	ed.popTri = triangleUp(color.NRGBA{0x14, 0x14, 0x18, 0xe0}, 20, 9)
	triCell := container.NewGridWrap(fyne.NewSize(20, 9), ed.popTri)
	popInner := container.NewVBox(container.NewCenter(triCell), container.NewCenter(popBox))
	ed.popSlide = &slideReveal{}
	ed.popoutWrap = container.New(ed.popSlide, popInner)
	ed.popoutWrap.Hide()

	// Brush-size preview: a dot the size of the stroke, centred over the canvas.
	sizeBg := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0xb4})
	sizeBg.CornerRadius = 14
	sizeBg.Resize(fyne.NewSize(96, 96))
	ed.sizeDot = canvas.NewCircle(color.NRGBA{0xff, 0xff, 0xff, 0xf0})
	ed.sizeDot.StrokeColor = color.NRGBA{0x10, 0x10, 0x12, 0xff}
	ed.sizeDot.StrokeWidth = 1
	ed.sizeLbl = canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	ed.sizeLbl.TextSize = 13
	ed.sizeLbl.Alignment = fyne.TextAlignCenter
	sizePanel := container.NewWithoutLayout(sizeBg, ed.sizeDot, ed.sizeLbl)
	ed.sizeLayer = container.NewCenter(container.NewGridWrap(fyne.NewSize(96, 96), sizePanel))
	ed.sizeLayer.Hide()

	ed.setActiveTool(mkToolBrush)

	// Float the toolbar and popout at the top-centre, over the canvas.
	ed.floatLayer = container.NewVBox(
		newHeightSpacer(14),
		container.NewCenter(toolbar),
		newHeightSpacer(6),
		ed.popoutWrap,
		layout.NewSpacer(),
	)
	return ed.floatLayer
}

// slideReveal centres its single child horizontally and offsets it vertically by
// offset (animated) so the popout can slide in/out from under the toolbar.
type slideReveal struct{ offset float32 }

func (l *slideReveal) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }
func (l *slideReveal) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	o := objs[0]
	m := o.MinSize()
	o.Resize(m)
	o.Move(fyne.NewPos((size.Width-m.Width)/2, l.offset))
}

// Every tool now has a contextual popout (crop shows its adjust/apply controls).
func toolHasOptions(markupTool) bool { return true }

// setActiveTool sets the tool and highlights its icon.
func (ed *markupEditor) setActiveTool(t markupTool) {
	ed.tool = t
	for tt, b := range ed.toolBtns {
		if tt == t {
			b.Importance = widget.HighImportance
		} else {
			b.Importance = widget.LowImportance
		}
		b.Refresh()
	}
}

// onToolClicked switches tool and manages the options popout: a new tool opens
// its popout, re-clicking the active tool toggles it, an option-less tool closes.
func (ed *markupEditor) onToolClicked(t markupTool) {
	same := ed.tool == t
	if !same && ed.cv != nil {
		ed.cv.commitActive() // bake any in-progress shape before switching tools
	}
	ed.setActiveTool(t)
	if !toolHasOptions(t) {
		ed.closePopout()
		return
	}
	if same {
		ed.togglePopout()
	} else {
		ed.openPopout(t)
	}
}

func (ed *markupEditor) openPopout(t markupTool) {
	setVis := func(o fyne.CanvasObject, show bool) {
		if o == nil {
			return
		}
		if show {
			o.Show()
		} else {
			o.Hide()
		}
	}
	isCrop := t == mkToolCrop
	setVis(ed.paletteRow, !isCrop && t != mkToolBlur)
	setVis(ed.sizeRow, !isCrop)
	setVis(ed.fillRow, t == mkToolRect || t == mkToolCircle)
	setVis(ed.fillPalette, ed.fill) // fill colours only when Fill is enabled
	setVis(ed.blurRow, t == mkToolBlur)
	setVis(ed.cropRow, isCrop)
	if isCrop {
		ed.cv.enterCrop()
	} else if ed.cv.cropping {
		ed.cv.exitCrop() // switched away from crop
	}
	ed.popoutWrap.Show()
	ed.floatLayer.Refresh() // re-layout so the now-visible popout gets real width
	ed.popoutOpen = true
	ed.animatePopout(true)
}

func (ed *markupEditor) closePopout() {
	if ed.cv != nil && ed.cv.cropping {
		ed.cv.exitCrop()
	}
	if !ed.popoutOpen {
		ed.popoutWrap.Hide()
		return
	}
	ed.popoutOpen = false
	ed.animatePopout(false)
}

func (ed *markupEditor) togglePopout() {
	if ed.popoutOpen {
		ed.closePopout()
	} else {
		ed.openPopout(ed.tool)
	}
}

// animatePopout slides + fades the popout in (in=true) or out, hiding it at the
// end of the close animation.
func (ed *markupEditor) animatePopout(in bool) {
	if ed.popSlide == nil {
		return
	}
	if ed.popAnim != nil {
		ed.popAnim.Stop()
	}
	const dist float32 = 14
	ed.popAnim = fyne.NewAnimation(150*time.Millisecond, func(f float32) {
		p := f
		if !in {
			p = 1 - f
		}
		ed.popSlide.offset = -dist * (1 - p)
		ed.popBg.FillColor = color.NRGBA{0x14, 0x14, 0x18, uint8(float32(0xbe) * p)}
		ed.popBg.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, uint8(float32(0x20) * p)}
		ed.popTri.Translucency = float64(1 - p)
		ed.popoutWrap.Refresh()
		canvas.Refresh(ed.popBg)
		canvas.Refresh(ed.popTri)
		if f >= 1 && !in {
			ed.popoutWrap.Hide()
		}
	})
	ed.popAnim.Curve = fyne.AnimationEaseOut
	ed.popAnim.Start()
}

// The colour/fill/size setters also live-update the active shape, if any, so an
// already-placed shape can be recoloured/resized before it's baked.
func (ed *markupEditor) setColor(c color.NRGBA) {
	ed.col = c
	if ed.cv != nil && ed.cv.active != nil {
		ed.cv.active.stroke = c
		ed.cv.updateActive()
	}
}

func (ed *markupEditor) setFillColor(c color.NRGBA) {
	ed.fillCol = c
	if ed.cv != nil && ed.cv.active != nil {
		ed.cv.active.fill = c
		ed.cv.updateActive()
	}
}

func (ed *markupEditor) setFilled(b bool) {
	ed.fill = b
	if ed.fillPalette != nil {
		if b {
			ed.fillPalette.Show()
		} else {
			ed.fillPalette.Hide()
		}
	}
	if ed.cv != nil && ed.cv.active != nil {
		ed.cv.active.filled = b
		ed.cv.updateActive()
	}
}

func (ed *markupEditor) setSize(v int) {
	ed.size = v
	if ed.cv != nil && ed.cv.active != nil {
		ed.cv.active.strokeW = v
		ed.cv.updateActive()
	}
	ed.showSizePreview() // real-time thickness overlay (auto-hides)
}

// showSizePreview flashes a dot sized to the current stroke, centred over the
// canvas, and hides it after 2s of no further size changes.
func (ed *markupEditor) showSizePreview() {
	if ed.sizeLayer == nil {
		return
	}
	d := float32(clampInt(ed.size, 1, 40))
	const box = 96
	ed.sizeDot.Resize(fyne.NewSize(d, d))
	ed.sizeDot.Move(fyne.NewPos((box-d)/2, (64-d)/2))
	ed.sizeLbl.Text = fmt.Sprintf("%d px", ed.size)
	ed.sizeLbl.Move(fyne.NewPos(0, 68))
	ed.sizeLbl.Resize(fyne.NewSize(box, 20))
	ed.sizeLbl.Refresh()
	ed.sizeLayer.Show()
	ed.sizeLayer.Refresh()

	if ed.sizeTimer != nil {
		ed.sizeTimer.Stop()
	}
	ed.sizeTimer = time.AfterFunc(2*time.Second, func() {
		ed.ui.runOnMain(func() { ed.sizeLayer.Hide() })
	})
}

// glassBox wraps content in a translucent, softly-outlined rounded panel.
func glassBox(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{0x14, 0x14, 0x18, 0xbe})
	bg.CornerRadius = 12
	bg.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0x20}
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(content))
}

// triangleUp draws a small upward-pointing triangle (the popout's pointer).
func triangleUp(col color.NRGBA, w, h int) *canvas.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	cx := w / 2
	for y := 0; y < h; y++ {
		half := y * w / (2 * h)
		for x := cx - half; x <= cx+half; x++ {
			if x >= 0 && x < w {
				img.Set(x, y, col)
			}
		}
	}
	ci := canvas.NewImageFromImage(img)
	ci.FillMode = canvas.ImageFillOriginal
	return ci
}

// ─── colour swatch ────────────────────────────────────────────────────────────

type colorSwatch struct {
	widget.BaseWidget
	col   color.NRGBA
	onTap func(color.NRGBA)
}

func newColorSwatch(c color.NRGBA, onTap func(color.NRGBA)) *colorSwatch {
	s := &colorSwatch{col: c, onTap: onTap}
	s.ExtendBaseWidget(s)
	return s
}

func (s *colorSwatch) MinSize() fyne.Size                 { return fyne.NewSize(26, 26) }
func (s *colorSwatch) Cursor() desktop.Cursor             { return desktop.PointerCursor }
func (s *colorSwatch) Tapped(*fyne.PointEvent)            { if s.onTap != nil { s.onTap(s.col) } }
func (s *colorSwatch) TappedSecondary(*fyne.PointEvent)   {}

func (s *colorSwatch) CreateRenderer() fyne.WidgetRenderer {
	r := canvas.NewRectangle(s.col)
	r.CornerRadius = 5
	r.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0x55}
	r.StrokeWidth = 1
	return widget.NewSimpleRenderer(container.NewPadded(r))
}

// ─── markup canvas ────────────────────────────────────────────────────────────

// The canvas is fully responsive: it computes the contain-fit rectangle from the
// real size it's given at Layout time (never a precomputed screen size), and its
// buffer is (re)created to match. So the image and drawing are always confined to
// whatever area the layout allots, never overflowing the window.

type markupCanvas struct {
	widget.BaseWidget
	ed     *markupEditor
	iw, ih int // source image dimensions

	fitX, fitY, fitW, fitH float32 // display rect (set in Layout)
	bufW, bufH             int     // buffer dimensions (== int(fitW/H))

	buf      *image.RGBA // markup buffer (fit resolution)
	bgScaled *image.RGBA // image scaled to fit resolution (for blur sampling)

	darkBg  *canvas.Rectangle
	bgObj   *canvas.Image
	overlay *canvas.Image

	// Lightweight vector previews for shapes/blur (drawn while dragging so we
	// don't touch/re-upload the big overlay texture every mouse move).
	prevRect   *canvas.Rectangle
	prevCircle *canvas.Circle
	prevLine   *canvas.Line

	drawing        bool
	startX, startY int
	curX, curY     int
	brushPts       []image.Point
	undo           []undoEntry
	redo           []undoEntry
	restoring      bool // set while undo/redo restores, so ensureBuffers keeps stacks

	// Interactive crop mode (region-selector-style adjustable frame + handles).
	cropping   bool
	cropRect   image.Rectangle // buffer coords
	cropDrag   string          // "", "move", "new" or handle code (n/s/e/w/ne/nw/se/sw)
	cropAnchor image.Point     // buffer coords at drag start
	cropStart  image.Rectangle // crop rect at drag start (for "move")
	cropDim    []*canvas.Rectangle
	cropFrame  *canvas.Rectangle
	cropHandle []*canvas.Circle
	cropGrid   []*canvas.Line

	// Where the image is drawn on screen. Normally equals the fit rect; in crop
	// mode it's letterboxed (padded) so the handles sit inside the window.
	dispX, dispY, dispW, dispH float32

	// The last-drawn shape stays adjustable (move/resize/recolour) until it's
	// baked into the buffer (on the next action, tool switch, or save).
	active    *editShape
	actDrag   string // "", "new", "move", bbox handle code, or "p0"/"p1" (arrow ends)
	actAnchor image.Point
	actStart  editShape
	selHandle []*canvas.Circle
	prevHead1 *canvas.Line // arrow head preview
	prevHead2 *canvas.Line
}

// editShape is a vector shape being edited before it's rasterised into the buffer.
type editShape struct {
	kind          markupTool // mkToolRect / mkToolCircle / mkToolArrow
	x0, y0, x1, y1 int        // buffer coords (bbox for rect/circle, endpoints for arrow)
	stroke        color.NRGBA
	fill          color.NRGBA
	filled        bool
	strokeW       int
}

func (s *editShape) bbox() (int, int, int, int) {
	return min(s.x0, s.x1), min(s.y0, s.y1), max(s.x0, s.x1), max(s.y0, s.y1)
}

func isVectorShape(t markupTool) bool {
	return t == mkToolRect || t == mkToolCircle || t == mkToolArrow
}

// cropHandleCodes lists the 8 resize handles clockwise from the top-left.
var cropHandleCodes = []string{"nw", "n", "ne", "e", "se", "s", "sw", "w"}

// undoEntry snapshots state for undo. A plain draw only needs the markup buffer;
// a transform (rotate/flip/crop) also snapshots the base image and dimensions.
type undoEntry struct {
	buf    *image.RGBA
	full   image.Image // non-nil ⇒ transform: restore base image + dims too
	iw, ih int
}

func newMarkupCanvas(ed *markupEditor) *markupCanvas {
	b := ed.full.Bounds()
	c := &markupCanvas{ed: ed, iw: b.Dx(), ih: b.Dy()}
	c.darkBg = canvas.NewRectangle(color.NRGBA{0x0b, 0x0b, 0x0d, 0xff})
	c.bgObj = canvas.NewImageFromImage(ed.full)
	c.bgObj.FillMode = canvas.ImageFillStretch
	c.overlay = canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	c.overlay.FillMode = canvas.ImageFillStretch
	// Fast (nearest) scaling: the overlay is re-uploaded on every draw, and smooth
	// resampling of a large image each time is what makes strokes lag behind the
	// cursor. The buffer is ~display resolution so quality is unaffected.
	c.overlay.ScaleMode = canvas.ImageScaleFastest

	c.prevRect = canvas.NewRectangle(color.Transparent)
	c.prevCircle = canvas.NewCircle(color.Transparent)
	c.prevLine = canvas.NewLine(color.Transparent)
	c.prevHead1 = canvas.NewLine(color.Transparent)
	c.prevHead2 = canvas.NewLine(color.Transparent)
	c.prevRect.Hide()
	c.prevCircle.Hide()
	c.prevLine.Hide()
	c.prevHead1.Hide()
	c.prevHead2.Hide()
	// Selection handles for the active shape (same bold-dot style as crop).
	for i := 0; i < len(cropHandleCodes); i++ {
		h := canvas.NewCircle(color.NRGBA{0xff, 0xff, 0xff, 0xff})
		h.StrokeColor = color.NRGBA{0x10, 0x10, 0x12, 0xff}
		h.StrokeWidth = 2
		h.Hide()
		c.selHandle = append(c.selHandle, h)
	}

	// Crop overlay: four dark masks around the frame, a white frame, 8 handles.
	for i := 0; i < 4; i++ {
		r := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0x99})
		r.Hide()
		c.cropDim = append(c.cropDim, r)
	}
	c.cropFrame = canvas.NewRectangle(color.Transparent)
	c.cropFrame.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	c.cropFrame.StrokeWidth = 2
	c.cropFrame.Hide()
	// Bold white dots with a dark ring so they're obvious on any background.
	for i := 0; i < len(cropHandleCodes); i++ {
		h := canvas.NewCircle(color.NRGBA{0xff, 0xff, 0xff, 0xff})
		h.StrokeColor = color.NRGBA{0x10, 0x10, 0x12, 0xff}
		h.StrokeWidth = 2.5
		h.Hide()
		c.cropHandle = append(c.cropHandle, h)
	}
	// Rule-of-thirds grid (2 vertical + 2 horizontal lines) inside the frame.
	for i := 0; i < 4; i++ {
		ln := canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x66})
		ln.StrokeWidth = 1
		ln.Hide()
		c.cropGrid = append(c.cropGrid, ln)
	}

	c.ExtendBaseWidget(c)
	return c
}

// ensureBuffers (re)creates the markup + scaled-background buffers at bw×bh,
// rescaling any existing drawing so it survives a size change.
func (c *markupCanvas) ensureBuffers(bw, bh int) {
	if bw < 1 {
		bw = 1
	}
	if bh < 1 {
		bh = 1
	}
	if c.buf != nil && c.bufW == bw && c.bufH == bh {
		return
	}
	nbuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	if c.buf != nil {
		xdraw.ApproxBiLinear.Scale(nbuf, nbuf.Bounds(), c.buf, c.buf.Bounds(), xdraw.Over, nil)
	}
	c.buf = nbuf
	c.bufW, c.bufH = bw, bh
	c.overlay.Image = c.buf

	c.bgScaled = image.NewRGBA(image.Rect(0, 0, bw, bh))
	xdraw.CatmullRom.Scale(c.bgScaled, c.bgScaled.Bounds(), c.ed.full, c.ed.full.Bounds(), xdraw.Src, nil)
	if !c.restoring {
		// A resize invalidates snapshots taken at the old size — but an undo/redo
		// is deliberately rebuilding at a stored size, so keep the stacks then.
		c.undo = nil
		c.redo = nil
	}
	if c.cropping {
		c.updateCropVisuals() // frame lives in buffer coords; refit to new size
	}
}

func (c *markupCanvas) CreateRenderer() fyne.WidgetRenderer {
	objs := []fyne.CanvasObject{
		c.darkBg, c.bgObj, c.overlay,
		c.prevRect, c.prevCircle, c.prevLine, c.prevHead1, c.prevHead2,
	}
	for _, h := range c.selHandle {
		objs = append(objs, h)
	}
	for _, r := range c.cropDim {
		objs = append(objs, r)
	}
	for _, ln := range c.cropGrid {
		objs = append(objs, ln)
	}
	objs = append(objs, c.cropFrame)
	for _, h := range c.cropHandle {
		objs = append(objs, h)
	}
	return &markupCanvasRenderer{c: c, objs: objs}
}

func (c *markupCanvas) Cursor() desktop.Cursor { return desktop.CrosshairCursor }

// bufCoord maps a widget-local point to buffer pixel coords, clamped to the image.
func (c *markupCanvas) bufCoord(p fyne.Position) (int, int, bool) {
	x := p.X - c.fitX
	y := p.Y - c.fitY
	inside := x >= 0 && y >= 0 && x < c.fitW && y < c.fitH
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x > c.fitW-1 {
		x = c.fitW - 1
	}
	if y > c.fitH-1 {
		y = c.fitH - 1
	}
	return int(x), int(y), inside
}

func (c *markupCanvas) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary || c.buf == nil {
		return
	}
	if c.cropping {
		c.cropMouseDown(ev.Position)
		return
	}
	// If a shape is still adjustable, a click on it (or its handles) edits it.
	if c.active != nil {
		if code := c.shapeHitTest(ev.Position); code != "" {
			c.actDrag = code
			c.actAnchor = c.bufPoint(ev.Position)
			c.actStart = *c.active
			return
		}
		c.commitActive() // clicked away → bake it, then start a new action below
	}

	x, y, inside := c.bufCoord(ev.Position)
	if !inside {
		return
	}

	if isVectorShape(c.ed.tool) {
		// Start a new editable shape; nothing is baked until it's committed.
		c.active = &editShape{
			kind: c.ed.tool, x0: x, y0: y, x1: x, y1: y,
			stroke: c.ed.col, fill: c.ed.fillCol, filled: c.ed.fill, strokeW: c.ed.size,
		}
		c.actDrag = "new"
		c.actAnchor = image.Pt(x, y)
		c.ed.dirty = true
		c.updateActive()
		canvas.Refresh(c) // register the just-shown preview objects promptly
		return
	}

	// Brush / highlight / blur rasterise immediately.
	c.pushUndo()
	c.drawing = true
	c.startX, c.startY, c.curX, c.curY = x, y, x, y
	c.brushPts = []image.Point{{X: x, Y: y}}
	if c.ed.tool == mkToolBlur {
		c.beginPreview()
		c.updatePreview()
	} else {
		c.applyBrush(c.brushPts) // initial dot
		c.refresh()
	}
}

func (c *markupCanvas) MouseUp(ev *desktop.MouseEvent) {
	if c.cropping {
		c.cropDrag = ""
		return
	}
	if c.active != nil && c.actDrag != "" {
		wasNew := c.actDrag == "new"
		c.actDrag = ""
		x0, y0, x1, y1 := c.active.bbox()
		if wasNew && (x1-x0 < 3 && y1-y0 < 3) {
			c.discardActive() // a click, not a drag
			return
		}
		c.updateActive() // finalise handles/preview for the placed shape
		return
	}
	if !c.drawing {
		return
	}
	c.drawing = false
	if c.ed.tool == mkToolBlur {
		c.hidePreview()
		c.applyShape() // bake the blur region
	}
	c.brushPts = nil
	c.refresh()
}

func (c *markupCanvas) MouseMoved(ev *desktop.MouseEvent) {
	if c.cropping {
		c.cropMouseMoved(ev.Position)
		return
	}
	if c.active != nil && c.actDrag != "" {
		c.dragActive(ev.Position)
		return
	}
	if !c.drawing {
		return
	}
	x, y, _ := c.bufCoord(ev.Position)
	c.curX, c.curY = x, y

	if c.ed.tool == mkToolBlur {
		c.updatePreview()
		return
	}
	last := c.brushPts[len(c.brushPts)-1]
	if dx, dy := x-last.X, y-last.Y; dx*dx+dy*dy >= 4 {
		np := image.Point{X: x, Y: y}
		c.applyBrush([]image.Point{last, np}) // only the new segment
		c.brushPts = append(c.brushPts, np)
		c.refresh() // enqueue-only; Fyne coalesces on the render thread
	}
}

// bufPoint maps a widget point to clamped buffer coords.
func (c *markupCanvas) bufPoint(p fyne.Position) image.Point {
	x := clampInt(int(p.X-c.fitX), 0, c.bufW)
	y := clampInt(int(p.Y-c.fitY), 0, c.bufH)
	return image.Pt(x, y)
}

func (c *markupCanvas) MouseIn(*desktop.MouseEvent) {}
func (c *markupCanvas) MouseOut()                   {}

// ─── blur region preview ───────────────────────────────────────────────────────
//
// Blur rasterises immediately (like the brush), so it uses a throwaway rectangle
// preview while dragging. Shapes are handled by the editable-shape code instead.

func (c *markupCanvas) beginPreview() {
	c.prevRect.StrokeColor = color.NRGBA{0xdd, 0xdd, 0xdd, 0xff}
	c.prevRect.StrokeWidth = 2
	c.prevRect.FillColor = color.NRGBA{0xff, 0xff, 0xff, 0x22}
	c.prevRect.Show()
}

func (c *markupCanvas) updatePreview() {
	x0 := c.fitX + float32(min(c.startX, c.curX))
	y0 := c.fitY + float32(min(c.startY, c.curY))
	c.prevRect.Move(fyne.NewPos(x0, y0))
	c.prevRect.Resize(fyne.NewSize(float32(absInt(c.curX-c.startX)), float32(absInt(c.curY-c.startY))))
	canvas.Refresh(c.prevRect)
}

func (c *markupCanvas) hidePreview() { c.prevRect.Hide() }

// applyBrush strokes just the given points into the buffer. It's called with a
// single point (the initial dot) or the latest 2-point segment — never the whole
// accumulated stroke, so cost per move is constant regardless of stroke length.
func (c *markupCanvas) applyBrush(pts []image.Point) {
	ed := c.ed
	col := ed.col
	soft := true
	if ed.tool == mkToolHighlight {
		col = color.NRGBA{ed.col.R, ed.col.G, ed.col.B, 0x60}
		soft = false
	}
	mkDrawBrush(c.buf, pts, col, max(ed.size, 1), soft)
}

// applyShape rasterises the dragged blur region into the buffer.
func (c *markupCanvas) applyShape() {
	ed := c.ed
	sz := ed.size*2 + 4
	src := c.blurSource()
	if ed.blurStyle == 0 {
		mkMosaicBlur(c.buf, src, c.startX, c.startY, c.curX, c.curY, sz)
	} else {
		mkBoxBlur(c.buf, src, c.startX, c.startY, c.curX, c.curY, sz)
	}
}

// blurSource is the background composited with the current annotations, so a
// blur samples whatever is visible (drawn strokes/shapes included), not just
// the clean background underneath.
func (c *markupCanvas) blurSource() *image.RGBA {
	comp := image.NewRGBA(c.bgScaled.Bounds())
	copy(comp.Pix, c.bgScaled.Pix)
	xdraw.Draw(comp, comp.Bounds(), c.buf, image.Point{}, xdraw.Over)
	return comp
}

// ─── editable shapes ───────────────────────────────────────────────────────────

// activeCodes are the handle codes for the active shape: rect/circle use all 8
// bounding-box handles, arrow uses just its two endpoints.
func (c *markupCanvas) activeCodes() []string {
	if c.active != nil && c.active.kind == mkToolArrow {
		return []string{"p0", "p1"}
	}
	return cropHandleCodes
}

func (c *markupCanvas) shapeHandlePos(code string) fyne.Position {
	s := c.active
	switch code {
	case "p0":
		return fyne.NewPos(c.fitX+float32(s.x0), c.fitY+float32(s.y0))
	case "p1":
		return fyne.NewPos(c.fitX+float32(s.x1), c.fitY+float32(s.y1))
	}
	bx0, by0, bx1, by1 := s.bbox()
	x0, y0 := c.fitX+float32(bx0), c.fitY+float32(by0)
	x1, y1 := c.fitX+float32(bx1), c.fitY+float32(by1)
	mx, my := (x0+x1)/2, (y0+y1)/2
	switch code {
	case "nw":
		return fyne.NewPos(x0, y0)
	case "n":
		return fyne.NewPos(mx, y0)
	case "ne":
		return fyne.NewPos(x1, y0)
	case "e":
		return fyne.NewPos(x1, my)
	case "se":
		return fyne.NewPos(x1, y1)
	case "s":
		return fyne.NewPos(mx, y1)
	case "sw":
		return fyne.NewPos(x0, y1)
	default: // "w"
		return fyne.NewPos(x0, my)
	}
}

// shapeHitTest returns the handle under p, "move" if on the shape body, else "".
func (c *markupCanvas) shapeHitTest(p fyne.Position) string {
	if c.active == nil {
		return ""
	}
	const hit = 12
	for _, code := range c.activeCodes() {
		hp := c.shapeHandlePos(code)
		if absF(p.X-hp.X) <= hit && absF(p.Y-hp.Y) <= hit {
			return code
		}
	}
	bx0, by0, bx1, by1 := c.active.bbox()
	if p.X >= c.fitX+float32(bx0)-4 && p.X <= c.fitX+float32(bx1)+4 &&
		p.Y >= c.fitY+float32(by0)-4 && p.Y <= c.fitY+float32(by1)+4 {
		return "move"
	}
	return ""
}

func (c *markupCanvas) dragActive(p fyne.Position) {
	s := c.active
	b := c.bufPoint(p)
	switch c.actDrag {
	case "new", "p1":
		s.x1, s.y1 = b.X, b.Y
	case "p0":
		s.x0, s.y0 = b.X, b.Y
	case "move":
		dx, dy := b.X-c.actAnchor.X, b.Y-c.actAnchor.Y
		w, h := c.actStart.x1-c.actStart.x0, c.actStart.y1-c.actStart.y0
		s.x0 = c.actStart.x0 + dx
		s.y0 = c.actStart.y0 + dy
		s.x1, s.y1 = s.x0+w, s.y0+h
	default: // rect/circle bbox handle
		bx0, by0, bx1, by1 := c.actStart.bbox()
		for _, ch := range c.actDrag {
			switch ch {
			case 'n':
				by0 = b.Y
			case 's':
				by1 = b.Y
			case 'w':
				bx0 = b.X
			case 'e':
				bx1 = b.X
			}
		}
		s.x0, s.y0, s.x1, s.y1 = bx0, by0, bx1, by1
	}
	c.updateActive()
}

// updateActive repositions the active shape's preview outline + selection handles.
func (c *markupCanvas) updateActive() {
	s := c.active
	if s == nil {
		return
	}
	c.prevRect.Hide()
	c.prevCircle.Hide()
	c.prevLine.Hide()
	c.prevHead1.Hide()
	c.prevHead2.Hide()

	sw := float32(max(s.strokeW, 2))
	fill := color.Color(color.Transparent)
	if s.filled {
		fill = s.fill
	}
	bx0, by0, bx1, by1 := s.bbox()
	x0, y0 := c.fitX+float32(bx0), c.fitY+float32(by0)
	w, h := float32(bx1-bx0), float32(by1-by0)

	switch s.kind {
	case mkToolRect:
		c.prevRect.StrokeColor = s.stroke
		c.prevRect.StrokeWidth = sw
		c.prevRect.FillColor = fill
		c.prevRect.Move(fyne.NewPos(x0, y0))
		c.prevRect.Resize(fyne.NewSize(w, h))
		c.prevRect.Show()
		canvas.Refresh(c.prevRect)
	case mkToolCircle:
		c.prevCircle.StrokeColor = s.stroke
		c.prevCircle.StrokeWidth = sw
		c.prevCircle.FillColor = fill
		c.prevCircle.Move(fyne.NewPos(x0, y0))
		c.prevCircle.Resize(fyne.NewSize(w, h))
		c.prevCircle.Show()
		canvas.Refresh(c.prevCircle)
	case mkToolArrow:
		p1 := fyne.NewPos(c.fitX+float32(s.x0), c.fitY+float32(s.y0))
		p2 := fyne.NewPos(c.fitX+float32(s.x1), c.fitY+float32(s.y1))
		c.prevLine.StrokeColor = s.stroke
		c.prevLine.StrokeWidth = sw
		c.prevLine.Position1, c.prevLine.Position2 = p1, p2
		c.prevLine.Show()
		canvas.Refresh(c.prevLine)
		c.setArrowHead(p1, p2, s.stroke, sw)
	}
	c.updateSelHandles()
}

func (c *markupCanvas) setArrowHead(p1, p2 fyne.Position, col color.NRGBA, sw float32) {
	dx, dy := p2.X-p1.X, p2.Y-p1.Y
	l := float32(math.Hypot(float64(dx), float64(dy)))
	if l < 1 {
		c.prevHead1.Hide()
		c.prevHead2.Hide()
		return
	}
	rx, ry := -dx/l, -dy/l // reverse direction (from head back along shaft)
	const ang = 0.5        // ~28°
	cosA, sinA := float32(math.Cos(ang)), float32(math.Sin(ang))
	hl := sw*2.2 + 8
	h1 := fyne.NewPos(p2.X+(rx*cosA-ry*sinA)*hl, p2.Y+(rx*sinA+ry*cosA)*hl)
	h2 := fyne.NewPos(p2.X+(rx*cosA+ry*sinA)*hl, p2.Y+(-rx*sinA+ry*cosA)*hl)
	for _, ln := range []*canvas.Line{c.prevHead1, c.prevHead2} {
		ln.StrokeColor = col
		ln.StrokeWidth = sw
		ln.Position1 = p2
	}
	c.prevHead1.Position2, c.prevHead2.Position2 = h1, h2
	c.prevHead1.Show()
	c.prevHead2.Show()
	canvas.Refresh(c.prevHead1)
	canvas.Refresh(c.prevHead2)
}

func (c *markupCanvas) updateSelHandles() {
	codes := c.activeCodes()
	const hs = 13
	for i := range c.selHandle {
		if i < len(codes) {
			hp := c.shapeHandlePos(codes[i])
			c.selHandle[i].Move(fyne.NewPos(hp.X-hs/2, hp.Y-hs/2))
			c.selHandle[i].Resize(fyne.NewSize(hs, hs))
			c.selHandle[i].Show()
			canvas.Refresh(c.selHandle[i])
		} else {
			c.selHandle[i].Hide()
		}
	}
}

func (c *markupCanvas) hideActive() {
	c.prevRect.Hide()
	c.prevCircle.Hide()
	c.prevLine.Hide()
	c.prevHead1.Hide()
	c.prevHead2.Hide()
	for _, h := range c.selHandle {
		h.Hide()
	}
	canvas.Refresh(c)
}

// commitActive bakes the active shape into the buffer (an undoable step).
func (c *markupCanvas) commitActive() {
	s := c.active
	if s == nil {
		return
	}
	c.active = nil
	c.actDrag = ""
	c.hideActive()
	c.pushUndo()
	r := max(s.strokeW/2, 1)
	bx0, by0, bx1, by1 := s.bbox()
	switch s.kind {
	case mkToolRect:
		mkDrawRect(c.buf, bx0, by0, bx1, by1, s.stroke, r, s.fill, s.filled)
	case mkToolCircle:
		mkDrawEllipse(c.buf, bx0, by0, bx1, by1, s.stroke, r, s.fill, s.filled)
	case mkToolArrow:
		mkDrawArrow(c.buf, s.x0, s.y0, s.x1, s.y1, s.stroke, max(s.strokeW/2, 2))
	}
	c.refresh()
}

func (c *markupCanvas) discardActive() {
	c.active = nil
	c.actDrag = ""
	c.hideActive()
}

// ─── transforms (rotate / flip / crop) ─────────────────────────────────────────

// applyTransform runs fn on both the base image and the markup buffer, then
// re-lays-out so the fit rectangle and buffers rebuild for the new dimensions.
func (c *markupCanvas) applyTransform(fn func(image.Image) *image.RGBA) {
	if c.buf == nil {
		return
	}
	c.pushTransformUndo()
	c.ed.full = fn(c.ed.full)
	c.buf = fn(c.buf)
	c.bgObj.Image = c.ed.full
	c.overlay.Image = c.buf
	b := c.ed.full.Bounds()
	c.iw, c.ih = b.Dx(), b.Dy()
	c.bufW, c.bufH = -1, -1 // force ensureBuffers to rescale to the new fit
	c.refreshKeepingHistory()
}

// refreshKeepingHistory re-lays-out after a size-changing commit (crop / rotate /
// flip) without letting ensureBuffers wipe the undo/redo stacks — the entry for
// this very operation was just pushed and must survive the re-layout.
func (c *markupCanvas) refreshKeepingHistory() {
	c.restoring = true
	c.Refresh()
	c.restoring = false
}

// ─── interactive crop ──────────────────────────────────────────────────────────

// enterCrop starts crop mode with the frame covering the whole image. The user
// drags the handles/frame to adjust, then confirms (Enter / Apply) or cancels.
func (c *markupCanvas) enterCrop() {
	if c.bufW < 1 || c.bufH < 1 {
		return
	}
	c.cropping = true
	c.cropDrag = ""
	c.cropRect = image.Rect(0, 0, c.bufW, c.bufH)
	for _, r := range c.cropDim {
		r.Show()
	}
	c.cropFrame.Show()
	for _, h := range c.cropHandle {
		h.Show()
	}
	for _, ln := range c.cropGrid {
		ln.Show()
	}
	c.Refresh() // re-layout: letterbox the image and place the crop overlay
}

func (c *markupCanvas) exitCrop() {
	c.cropping = false
	c.cropDrag = ""
	for _, r := range c.cropDim {
		r.Hide()
	}
	c.cropFrame.Hide()
	for _, h := range c.cropHandle {
		h.Hide()
	}
	for _, ln := range c.cropGrid {
		ln.Hide()
	}
	c.Refresh() // re-layout: restore the image to the full fit rect
}

// cropSX/cropSY convert buffer pixels to on-screen pixels (the image may be
// displayed smaller than its buffer resolution while cropping).
func (c *markupCanvas) cropSX() float32 {
	if c.bufW == 0 {
		return 1
	}
	return c.dispW / float32(c.bufW)
}
func (c *markupCanvas) cropSY() float32 {
	if c.bufH == 0 {
		return 1
	}
	return c.dispH / float32(c.bufH)
}

// cropHandlePos returns the widget-space centre of a handle for the given crop
// rectangle (in buffer coords).
func (c *markupCanvas) cropHandlePos(code string) fyne.Position {
	sx, sy := c.cropSX(), c.cropSY()
	x0 := c.dispX + float32(c.cropRect.Min.X)*sx
	y0 := c.dispY + float32(c.cropRect.Min.Y)*sy
	x1 := c.dispX + float32(c.cropRect.Max.X)*sx
	y1 := c.dispY + float32(c.cropRect.Max.Y)*sy
	mx, my := (x0+x1)/2, (y0+y1)/2
	switch code {
	case "nw":
		return fyne.NewPos(x0, y0)
	case "n":
		return fyne.NewPos(mx, y0)
	case "ne":
		return fyne.NewPos(x1, y0)
	case "e":
		return fyne.NewPos(x1, my)
	case "se":
		return fyne.NewPos(x1, y1)
	case "s":
		return fyne.NewPos(mx, y1)
	case "sw":
		return fyne.NewPos(x0, y1)
	default: // "w"
		return fyne.NewPos(x0, my)
	}
}

// cropHitTest returns the handle code under p, "move" if inside the frame, or "".
func (c *markupCanvas) cropHitTest(p fyne.Position) string {
	const hit = 14
	for _, code := range cropHandleCodes {
		hp := c.cropHandlePos(code)
		if absF(p.X-hp.X) <= hit && absF(p.Y-hp.Y) <= hit {
			return code
		}
	}
	sx, sy := c.cropSX(), c.cropSY()
	x0 := c.dispX + float32(c.cropRect.Min.X)*sx
	y0 := c.dispY + float32(c.cropRect.Min.Y)*sy
	x1 := c.dispX + float32(c.cropRect.Max.X)*sx
	y1 := c.dispY + float32(c.cropRect.Max.Y)*sy
	if p.X >= x0 && p.X <= x1 && p.Y >= y0 && p.Y <= y1 {
		return "move"
	}
	return ""
}

// cropBufPoint maps a widget point to buffer coords, clamped to the image.
func (c *markupCanvas) cropBufPoint(p fyne.Position) image.Point {
	x := clampInt(int((p.X-c.dispX)/c.cropSX()), 0, c.bufW)
	y := clampInt(int((p.Y-c.dispY)/c.cropSY()), 0, c.bufH)
	return image.Pt(x, y)
}

func (c *markupCanvas) cropMouseDown(p fyne.Position) {
	code := c.cropHitTest(p)
	c.cropAnchor = c.cropBufPoint(p)
	c.cropStart = c.cropRect
	if code == "" {
		// Empty space → start a fresh selection from here.
		code = "new"
		c.cropRect = image.Rectangle{Min: c.cropAnchor, Max: c.cropAnchor}
	}
	c.cropDrag = code
}

func (c *markupCanvas) cropMouseMoved(p fyne.Position) {
	if c.cropDrag == "" {
		return
	}
	b := c.cropBufPoint(p)
	r := c.cropRect
	switch c.cropDrag {
	case "new":
		r = image.Rectangle{Min: c.cropAnchor, Max: b}
	case "move":
		dx := b.X - c.cropAnchor.X
		dy := b.Y - c.cropAnchor.Y
		w, h := c.cropStart.Dx(), c.cropStart.Dy()
		nx := clampInt(c.cropStart.Min.X+dx, 0, c.bufW-w)
		ny := clampInt(c.cropStart.Min.Y+dy, 0, c.bufH-h)
		r = image.Rect(nx, ny, nx+w, ny+h)
	default:
		for _, ch := range c.cropDrag {
			switch ch {
			case 'n':
				r.Min.Y = b.Y
			case 's':
				r.Max.Y = b.Y
			case 'w':
				r.Min.X = b.X
			case 'e':
				r.Max.X = b.X
			}
		}
	}
	c.cropRect = normalizeCropRect(r, c.bufW, c.bufH)
	c.updateCropVisuals()
}

func normalizeCropRect(r image.Rectangle, bw, bh int) image.Rectangle {
	if r.Min.X > r.Max.X {
		r.Min.X, r.Max.X = r.Max.X, r.Min.X
	}
	if r.Min.Y > r.Max.Y {
		r.Min.Y, r.Max.Y = r.Max.Y, r.Min.Y
	}
	r.Min.X = clampInt(r.Min.X, 0, bw)
	r.Min.Y = clampInt(r.Min.Y, 0, bh)
	r.Max.X = clampInt(r.Max.X, 0, bw)
	r.Max.Y = clampInt(r.Max.Y, 0, bh)
	return r
}

func (c *markupCanvas) updateCropVisuals() {
	sx, sy := c.cropSX(), c.cropSY()
	dx, dy, dw, dh := c.dispX, c.dispY, c.dispW, c.dispH
	x0 := dx + float32(c.cropRect.Min.X)*sx
	y0 := dy + float32(c.cropRect.Min.Y)*sy
	x1 := dx + float32(c.cropRect.Max.X)*sx
	y1 := dy + float32(c.cropRect.Max.Y)*sy

	// Dark masks: top, bottom, left, right of the frame (within the image area).
	place := func(r *canvas.Rectangle, px, py, w, h float32) {
		if w < 0 {
			w = 0
		}
		if h < 0 {
			h = 0
		}
		r.Move(fyne.NewPos(px, py))
		r.Resize(fyne.NewSize(w, h))
		canvas.Refresh(r)
	}
	place(c.cropDim[0], dx, dy, dw, y0-dy)      // top
	place(c.cropDim[1], dx, y1, dw, dy+dh-y1)   // bottom
	place(c.cropDim[2], dx, y0, x0-dx, y1-y0)   // left
	place(c.cropDim[3], x1, y0, dx+dw-x1, y1-y0) // right

	c.cropFrame.Move(fyne.NewPos(x0, y0))
	c.cropFrame.Resize(fyne.NewSize(x1-x0, y1-y0))
	canvas.Refresh(c.cropFrame)

	// Rule-of-thirds grid inside the frame.
	fw, fh := x1-x0, y1-y0
	vx1, vx2 := x0+fw/3, x0+2*fw/3
	hy1, hy2 := y0+fh/3, y0+2*fh/3
	setLine := func(ln *canvas.Line, ax, ay, bx, by float32) {
		ln.Position1 = fyne.NewPos(ax, ay)
		ln.Position2 = fyne.NewPos(bx, by)
		canvas.Refresh(ln)
	}
	setLine(c.cropGrid[0], vx1, y0, vx1, y1)
	setLine(c.cropGrid[1], vx2, y0, vx2, y1)
	setLine(c.cropGrid[2], x0, hy1, x1, hy1)
	setLine(c.cropGrid[3], x0, hy2, x1, hy2)

	const hs = 15
	for i, code := range cropHandleCodes {
		hp := c.cropHandlePos(code)
		c.cropHandle[i].Move(fyne.NewPos(hp.X-hs/2, hp.Y-hs/2))
		c.cropHandle[i].Resize(fyne.NewSize(hs, hs))
		canvas.Refresh(c.cropHandle[i])
	}
}

// confirmCrop crops both the base image and buffer to the current crop frame.
func (c *markupCanvas) confirmCrop() {
	if !c.cropping {
		return
	}
	r := c.cropRect
	if r.Dx() < 8 || r.Dy() < 8 || c.bufW < 1 || c.bufH < 1 {
		c.exitCrop()
		return
	}
	c.exitCrop()
	c.pushTransformUndo()

	fb := c.ed.full.Bounds()
	fw, fh := fb.Dx(), fb.Dy()
	fr := image.Rect(
		fb.Min.X+r.Min.X*fw/c.bufW, fb.Min.Y+r.Min.Y*fh/c.bufH,
		fb.Min.X+r.Max.X*fw/c.bufW, fb.Min.Y+r.Max.Y*fh/c.bufH,
	)
	c.ed.full = cropImage(c.ed.full, fr)
	c.buf = cropImage(c.buf, r)
	c.bgObj.Image = c.ed.full
	c.overlay.Image = c.buf
	nb := c.ed.full.Bounds()
	c.iw, c.ih = nb.Dx(), nb.Dy()
	c.bufW, c.bufH = -1, -1
	c.refreshKeepingHistory()
}

func rotate90(src image.Image, cw bool) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w)) // dimensions swap
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px := src.At(b.Min.X+x, b.Min.Y+y)
			if cw {
				dst.Set(h-1-y, x, px)
			} else {
				dst.Set(y, w-1-x, px)
			}
		}
	}
	return dst
}

func flipHoriz(src image.Image) *image.RGBA { // mirror left↔right
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipVert(src image.Image) *image.RGBA { // flip top↕bottom
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func cropImage(src image.Image, r image.Rectangle) *image.RGBA {
	r = r.Intersect(src.Bounds())
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	xdraw.Draw(dst, dst.Bounds(), src, r.Min, xdraw.Src)
	return dst
}

func (c *markupCanvas) pushUndo() { c.pushUndoEntry(undoEntry{buf: cloneRGBA(c.buf)}) }

// pushTransformUndo snapshots the base image + buffer + dims before a transform.
func (c *markupCanvas) pushTransformUndo() {
	c.pushUndoEntry(undoEntry{buf: cloneRGBA(c.buf), full: c.ed.full, iw: c.iw, ih: c.ih})
}

func (c *markupCanvas) pushUndoEntry(e undoEntry) {
	c.ed.dirty = true // any snapshot means an edit is being made
	c.undo = append(c.undo, e)
	if len(c.undo) > 24 {
		c.undo = c.undo[len(c.undo)-24:]
	}
	c.redo = nil // a fresh edit invalidates the redo history
}

// snapshot captures the current state, matching whether transform-level info
// (base image + dimensions) is needed to reverse the paired entry.
func (c *markupCanvas) snapshot(withFull bool) undoEntry {
	if withFull {
		return undoEntry{buf: cloneRGBA(c.buf), full: c.ed.full, iw: c.iw, ih: c.ih}
	}
	return undoEntry{buf: cloneRGBA(c.buf)}
}

// restore reinstates the state captured in e.
func (c *markupCanvas) restore(e undoEntry) {
	c.restoring = true
	defer func() { c.restoring = false }()
	if e.full != nil {
		c.ed.full = e.full
		c.bgObj.Image = e.full
		c.buf = e.buf
		c.overlay.Image = e.buf
		c.iw, c.ih = e.iw, e.ih
		c.bufW, c.bufH = -1, -1 // force ensureBuffers to rebuild at the stored size
		c.Refresh()
		return
	}
	copy(c.buf.Pix, e.buf.Pix)
	c.refresh()
}

func (c *markupCanvas) undoLast() {
	// An in-progress (unbaked) shape is cancelled by the first undo.
	if c.active != nil {
		c.discardActive()
		return
	}
	if len(c.undo) == 0 {
		return
	}
	if c.cropping {
		c.exitCrop()
	}
	e := c.undo[len(c.undo)-1]
	c.undo = c.undo[:len(c.undo)-1]
	c.redo = append(c.redo, c.snapshot(e.full != nil))
	c.restore(e)
}

func (c *markupCanvas) redoLast() {
	if len(c.redo) == 0 {
		return
	}
	if c.cropping {
		c.exitCrop()
	}
	e := c.redo[len(c.redo)-1]
	c.redo = c.redo[:len(c.redo)-1]
	c.undo = append(c.undo, c.snapshot(e.full != nil))
	c.restore(e)
}

func (c *markupCanvas) refresh() { canvas.Refresh(c.overlay) }

type markupCanvasRenderer struct {
	c    *markupCanvas
	objs []fyne.CanvasObject
}

func (r *markupCanvasRenderer) Layout(size fyne.Size) {
	c := r.c
	c.darkBg.Resize(size)
	c.darkBg.Move(fyne.NewPos(0, 0))

	// Contain-fit the image within the actual allotted area.
	if c.iw == 0 || c.ih == 0 || size.Width < 1 || size.Height < 1 {
		return
	}
	scale := size.Width / float32(c.iw)
	if s := size.Height / float32(c.ih); s < scale {
		scale = s
	}
	c.fitW = float32(c.iw) * scale
	c.fitH = float32(c.ih) * scale
	c.fitX = (size.Width - c.fitW) / 2
	c.fitY = (size.Height - c.fitH) / 2

	c.ensureBuffers(int(c.fitW), int(c.fitH))

	// The image is displayed at the fit rect normally, but letterboxed (padded)
	// in crop mode so the crop handles sit comfortably inside the window rather
	// than under the toolbar or off the edges. The buffer resolution is unchanged.
	c.dispX, c.dispY, c.dispW, c.dispH = c.fitX, c.fitY, c.fitW, c.fitH
	if c.cropping {
		c.dispX, c.dispY, c.dispW, c.dispH = c.computeCropDisplay(size)
	}
	pos := fyne.NewPos(c.dispX, c.dispY)
	sz := fyne.NewSize(c.dispW, c.dispH)
	c.bgObj.Move(pos)
	c.bgObj.Resize(sz)
	c.overlay.Move(pos)
	c.overlay.Resize(sz)
	canvas.Refresh(c.bgObj)
	canvas.Refresh(c.overlay)
	if c.cropping {
		c.updateCropVisuals()
	}
}

// computeCropDisplay contain-fits the image inside the widget inset by an even
// padding on all sides, so the crop handles sit just inside the window edges.
func (c *markupCanvas) computeCropDisplay(size fyne.Size) (x, y, w, h float32) {
	const pad = 48
	availW := size.Width - 2*pad
	availH := size.Height - 2*pad
	if availW < 40 {
		availW = size.Width
	}
	if availH < 40 {
		availH = size.Height
	}
	s := availW / float32(c.iw)
	if v := availH / float32(c.ih); v < s {
		s = v
	}
	w, h = float32(c.iw)*s, float32(c.ih)*s
	x = pad + (availW-w)/2
	y = pad + (availH-h)/2
	return
}
func (r *markupCanvasRenderer) MinSize() fyne.Size { return fyne.NewSize(320, 240) }

// Refresh re-runs Layout so transforms (which change the image and its size)
// recompute the fit and rebuild the buffers.
func (r *markupCanvasRenderer) Refresh() { r.Layout(r.c.Size()) }
func (r *markupCanvasRenderer) Destroy()                     {}
func (r *markupCanvasRenderer) Objects() []fyne.CanvasObject { return r.objs }

// ─── save / backup / revert ───────────────────────────────────────────────────

// backupAndCompose backs the original up to "<path>.orig" (once), then writes the
// composited (image + markup) result over path.
func backupAndCompose(path string, full image.Image, buf *image.RGBA) (string, error) {
	orig := path + ".orig"
	if _, err := os.Stat(orig); os.IsNotExist(err) {
		if err := copyFile(path, orig); err != nil {
			return "", err
		}
	}
	if err := saveComposite(full, buf, path); err != nil {
		return "", err
	}
	return path, nil
}

func hasEditBackup(path string) bool {
	_, err := os.Stat(path + ".orig")
	return err == nil
}

// revertEdit restores the backed-up original over path and removes the backup.
func revertEdit(path string) error {
	orig := path + ".orig"
	if _, err := os.Stat(orig); err != nil {
		return err
	}
	if err := copyFile(orig, path); err != nil {
		return err
	}
	return os.Remove(orig)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}


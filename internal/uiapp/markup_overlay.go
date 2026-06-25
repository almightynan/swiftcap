package uiapp

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// ─── types and constants ─────────────────────────────────────────────────────

type markupTool int

const (
	mkToolBrush     markupTool = 0
	mkToolHighlight markupTool = 1
	mkToolRect      markupTool = 2
	mkToolCircle    markupTool = 3
	mkToolBlur      markupTool = 4
)

var mkToolLabels = [5]string{"Brush", "Highlight", "Rect", "Circle", "Blur"}

// hit codes for hitMarkupBar
const (
	mkHitNone         = -1
	mkHitTool0        = 0  // brush
	mkHitTool1        = 1  // highlight
	mkHitTool2        = 2  // rect
	mkHitTool3        = 3  // circle
	mkHitTool4        = 4  // blur
	mkHitColor        = 10 // stroke color swatch
	mkHitSizeMinus    = 11
	mkHitSizePlus     = 12
	mkHitFillTog      = 13 // fill toggle button
	mkHitFillColor    = 14 // fill color swatch
	mkHitBlur0        = 20 // mosaic
	mkHitBlur1        = 21 // smooth
	mkHitCapture      = 30
	mkHitUndo         = 31
	mkHitPalBase      = 100 // +index → palette for stroke color
	mkHitPalFillBase  = 200 // +index → palette for fill color
)

var mkPaletteColors = [12]color.NRGBA{
	{0xff, 0x33, 0x33, 0xff}, // red
	{0xff, 0x88, 0x00, 0xff}, // orange
	{0xff, 0xee, 0x11, 0xff}, // yellow
	{0x22, 0xcc, 0x55, 0xff}, // green
	{0x22, 0x99, 0xff, 0xff}, // sky blue
	{0x88, 0x44, 0xff, 0xff}, // purple
	{0xff, 0x44, 0xcc, 0xff}, // pink
	{0x00, 0xee, 0xdd, 0xff}, // cyan
	{0xff, 0xff, 0xff, 0xff}, // white
	{0xaa, 0xaa, 0xaa, 0xff}, // light grey
	{0x44, 0x44, 0x44, 0xff}, // dark grey
	{0x00, 0x00, 0x00, 0xff}, // black
}

// toolbar layout
const (
	mkBarH       = float32(76)
	mkBtnW       = float32(82)
	mkBtnH       = float32(58)
	mkBtnGap     = float32(6)
	mkBarPadX    = float32(10)
	mkSwatchSz   = float32(28)
	mkStepperW   = float32(26)
	mkCaptureW   = float32(132)
	mkUndoW      = float32(70)
)

// ─── geometry helpers ─────────────────────────────────────────────────────────

func (w *regionOverlayWidget) mkBarTopY(sz fyne.Size) float32 { return sz.Height - mkBarH }
func (w *regionOverlayWidget) mkBtnX(i int) float32 {
	return mkBarPadX + float32(i)*(mkBtnW+mkBtnGap)
}
func (w *regionOverlayWidget) mkOptionsX() float32 {
	return mkBarPadX + 5*(mkBtnW+mkBtnGap) + 12
}

// mkGeom computes all x-positions for the options bar.  Returns absolute x coords.
func (w *regionOverlayWidget) mkGeom(sz fyne.Size) (optX, colorX, sizeMinX, sizeValX, sizePlusX, toolOptX, undoX, captureX float32) {
	optX = w.mkOptionsX()
	colorX = optX + 36    // swatch after "Color" label
	sizeMinX = colorX + mkSwatchSz + 14
	sizeValX = sizeMinX + mkStepperW + 4
	sizePlusX = sizeValX + 30
	toolOptX = sizePlusX + mkStepperW + 14
	undoX = sz.Width - mkBarPadX - mkCaptureW - 8 - mkUndoW
	captureX = sz.Width - mkBarPadX - mkCaptureW
	return
}

// ─── hit testing ─────────────────────────────────────────────────────────────

func (w *regionOverlayWidget) hitMarkupBar(pos fyne.Position, sz fyne.Size) int {
	barTopY := w.mkBarTopY(sz)

	// Palette popup is rendered ABOVE the toolbar
	w.mu.Lock()
	showPal := w.showPalette
	palForFill := w.palForFill
	tool := w.markTool
	fill := w.markFill
	w.mu.Unlock()

	if showPal {
		_, colorX, _, _, _, _, _, _ := w.mkGeom(sz)
		var swX float32
		if palForFill {
			// fill swatch position
			_, _, _, _, _, toolOptX, _, _ := w.mkGeom(sz)
			swX = toolOptX + 58
		} else {
			swX = colorX
		}
		palX := swX
		palY := barTopY - 90

		for i := 0; i < 12; i++ {
			col := i % 4
			row := i / 4
			px := palX + float32(col)*30
			py := palY + float32(row)*30
			if pos.X >= px && pos.X < px+26 && pos.Y >= py && pos.Y < py+26 {
				if palForFill {
					return mkHitPalFillBase + i
				}
				return mkHitPalBase + i
			}
		}
		return mkHitNone // click outside palette = close it
	}

	if pos.Y < barTopY {
		return mkHitNone
	}
	relY := pos.Y - barTopY

	// Tool buttons
	for i := 0; i < 5; i++ {
		bx := w.mkBtnX(i)
		btnTopY := (mkBarH - mkBtnH) / 2
		if pos.X >= bx && pos.X < bx+mkBtnW && relY >= btnTopY && relY < btnTopY+mkBtnH {
			return mkHitTool0 + i
		}
	}

	_, colorX, sizeMinX, sizeValX, sizePlusX, toolOptX, undoX, captureX := w.mkGeom(sz)
	_ = sizeValX

	// Color swatch
	swMidY := mkBarH / 2
	if pos.X >= colorX && pos.X < colorX+mkSwatchSz &&
		pos.Y >= barTopY+swMidY-mkSwatchSz/2 && pos.Y < barTopY+swMidY+mkSwatchSz/2 {
		return mkHitColor
	}

	// Size stepper [-]
	stepY := mkBarH / 2
	if pos.X >= sizeMinX && pos.X < sizeMinX+mkStepperW &&
		pos.Y >= barTopY+stepY-12 && pos.Y < barTopY+stepY+12 {
		return mkHitSizeMinus
	}
	// Size stepper [+]
	if pos.X >= sizePlusX && pos.X < sizePlusX+mkStepperW &&
		pos.Y >= barTopY+stepY-12 && pos.Y < barTopY+stepY+12 {
		return mkHitSizePlus
	}

	// Tool-specific options
	switch tool {
	case mkToolRect, mkToolCircle:
		// Fill toggle
		fillTogW := float32(52)
		if pos.X >= toolOptX && pos.X < toolOptX+fillTogW &&
			pos.Y >= barTopY+18 && pos.Y < barTopY+mkBarH-18 {
			return mkHitFillTog
		}
		// Fill swatch
		if fill {
			fsX := toolOptX + fillTogW + 8
			if pos.X >= fsX && pos.X < fsX+mkSwatchSz &&
				pos.Y >= barTopY+swMidY-mkSwatchSz/2 && pos.Y < barTopY+swMidY+mkSwatchSz/2 {
				return mkHitFillColor
			}
		}
	case mkToolBlur:
		bw2 := float32(66)
		for i := 0; i < 2; i++ {
			bx2 := toolOptX + float32(i)*(bw2+6)
			if pos.X >= bx2 && pos.X < bx2+bw2 &&
				pos.Y >= barTopY+18 && pos.Y < barTopY+mkBarH-18 {
				return mkHitBlur0 + i
			}
		}
	}

	// Undo
	if pos.X >= undoX && pos.X < undoX+mkUndoW &&
		pos.Y >= barTopY+14 && pos.Y < barTopY+mkBarH-14 {
		return mkHitUndo
	}
	// Capture
	if pos.X >= captureX && pos.X < captureX+mkCaptureW &&
		pos.Y >= barTopY+14 && pos.Y < barTopY+mkBarH-14 {
		return mkHitCapture
	}

	return mkHitNone
}

// ─── mouse handlers ──────────────────────────────────────────────────────────

func (w *regionOverlayWidget) handleMarkupMouseDown(pos fyne.Position) {
	sz := w.Size()
	hit := w.hitMarkupBar(pos, sz)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.done {
		return
	}

	// Close palette on any click
	if w.showPalette {
		switch {
		case hit >= mkHitPalBase && hit < mkHitPalBase+12:
			w.markColor = mkPaletteColors[hit-mkHitPalBase]
		case hit >= mkHitPalFillBase && hit < mkHitPalFillBase+12:
			w.markFillCol = mkPaletteColors[hit-mkHitPalFillBase]
		}
		w.showPalette = false
		return
	}

	switch {
	case hit >= mkHitTool0 && hit <= mkHitTool4:
		w.markTool = markupTool(hit)
		return

	case hit == mkHitColor:
		w.showPalette = true
		w.palForFill = false
		return

	case hit == mkHitSizeMinus:
		if w.markSize > 1 {
			w.markSize--
		}
		return

	case hit == mkHitSizePlus:
		if w.markSize < 30 {
			w.markSize++
		}
		return

	case hit == mkHitFillTog:
		w.markFill = !w.markFill
		return

	case hit == mkHitFillColor:
		w.showPalette = true
		w.palForFill = true
		return

	case hit == mkHitBlur0:
		w.markBlurType = 0
		return

	case hit == mkHitBlur1:
		w.markBlurType = 1
		return

	case hit == mkHitUndo:
		// handled below (needs unlock)
		n := len(w.mkUndoBufs)
		if n > 0 && w.mkBuf != nil {
			prev := w.mkUndoBufs[n-1]
			w.mkUndoBufs = w.mkUndoBufs[:n-1]
			w.mkBuf = prev
		}
		return

	case hit == mkHitCapture:
		// handled below (needs unlock + goroutine)
		if !w.done {
			w.done = true
			buf := w.mkBuf
			bgImg := w.bgImage
			onDone := w.onDone
			go func() {
				tmpOut := fmt.Sprintf("/tmp/swiftcap_markup_%d.png", time.Now().UnixNano())
				var err error
				if bgImg != nil && buf != nil {
					err = saveComposite(bgImg, buf, tmpOut)
				} else if buf != nil {
					// no bg, just save the markup buffer
					err = saveComposite(image.NewRGBA(image.Rect(0, 0, buf.Bounds().Dx(), buf.Bounds().Dy())), buf, tmpOut)
				}
				if err != nil {
					if onDone != nil {
						onDone("")
					}
					return
				}
				if onDone != nil {
					onDone("file:" + tmpOut)
				}
			}()
		}
		return

	case hit == mkHitNone:
		// Canvas click — start drawing
		if w.mkBuf != nil {
			// save undo snapshot
			snap := image.NewRGBA(w.mkBuf.Bounds())
			copy(snap.Pix, w.mkBuf.Pix)
			const maxUndo = 20
			w.mkUndoBufs = append(w.mkUndoBufs, snap)
			if len(w.mkUndoBufs) > maxUndo {
				w.mkUndoBufs = w.mkUndoBufs[len(w.mkUndoBufs)-maxUndo:]
			}
		}
		w.mkDrawing = true
		w.mkStartX = pos.X
		w.mkStartY = pos.Y
		w.mkCurX = pos.X
		w.mkCurY = pos.Y
		if w.markTool == mkToolBrush || w.markTool == mkToolHighlight {
			w.mkPoints = w.mkPoints[:0]
			w.mkPoints = append(w.mkPoints, image.Pt(int(pos.X), int(pos.Y)))
		}
	}
}

func (w *regionOverlayWidget) handleMarkupMouseMoved(pos fyne.Position) {
	// hitMarkupBar acquires w.mu internally, so call it before locking here —
	// sync.Mutex is not reentrant and double-locking deadlocks the main goroutine.
	sz := w.Size()
	hit := w.hitMarkupBar(pos, sz)

	w.mu.Lock()
	w.mouseX = pos.X
	w.mouseY = pos.Y
	w.mkHoverCode = hit

	if w.mkDrawing && !w.done {
		w.mkCurX = pos.X
		w.mkCurY = pos.Y
		if w.markTool == mkToolBrush || w.markTool == mkToolHighlight {
			if len(w.mkPoints) > 0 {
				last := w.mkPoints[len(w.mkPoints)-1]
				dx := int(pos.X) - last.X
				dy := int(pos.Y) - last.Y
				if dx*dx+dy*dy >= 4 {
					w.mkPoints = append(w.mkPoints, image.Pt(int(pos.X), int(pos.Y)))
				}
			}
		}
	}
	w.mu.Unlock()
	w.Refresh()
}

func (w *regionOverlayWidget) handleMarkupMouseUp(pos fyne.Position) {
	w.mu.Lock()
	if !w.mkDrawing || w.done {
		w.mu.Unlock()
		return
	}
	w.mkDrawing = false
	tool := w.markTool
	x0 := int(w.mkStartX)
	y0 := int(w.mkStartY)
	x1 := int(w.mkCurX)
	y1 := int(w.mkCurY)
	pts := append([]image.Point(nil), w.mkPoints...)
	col := w.markColor
	size := w.markSize
	fill := w.markFill
	fillCol := w.markFillCol
	blurType := w.markBlurType
	buf := w.mkBuf
	bgImg := w.bgImage
	w.mkPoints = w.mkPoints[:0]
	w.mu.Unlock()

	if buf == nil {
		return
	}

	// Commit the shape/blur into the buffer (brush is already committed incrementally)
	switch tool {
	case mkToolRect:
		r := size / 2
		if r < 1 {
			r = 1
		}
		mkDrawRect(buf, x0, y0, x1, y1, col, r, fillCol, fill)

	case mkToolCircle:
		r := size / 2
		if r < 1 {
			r = 1
		}
		mkDrawEllipse(buf, x0, y0, x1, y1, col, r, fillCol, fill)

	case mkToolBlur:
		if bgImg != nil {
			blurSz := size*2 + 4
			if blurType == 0 {
				mkMosaicBlur(buf, bgImg, x0, y0, x1, y1, blurSz)
			} else {
				mkBoxBlur(buf, bgImg, x0, y0, x1, y1, blurSz)
			}
		}

	case mkToolBrush, mkToolHighlight:
		// Already drawn incrementally in the renderer's paintMarkupMode;
		// copy committed state is already in buf at this point.
		_ = pts
	}

	w.Refresh()
}

// ─── markup rendering ─────────────────────────────────────────────────────────

func (r *regionOverlayRenderer) initMarkupObjects() {
	w := r.w

	// Markup raster
	r.mkBufRaster = canvas.NewRaster(func(rw, rh int) image.Image {
		w.mu.Lock()
		buf := w.mkBuf
		w.mu.Unlock()
		if buf == nil {
			return image.NewRGBA(image.Rect(0, 0, 1, 1))
		}
		return buf
	})

	// Bottom toolbar background
	r.mkBarBg = canvas.NewRectangle(color.NRGBA{0x18, 0x18, 0x18, 0xf0})
	r.mkBarBg.StrokeColor = color.NRGBA{0x38, 0x38, 0x38, 0xff}
	r.mkBarBg.StrokeWidth = 1

	// Tool buttons
	for i := range r.mkToolBg {
		bg := canvas.NewRectangle(color.Transparent)
		bg.CornerRadius = 6
		r.mkToolBg[i] = bg
		lbl := canvas.NewText(mkToolLabels[i], color.NRGBA{0xbb, 0xbb, 0xbb, 0xff})
		lbl.TextSize = 12
		lbl.Alignment = fyne.TextAlignCenter
		r.mkToolLbl[i] = lbl
	}

	// Color swatch
	r.mkColorLbl = canvas.NewText("Color", color.NRGBA{0x88, 0x88, 0x88, 0xff})
	r.mkColorLbl.TextSize = 11
	r.mkColorSwatch = canvas.NewRectangle(color.NRGBA{0xff, 0x33, 0x33, 0xff})
	r.mkColorSwatch.CornerRadius = 4
	r.mkColorSwatch.StrokeColor = color.NRGBA{0x66, 0x66, 0x66, 0xff}
	r.mkColorSwatch.StrokeWidth = 1

	// Size stepper
	r.mkSizeLbl = canvas.NewText("4", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	r.mkSizeLbl.TextSize = 13
	r.mkSizeLbl.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	r.mkSizeLbl.Alignment = fyne.TextAlignCenter
	r.mkSizeMinBg = canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	r.mkSizeMinBg.CornerRadius = 4
	r.mkSizePlusBg = canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	r.mkSizePlusBg.CornerRadius = 4
	r.mkSizeMinLbl = canvas.NewText("−", color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
	r.mkSizeMinLbl.TextSize = 15
	r.mkSizeMinLbl.Alignment = fyne.TextAlignCenter
	r.mkSizePlusLbl = canvas.NewText("+", color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
	r.mkSizePlusLbl.TextSize = 15
	r.mkSizePlusLbl.Alignment = fyne.TextAlignCenter

	// Fill toggle + fill swatch
	r.mkFillTogBg = canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	r.mkFillTogBg.CornerRadius = 4
	r.mkFillTogLbl = canvas.NewText("Fill", color.NRGBA{0xbb, 0xbb, 0xbb, 0xff})
	r.mkFillTogLbl.TextSize = 12
	r.mkFillTogLbl.Alignment = fyne.TextAlignCenter
	r.mkFillLbl = canvas.NewText("Fill color", color.NRGBA{0x77, 0x77, 0x77, 0xff})
	r.mkFillLbl.TextSize = 11
	r.mkFillSwatch = canvas.NewRectangle(color.NRGBA{0xff, 0x33, 0x33, 0x66})
	r.mkFillSwatch.CornerRadius = 4
	r.mkFillSwatch.StrokeColor = color.NRGBA{0x66, 0x66, 0x66, 0xff}
	r.mkFillSwatch.StrokeWidth = 1

	// Blur type buttons
	blurLabels := [2]string{"Mosaic", "Smooth"}
	for i := range r.mkBlurBg {
		bg := canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
		bg.CornerRadius = 4
		r.mkBlurBg[i] = bg
		lbl := canvas.NewText(blurLabels[i], color.NRGBA{0xbb, 0xbb, 0xbb, 0xff})
		lbl.TextSize = 12
		lbl.Alignment = fyne.TextAlignCenter
		r.mkBlurLbl[i] = lbl
	}

	// Undo button
	r.mkUndoBg = canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	r.mkUndoBg.CornerRadius = 6
	r.mkUndoBg.StrokeColor = color.NRGBA{0x44, 0x44, 0x44, 0xff}
	r.mkUndoBg.StrokeWidth = 1
	r.mkUndoLbl = canvas.NewText("Undo", color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
	r.mkUndoLbl.TextSize = 12
	r.mkUndoLbl.Alignment = fyne.TextAlignCenter

	// Capture button
	r.mkCaptureBg = canvas.NewRectangle(color.NRGBA{0x16, 0x70, 0xe8, 0xff})
	r.mkCaptureBg.CornerRadius = 8
	r.mkCaptureLbl = canvas.NewText("Save Screenshot", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	r.mkCaptureLbl.TextSize = 13
	r.mkCaptureLbl.TextStyle = fyne.TextStyle{Bold: true}
	r.mkCaptureLbl.Alignment = fyne.TextAlignCenter

	// Palette popup
	r.mkPalBg = canvas.NewRectangle(color.NRGBA{0x22, 0x22, 0x22, 0xf4})
	r.mkPalBg.CornerRadius = 6
	r.mkPalBg.StrokeColor = color.NRGBA{0x44, 0x44, 0x44, 0xff}
	r.mkPalBg.StrokeWidth = 1
	for i := range r.mkPalSwatch {
		sw := canvas.NewRectangle(mkPaletteColors[i])
		sw.CornerRadius = 3
		sw.StrokeColor = color.NRGBA{0x55, 0x55, 0x55, 0xff}
		sw.StrokeWidth = 1
		r.mkPalSwatch[i] = sw
	}

	// Preview objects
	r.mkPreviewRect = canvas.NewRectangle(color.Transparent)
	r.mkPreviewRect.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0x88}
	r.mkPreviewRect.StrokeWidth = 1
	r.mkPreviewCircle = canvas.NewCircle(color.Transparent)
	r.mkPreviewCircle.StrokeColor = color.NRGBA{0xff, 0xff, 0xff, 0x88}
	r.mkPreviewCircle.StrokeWidth = 1
	r.mkPreviewLine = canvas.NewLine(color.NRGBA{0xff, 0xff, 0xff, 0x88})
	r.mkPreviewLine.StrokeWidth = 1
}

func (r *regionOverlayRenderer) paintMarkupMode(sz fyne.Size) {
	w := r.w
	w.mu.Lock()
	tool := w.markTool
	col := w.markColor
	size := w.markSize
	fill := w.markFill
	fillCol := w.markFillCol
	blurType := w.markBlurType
	drawing := w.mkDrawing
	startX, startY := w.mkStartX, w.mkStartY
	curX, curY := w.mkCurX, w.mkCurY
	showPal := w.showPalette
	palForFill := w.palForFill
	pts := append([]image.Point(nil), w.mkPoints...)
	hoverCode := w.mkHoverCode
	hasUndo := len(w.mkUndoBufs) > 0

	// Ensure markup buffer exists
	bw := intMax(int(sz.Width), 1)
	bh := intMax(int(sz.Height), 1)
	if w.mkBuf == nil || w.mkBufW != bw || w.mkBufH != bh {
		newBuf := image.NewRGBA(image.Rect(0, 0, bw, bh))
		if w.mkBuf != nil && w.mkBufW == bw && w.mkBufH == bh {
			copy(newBuf.Pix, w.mkBuf.Pix)
		}
		w.mkBuf = newBuf
		w.mkBufW = bw
		w.mkBufH = bh
		r.mkLastPtN = 0
	}

	// Draw new brush/highlight points incrementally
	if (tool == mkToolBrush || tool == mkToolHighlight) && len(pts) > r.mkLastPtN {
		drawCol := col
		if tool == mkToolHighlight {
			drawCol = color.NRGBA{col.R, col.G, col.B, 0x60} // ~38% opacity
		}
		soft := tool == mkToolBrush // brush uses soft edge
		radius := size
		if radius < 1 {
			radius = 1
		}
		mkDrawBrush(w.mkBuf, pts[r.mkLastPtN:], drawCol, radius, soft)
		r.mkLastPtN = len(pts)
	}
	if !drawing {
		r.mkLastPtN = 0
	}

	w.mu.Unlock()

	zero := fyne.NewSize(0, 0)

	// ── Hide snip-mode UI ──────────────────────────────────────────────────────
	r.dimTop.Resize(zero)
	r.dimBot.Resize(zero)
	r.dimLeft.Resize(zero)
	r.dimRight.Resize(zero)
	r.selRect.Resize(zero)
	r.sizeBg.Resize(zero)
	r.sizeText.Text = ""
	for _, h := range r.handles {
		h.Resize(zero)
	}
	r.freeformRaster.Hide()
	r.helpMain.Hide()
	r.helpSub.Hide()
	r.magShadow.Resize(zero)
	r.magRaster.Resize(zero)
	r.magBorder.Resize(zero)
	r.coordBg.Resize(zero)
	r.instrText.Text = "ESC to cancel  ·  Use toolbar below to annotate"
	r.instrText.Move(fyne.NewPos(0, sz.Height-mkBarH-22))
	r.instrText.Resize(fyne.NewSize(sz.Width, 18))

	// ── Markup buffer raster ──────────────────────────────────────────────────
	r.mkBufRaster.Move(fyne.NewPos(0, 0))
	r.mkBufRaster.Resize(sz)
	r.mkBufRaster.Show()
	r.mkBufRaster.Refresh()

	// ── Shape preview during drag ─────────────────────────────────────────────
	minX := float32Min(startX, curX)
	minY := float32Min(startY, curY)
	maxX := float32Max(startX, curX)
	maxY := float32Max(startY, curY)

	switch {
	case drawing && (tool == mkToolRect || tool == mkToolBlur):
		r.mkPreviewRect.Move(fyne.NewPos(minX, minY))
		r.mkPreviewRect.Resize(fyne.NewSize(maxX-minX, maxY-minY))
		r.mkPreviewRect.Show()
		r.mkPreviewCircle.Resize(zero)
		r.mkPreviewLine.Position1 = fyne.NewPos(0, 0)
		r.mkPreviewLine.Position2 = fyne.NewPos(0, 0)
	case drawing && tool == mkToolCircle:
		r.mkPreviewCircle.Move(fyne.NewPos(minX, minY))
		r.mkPreviewCircle.Resize(fyne.NewSize(maxX-minX, maxY-minY))
		r.mkPreviewCircle.Show()
		r.mkPreviewRect.Resize(zero)
		r.mkPreviewLine.Position1 = fyne.NewPos(0, 0)
		r.mkPreviewLine.Position2 = fyne.NewPos(0, 0)
	default:
		r.mkPreviewRect.Resize(zero)
		r.mkPreviewCircle.Resize(zero)
		r.mkPreviewLine.Position1 = fyne.NewPos(0, 0)
		r.mkPreviewLine.Position2 = fyne.NewPos(0, 0)
	}

	// ── Bottom toolbar ────────────────────────────────────────────────────────
	barTopY := w.mkBarTopY(sz)
	r.mkBarBg.Move(fyne.NewPos(0, barTopY))
	r.mkBarBg.Resize(fyne.NewSize(sz.Width, mkBarH))

	btnTopY := barTopY + (mkBarH-mkBtnH)/2

	for i := 0; i < 5; i++ {
		bx := w.mkBtnX(i)
		isSelected := markupTool(i) == tool
		isHover := hoverCode == mkHitTool0+i

		if isSelected {
			r.mkToolBg[i].FillColor = color.NRGBA{0x26, 0x60, 0xd0, 0xff}
		} else if isHover {
			r.mkToolBg[i].FillColor = color.NRGBA{0x33, 0x33, 0x33, 0xff}
		} else {
			r.mkToolBg[i].FillColor = color.NRGBA{0x22, 0x22, 0x22, 0xff}
		}
		r.mkToolBg[i].Move(fyne.NewPos(bx, btnTopY))
		r.mkToolBg[i].Resize(fyne.NewSize(mkBtnW, mkBtnH))

		if isSelected {
			r.mkToolLbl[i].Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			r.mkToolLbl[i].TextStyle = fyne.TextStyle{Bold: true}
		} else {
			r.mkToolLbl[i].Color = color.NRGBA{0xbb, 0xbb, 0xbb, 0xff}
			r.mkToolLbl[i].TextStyle = fyne.TextStyle{}
		}
		r.mkToolLbl[i].Move(fyne.NewPos(bx, btnTopY+mkBtnH/2-6))
		r.mkToolLbl[i].Resize(fyne.NewSize(mkBtnW, 18))
	}

	// ── Options section ────────────────────────────────────────────────────────
	_, colorX, sizeMinX, sizeValX, sizePlusX, toolOptX, undoX, captureX := w.mkGeom(sz)
	midY := barTopY + mkBarH/2

	// Color swatch + label
	r.mkColorLbl.Move(fyne.NewPos(w.mkOptionsX(), midY-18))
	r.mkColorLbl.Resize(fyne.NewSize(32, 14))
	r.mkColorSwatch.FillColor = col
	r.mkColorSwatch.Move(fyne.NewPos(colorX, midY-mkSwatchSz/2))
	r.mkColorSwatch.Resize(fyne.NewSize(mkSwatchSz, mkSwatchSz))

	// Separator label for size
	r.mkSizeLbl.Text = fmt.Sprintf("%d", size)
	r.mkSizeLbl.Move(fyne.NewPos(sizeValX, midY-10))
	r.mkSizeLbl.Resize(fyne.NewSize(26, 18))
	canvas.Refresh(r.mkSizeLbl)

	// [-]
	r.mkSizeMinBg.Move(fyne.NewPos(sizeMinX, midY-13))
	r.mkSizeMinBg.Resize(fyne.NewSize(mkStepperW, 26))
	r.mkSizeMinLbl.Move(fyne.NewPos(sizeMinX, midY-10))
	r.mkSizeMinLbl.Resize(fyne.NewSize(mkStepperW, 18))

	// [+]
	r.mkSizePlusBg.Move(fyne.NewPos(sizePlusX, midY-13))
	r.mkSizePlusBg.Resize(fyne.NewSize(mkStepperW, 26))
	r.mkSizePlusLbl.Move(fyne.NewPos(sizePlusX, midY-10))
	r.mkSizePlusLbl.Resize(fyne.NewSize(mkStepperW, 18))

	// Tool-specific options
	fillTogW := float32(52)
	switch tool {
	case mkToolRect, mkToolCircle:
		if fill {
			r.mkFillTogBg.FillColor = color.NRGBA{0x26, 0x60, 0xd0, 0xff}
			r.mkFillTogLbl.Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
		} else {
			r.mkFillTogBg.FillColor = color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
			r.mkFillTogLbl.Color = color.NRGBA{0xbb, 0xbb, 0xbb, 0xff}
		}
		r.mkFillTogBg.Move(fyne.NewPos(toolOptX, midY-14))
		r.mkFillTogBg.Resize(fyne.NewSize(fillTogW, 28))
		r.mkFillTogLbl.Move(fyne.NewPos(toolOptX, midY-8))
		r.mkFillTogLbl.Resize(fyne.NewSize(fillTogW, 16))
		r.mkFillTogBg.Show()
		r.mkFillTogLbl.Show()

		if fill {
			fsX := toolOptX + fillTogW + 8
			r.mkFillSwatch.FillColor = fillCol
			r.mkFillSwatch.Move(fyne.NewPos(fsX, midY-mkSwatchSz/2))
			r.mkFillSwatch.Resize(fyne.NewSize(mkSwatchSz, mkSwatchSz))
			r.mkFillSwatch.Show()
		} else {
			r.mkFillSwatch.Resize(zero)
		}
		for _, b := range r.mkBlurBg {
			b.Resize(zero)
		}
		for _, l := range r.mkBlurLbl {
			l.Resize(zero)
		}

	case mkToolBlur:
		r.mkFillTogBg.Resize(zero)
		r.mkFillTogLbl.Resize(zero)
		r.mkFillSwatch.Resize(zero)

		bw2 := float32(66)
		blurNames := [2]string{"Mosaic", "Smooth"}
		for i := 0; i < 2; i++ {
			bx2 := toolOptX + float32(i)*(bw2+6)
			if blurType == i {
				r.mkBlurBg[i].FillColor = color.NRGBA{0x26, 0x60, 0xd0, 0xff}
				r.mkBlurLbl[i].Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			} else {
				r.mkBlurBg[i].FillColor = color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
				r.mkBlurLbl[i].Color = color.NRGBA{0xbb, 0xbb, 0xbb, 0xff}
			}
			r.mkBlurBg[i].Move(fyne.NewPos(bx2, midY-14))
			r.mkBlurBg[i].Resize(fyne.NewSize(bw2, 28))
			r.mkBlurLbl[i].Text = blurNames[i]
			r.mkBlurLbl[i].Move(fyne.NewPos(bx2, midY-8))
			r.mkBlurLbl[i].Resize(fyne.NewSize(bw2, 16))
		}

	default:
		r.mkFillTogBg.Resize(zero)
		r.mkFillTogLbl.Resize(zero)
		r.mkFillSwatch.Resize(zero)
		for _, b := range r.mkBlurBg {
			b.Resize(zero)
		}
		for _, l := range r.mkBlurLbl {
			l.Resize(zero)
		}
	}

	// Undo button
	undoAlpha := uint8(0xff)
	if !hasUndo {
		undoAlpha = 0x44
	}
	undoBtnH := float32(42)
	r.mkUndoBg.Move(fyne.NewPos(undoX, midY-undoBtnH/2))
	r.mkUndoBg.Resize(fyne.NewSize(mkUndoW, undoBtnH))
	r.mkUndoLbl.Color = color.NRGBA{undoAlpha, undoAlpha, undoAlpha, undoAlpha}
	r.mkUndoLbl.Move(fyne.NewPos(undoX, midY-8))
	r.mkUndoLbl.Resize(fyne.NewSize(mkUndoW, 16))

	// Capture button
	capBtnH := float32(48)
	r.mkCaptureBg.Move(fyne.NewPos(captureX, midY-capBtnH/2))
	r.mkCaptureBg.Resize(fyne.NewSize(mkCaptureW, capBtnH))
	r.mkCaptureLbl.Move(fyne.NewPos(captureX, midY-9))
	r.mkCaptureLbl.Resize(fyne.NewSize(mkCaptureW, 18))

	// ── Palette popup ──────────────────────────────────────────────────────────
	if showPal {
		var palAnchorX float32
		if palForFill {
			fillTogW2 := float32(52)
			_, _, _, _, _, toolOptX2, _, _ := w.mkGeom(sz)
			palAnchorX = toolOptX2 + fillTogW2 + 8
		} else {
			_, colorX2, _, _, _, _, _, _ := w.mkGeom(sz)
			palAnchorX = colorX2
		}
		palX := palAnchorX
		palY := barTopY - 94
		palW := float32(4*30 + 4)
		palH := float32(3*30 + 4)

		r.mkPalBg.Move(fyne.NewPos(palX-4, palY-4))
		r.mkPalBg.Resize(fyne.NewSize(palW+8, palH+8))
		r.mkPalBg.Show()

		for i := 0; i < 12; i++ {
			col2 := i % 4
			row := i / 4
			px := palX + float32(col2)*30
			py := palY + float32(row)*30
			r.mkPalSwatch[i].Move(fyne.NewPos(px, py))
			r.mkPalSwatch[i].Resize(fyne.NewSize(26, 26))
			r.mkPalSwatch[i].Show()
		}
	} else {
		r.mkPalBg.Resize(zero)
		for _, sw := range r.mkPalSwatch {
			sw.Resize(zero)
		}
	}
}

// ─── utilities ────────────────────────────────────────────────────────────────

func float32Min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func float32Max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func copyRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// saveMarkupTmpFile saves the composite and returns the temp file path.
func saveMarkupTmpFile(bgImg image.Image, buf *image.RGBA) (string, error) {
	tmpOut := fmt.Sprintf("/tmp/swiftcap_markup_%d.png", time.Now().UnixNano())
	var base image.Image
	if bgImg != nil {
		base = bgImg
	} else if buf != nil {
		base = image.NewRGBA(buf.Bounds())
	} else {
		base = image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if err := saveComposite(base, buf, tmpOut); err != nil {
		os.Remove(tmpOut)
		return "", err
	}
	return tmpOut, nil
}

package uiapp

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"time"

	"fyne.io/fyne/v2"
)

// ── compositing ───────────────────────────────────────────────────────────────

// saveComposite blends markupBuf (widget-logical-size RGBA) over bgImg
// (full-screen screenshot) and writes the result as PNG to outFile.
func saveComposite(bgImg image.Image, markupBuf *image.RGBA, outFile string) error {
	bgB := bgImg.Bounds()
	out := image.NewRGBA(bgB)
	bw := bgB.Dx()
	bh := bgB.Dy()
	mB := markupBuf.Bounds()
	mw := mB.Dx()
	mh := mB.Dy()

	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			br, bg, bb, ba := bgImg.At(bgB.Min.X+x, bgB.Min.Y+y).RGBA()

			mx, my := 0, 0
			if mw > 0 {
				mx = x * mw / bw
			}
			if mh > 0 {
				my = y * mh / bh
			}
			mk := markupBuf.RGBAAt(mB.Min.X+mx, mB.Min.Y+my)

			// Porter-Duff "over" (markup premultiplied RGBA over background)
			fa := float64(mk.A) / 255.0
			out.SetRGBA(bgB.Min.X+x, bgB.Min.Y+y, color.RGBA{
				R: clampU8(float64(br>>8)*(1-fa) + float64(mk.R)),
				G: clampU8(float64(bg>>8)*(1-fa) + float64(mk.G)),
				B: clampU8(float64(bb>>8)*(1-fa) + float64(mk.B)),
				A: clampU8(float64(ba>>8)*(1-fa) + float64(mk.A)),
			})
		}
	}

	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ── pixel helpers ─────────────────────────────────────────────────────────────

// blendOver composites src (NRGBA) over the existing pixel at (x,y) in img.
func blendOver(img *image.RGBA, x, y int, src color.NRGBA) {
	if src.A == 0 {
		return
	}
	dst := img.RGBAAt(x, y)
	sa := float64(src.A) / 255.0
	da := float64(dst.A) / 255.0
	oa := sa + da*(1-sa)
	if oa == 0 {
		img.SetRGBA(x, y, color.RGBA{})
		return
	}
	img.SetRGBA(x, y, color.RGBA{
		R: clampU8((float64(src.R)*sa + float64(dst.R)*da*(1-sa)) / oa),
		G: clampU8((float64(src.G)*sa + float64(dst.G)*da*(1-sa)) / oa),
		B: clampU8((float64(src.B)*sa + float64(dst.B)*da*(1-sa)) / oa),
		A: uint8(oa * 255),
	})
}

func blendClamped(img *image.RGBA, x, y int, c color.NRGBA, b image.Rectangle) {
	if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
		blendOver(img, x, y, c)
	}
}

// ── drawing primitives ────────────────────────────────────────────────────────

// mkDrawBrush draws a soft or hard round stroke between consecutive points.
func mkDrawBrush(img *image.RGBA, pts []image.Point, col color.NRGBA, radius int, soft bool) {
	b := img.Bounds()
	for i, p := range pts {
		if i > 0 {
			mkBresenham(img, pts[i-1].X, pts[i-1].Y, p.X, p.Y, col, radius, soft, b)
		} else {
			mkDisk(img, p.X, p.Y, col, radius, soft, b)
		}
	}
}

func mkBresenham(img *image.RGBA, x0, y0, x1, y1 int, col color.NRGBA, r int, soft bool, b image.Rectangle) {
	dx, dy := x1-x0, y1-y0
	adx, ady := dx, dy
	if adx < 0 { adx = -adx }
	if ady < 0 { ady = -ady }
	sx, sy := 1, 1
	if x0 > x1 { sx = -1 }
	if y0 > y1 { sy = -1 }
	err := adx - ady
	for {
		mkDisk(img, x0, y0, col, r, soft, b)
		if x0 == x1 && y0 == y1 { break }
		e2 := 2 * err
		if e2 > -ady { err -= ady; x0 += sx }
		if e2 < adx  { err += adx; y0 += sy }
	}
}

func mkDisk(img *image.RGBA, cx, cy int, col color.NRGBA, r int, soft bool, b image.Rectangle) {
	r2 := float64(r * r)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			d2 := float64(dx*dx + dy*dy)
			if d2 > r2 { continue }
			px, py := cx+dx, cy+dy
			c := col
			if soft {
				edge := 1.0 - d2/r2
				c.A = uint8(float64(col.A) * edge)
			}
			blendClamped(img, px, py, c, b)
		}
	}
}

// mkDrawRect draws a rectangle border (and optional fill).
func mkDrawRect(img *image.RGBA, x0, y0, x1, y1 int, bCol color.NRGBA, bR int, fCol color.NRGBA, fill bool) {
	if x0 > x1 { x0, x1 = x1, x0 }
	if y0 > y1 { y0, y1 = y1, y0 }
	b := img.Bounds()
	if fill && fCol.A > 0 {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				blendClamped(img, x, y, fCol, b)
			}
		}
	}
	for x := x0; x <= x1; x++ {
		for t := -bR; t <= bR; t++ {
			blendClamped(img, x, y0+t, bCol, b)
			blendClamped(img, x, y1+t, bCol, b)
		}
	}
	for y := y0; y <= y1; y++ {
		for t := -bR; t <= bR; t++ {
			blendClamped(img, x0+t, y, bCol, b)
			blendClamped(img, x1+t, y, bCol, b)
		}
	}
}

// mkDrawEllipse draws an ellipse border (and optional fill).
func mkDrawEllipse(img *image.RGBA, x0, y0, x1, y1 int, bCol color.NRGBA, bR int, fCol color.NRGBA, fill bool) {
	if x0 > x1 { x0, x1 = x1, x0 }
	if y0 > y1 { y0, y1 = y1, y0 }
	b := img.Bounds()
	cx := float64(x0+x1) / 2
	cy := float64(y0+y1) / 2
	rx := float64(x1-x0) / 2
	ry := float64(y1-y0) / 2
	if rx < 1 { rx = 1 }
	if ry < 1 { ry = 1 }

	if fill && fCol.A > 0 {
		for y := y0; y <= y1; y++ {
			dy := (float64(y) - cy) / ry
			if dy < -1 || dy > 1 { continue }
			w := math.Sqrt(1-dy*dy) * rx
			for x := int(cx - w); x <= int(cx+w); x++ {
				blendClamped(img, x, y, fCol, b)
			}
		}
	}

	steps := int(2 * math.Pi * math.Max(rx, ry) * 2)
	if steps < 360 { steps = 360 }
	for i := 0; i <= steps; i++ {
		t := 2 * math.Pi * float64(i) / float64(steps)
		px := int(math.Round(cx + rx*math.Cos(t)))
		py := int(math.Round(cy + ry*math.Sin(t)))
		mkDisk(img, px, py, bCol, bR, false, b)
	}
}

// mkDrawLine draws a thick line from p0 to p1.
func mkDrawLine(img *image.RGBA, x0, y0, x1, y1 int, col color.NRGBA, r int) {
	b := img.Bounds()
	mkBresenham(img, x0, y0, x1, y1, col, r, false, b)
}

// mkDrawArrow draws a line + arrowhead.
func mkDrawArrow(img *image.RGBA, x0, y0, x1, y1 int, col color.NRGBA, r int) {
	b := img.Bounds()
	mkBresenham(img, x0, y0, x1, y1, col, r, false, b)
	angle := math.Atan2(float64(y1-y0), float64(x1-x0))
	headLen := float64(r*4 + 14)
	spread := math.Pi / 6
	for _, side := range []float64{-spread, spread} {
		ax := int(float64(x1) - headLen*math.Cos(angle+side))
		ay := int(float64(y1) - headLen*math.Sin(angle+side))
		mkBresenham(img, x1, y1, ax, ay, col, r, false, b)
	}
}

// ── blur ─────────────────────────────────────────────────────────────────────

// mkMosaicBlur pixelates the bg region [wx0,wy0,wx1,wy1] (widget coords) into img.
func mkMosaicBlur(img *image.RGBA, bg image.Image, wx0, wy0, wx1, wy1, blockSize int) {
	if wx0 > wx1 { wx0, wx1 = wx1, wx0 }
	if wy0 > wy1 { wy0, wy1 = wy1, wy0 }
	if blockSize < 2 { blockSize = 2 }
	b := img.Bounds()
	bgB := bg.Bounds()
	bw, bh := bgB.Dx(), bgB.Dy()
	mw, mh := b.Dx(), b.Dy()

	for by2 := wy0; by2 < wy1; by2 += blockSize {
		for bx2 := wx0; bx2 < wx1; bx2 += blockSize {
			scx := (bx2 + blockSize/2) * bw / mw
			scy := (by2 + blockSize/2) * bh / mh
			if scx >= bw { scx = bw - 1 }
			if scy >= bh { scy = bh - 1 }
			rr, gg, bb2, _ := bg.At(bgB.Min.X+scx, bgB.Min.Y+scy).RGBA()
			bc := color.NRGBA{uint8(rr >> 8), uint8(gg >> 8), uint8(bb2 >> 8), 0xff}
			for dy := 0; dy < blockSize && by2+dy < wy1; dy++ {
				for dx := 0; dx < blockSize && bx2+dx < wx1; dx++ {
					blendClamped(img, bx2+dx, by2+dy, bc, b)
				}
			}
		}
	}
}

// mkBoxBlur applies a box blur using the bg image.
func mkBoxBlur(img *image.RGBA, bg image.Image, wx0, wy0, wx1, wy1, radius int) {
	if wx0 > wx1 { wx0, wx1 = wx1, wx0 }
	if wy0 > wy1 { wy0, wy1 = wy1, wy0 }
	if radius < 1 { radius = 1 }
	b := img.Bounds()
	bgB := bg.Bounds()
	bw, bh := bgB.Dx(), bgB.Dy()
	mw, mh := b.Dx(), b.Dy()

	for y := wy0; y < wy1; y++ {
		if y < b.Min.Y || y >= b.Max.Y { continue }
		for x := wx0; x < wx1; x++ {
			if x < b.Min.X || x >= b.Max.X { continue }
			var rs, gs, bs, n int64
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					bgX := (x + dx) * bw / mw
					bgY := (y + dy) * bh / mh
					if bgX < 0 { bgX = 0 }
					if bgY < 0 { bgY = 0 }
					if bgX >= bw { bgX = bw - 1 }
					if bgY >= bh { bgY = bh - 1 }
					rr, gg, bb2, _ := bg.At(bgB.Min.X+bgX, bgB.Min.Y+bgY).RGBA()
					rs += int64(rr >> 8)
					gs += int64(gg >> 8)
					bs += int64(bb2 >> 8)
					n++
				}
			}
			if n > 0 {
				img.Set(x, y, color.NRGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), 0xff})
			}
		}
	}
}

// ── freeform capture ──────────────────────────────────────────────────────────

// saveFreeformTmpFile crops bgImg to the polygon defined by freePoints (widget
// logical coords), masks out pixels outside the polygon, and saves a PNG to a
// temp file.  Returns the file path on success.
func saveFreeformTmpFile(bgImg image.Image, freePoints []fyne.Position, scale float32) (string, error) {
	if bgImg == nil || len(freePoints) < 3 {
		return "", fmt.Errorf("need at least 3 points")
	}

	// Bounding box in widget coords
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

	bgB := bgImg.Bounds()
	bgW, bgH := bgB.Dx(), bgB.Dy()

	// Bounding box in screen pixel coords (clamped)
	sx0 := int(bMinX * scale)
	sy0 := int(bMinY * scale)
	sx1 := int(bMaxX * scale)
	sy1 := int(bMaxY * scale)
	if sx0 < 0 {
		sx0 = 0
	}
	if sy0 < 0 {
		sy0 = 0
	}
	if sx1 > bgW {
		sx1 = bgW
	}
	if sy1 > bgH {
		sy1 = bgH
	}
	cropW := sx1 - sx0
	cropH := sy1 - sy0
	if cropW <= 0 || cropH <= 0 {
		return "", fmt.Errorf("empty region")
	}

	out := image.NewNRGBA(image.Rect(0, 0, cropW, cropH))
	n := len(freePoints)

	// Scanline fill: for each row compute x-intersections, copy pixels between pairs.
	for cy := 0; cy < cropH; cy++ {
		py := sy0 + cy
		wy := float32(py) / scale // widget y for this screen row

		var xs []float32
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			yi := freePoints[i].Y
			yj := freePoints[j].Y
			if (yi <= wy && yj > wy) || (yj <= wy && yi > wy) {
				xi := freePoints[i].X
				xj := freePoints[j].X
				wx := xi + (wy-yi)*(xj-xi)/(yj-yi)
				xs = append(xs, wx)
			}
		}
		sort.Slice(xs, func(a, b int) bool { return xs[a] < xs[b] })

		for k := 0; k+1 < len(xs); k += 2 {
			fillX0 := int(xs[k]*scale) - sx0
			fillX1 := int(xs[k+1]*scale) - sx0
			if fillX0 < 0 {
				fillX0 = 0
			}
			if fillX1 > cropW {
				fillX1 = cropW
			}
			for cx := fillX0; cx < fillX1; cx++ {
				px := sx0 + cx
				r, g, b, a := bgImg.At(bgB.Min.X+px, bgB.Min.Y+py).RGBA()
				out.SetNRGBA(cx, cy, color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
			}
		}
		xs = xs[:0]
	}

	tmpOut := fmt.Sprintf("/tmp/swiftcap_freeform_%d.png", time.Now().UnixNano())
	f, err := os.Create(tmpOut)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		return "", err
	}
	return tmpOut, nil
}

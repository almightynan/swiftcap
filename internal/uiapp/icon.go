package uiapp

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sync"

	"fyne.io/fyne/v2"
)

var (
	iconOnce sync.Once
	idleIcon fyne.Resource
)

func baseAppIcon() fyne.Resource {
	iconOnce.Do(func() {
		idleIcon = renderTrayIcon(false, false, false, false)
	})
	return idleIcon
}

func trayIcon(recording, paused, flash, showPlay bool) fyne.Resource {
	if !recording {
		return baseAppIcon()
	}
	return renderTrayIcon(recording, paused, flash, showPlay)
}

func renderTrayIcon(recording, paused, flash, showPlay bool) fyne.Resource {
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.NRGBA{0x0f, 0x0f, 0x12, 0xff}}, image.Point{}, draw.Src)

	frame := image.Rect(6, 6, 122, 122)
	draw.Draw(img, frame, &image.Uniform{color.NRGBA{0x32, 0x33, 0x39, 0xff}}, image.Point{}, draw.Src)
	inner := image.Rect(12, 12, 116, 116)
	draw.Draw(img, inner, &image.Uniform{color.NRGBA{0x13, 0x13, 0x16, 0xff}}, image.Point{}, draw.Src)

	// simple diagonal accent to hint at motion
	for i := 0; i < 50; i++ {
		x := 20 + i
		y := 80 - i/2
		if x >= 0 && x < 128 && y >= 0 && y < 128 {
			img.Set(x, y, color.NRGBA{0x64, 0x64, 0x70, 0xff})
			if x+1 < 128 {
				img.Set(x+1, y, color.NRGBA{0x64, 0x64, 0x70, 0x88})
			}
		}
	}

	if recording {
		drawOverlaySymbol(img, paused, flash, showPlay)
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return fyne.NewStaticResource("swiftcap-tray.png", buf.Bytes())
}

// drawOverlaySymbol draws a large, centered status glyph so the tray icon makes
// the recording state obvious at a glance.
func drawOverlaySymbol(img *image.RGBA, paused, flash, showPlay bool) {
	switch {
	case paused:
		drawPauseGlyph(img)
	case showPlay:
		drawPlayGlyph(img)
	default:
		drawRecGlyph(img, flash)
	}
}

// drawRecGlyph draws a big red record dot that pulses (brighter + larger) on the
// flash beat, with a dark halo so it stands out on any tray background.
func drawRecGlyph(img *image.RGBA, flash bool) {
	const cx, cy = 64, 64
	fill := color.NRGBA{0xcf, 0x24, 0x22, 0xff}
	r := 36
	if flash {
		fill = color.NRGBA{0xff, 0x40, 0x38, 0xff}
		r = 41
	}
	fillCircle(img, cx, cy, r+6, color.NRGBA{0x0b, 0x0b, 0x0d, 0xff})
	fillCircle(img, cx, cy, r, fill)
}

func drawPauseGlyph(img *image.RGBA) {
	fillCircle(img, 64, 64, 46, color.NRGBA{0x0b, 0x0b, 0x0d, 0xff})
	bar := color.NRGBA{0xf1, 0xc0, 0x5a, 0xff}
	draw.Draw(img, image.Rect(46, 40, 60, 88), &image.Uniform{bar}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(68, 40, 82, 88), &image.Uniform{bar}, image.Point{}, draw.Src)
}

func drawPlayGlyph(img *image.RGBA) {
	fillCircle(img, 64, 64, 46, color.NRGBA{0x0b, 0x0b, 0x0d, 0xff})
	pts := []image.Point{{52, 42}, {88, 64}, {52, 86}}
	fillTriangle(img, pts, color.NRGBA{0x27, 0xb3, 0x72, 0xff})
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.NRGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				img.Set(cx+x, cy+y, col)
			}
		}
	}
}

func fillTriangle(img *image.RGBA, pts []image.Point, fill color.NRGBA) {
	minY, maxY := pts[0].Y, pts[0].Y
	for _, p := range pts[1:] {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	for y := minY; y <= maxY; y++ {
		var intersections []int
		for i := 0; i < len(pts); i++ {
			j := (i + 1) % len(pts)
			y1 := pts[i].Y
			y2 := pts[j].Y
			if y1 == y2 {
				continue
			}
			if y >= min(y1, y2) && y < max(y1, y2) {
				x := pts[i].X + (y-y1)*(pts[j].X-pts[i].X)/(y2-y1)
				intersections = append(intersections, x)
			}
		}
		if len(intersections) < 2 {
			continue
		}
		if intersections[0] > intersections[1] {
			intersections[0], intersections[1] = intersections[1], intersections[0]
		}
		for x := intersections[0]; x <= intersections[1]; x++ {
			img.Set(x, y, fill)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

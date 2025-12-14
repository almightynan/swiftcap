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

func drawOverlaySymbol(img *image.RGBA, paused, flash, showPlay bool) {
	overlay := image.Rect(80, 80, 118, 118)
	draw.Draw(img, overlay, &image.Uniform{color.NRGBA{0x0b, 0x0b, 0x0d, 0xee}}, image.Point{}, draw.Src)

	switch {
	case paused:
		drawPauseBars(img, overlay)
	case showPlay:
		drawPlayTriangle(img, overlay)
	case flash:
		drawRecordCircle(img, overlay, color.NRGBA{0xdc, 0x2a, 0x2a, 0xff})
	default:
		drawRecordCircle(img, overlay, color.NRGBA{0xaa, 0x1e, 0x1e, 0xff})
	}
}

func drawPauseBars(img *image.RGBA, area image.Rectangle) {
	barWidth := area.Dx() / 4
	padding := barWidth / 2
	left := image.Rect(area.Min.X+padding, area.Min.Y+2, area.Min.X+padding+barWidth, area.Max.Y-2)
	right := image.Rect(area.Max.X-padding-barWidth, area.Min.Y+2, area.Max.X-padding, area.Max.Y-2)
	draw.Draw(img, left, &image.Uniform{color.NRGBA{0xee, 0xee, 0xee, 0xff}}, image.Point{}, draw.Src)
	draw.Draw(img, right, &image.Uniform{color.NRGBA{0xee, 0xee, 0xee, 0xff}}, image.Point{}, draw.Src)
}

func drawPlayTriangle(img *image.RGBA, area image.Rectangle) {
	points := []image.Point{
		{area.Min.X + 3, area.Min.Y + 2},
		{area.Max.X - 3, area.Min.Y + area.Dy()/2},
		{area.Min.X + 3, area.Max.Y - 2},
	}
	fillTriangle(img, points, color.NRGBA{0xee, 0xee, 0xee, 0xff})
}

func drawRecordCircle(img *image.RGBA, area image.Rectangle, fill color.NRGBA) {
	size := area.Dx()
	radius := size / 3
	cx := area.Min.X + size/2
	cy := area.Min.Y + size/2
	for x := -radius; x <= radius; x++ {
		for y := -radius; y <= radius; y++ {
			if x*x+y*y <= radius*radius {
				img.Set(cx+x, cy+y, fill)
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

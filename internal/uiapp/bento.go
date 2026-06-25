package uiapp

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var (
	bentoSurface = color.NRGBA{0xf7, 0xf8, 0xfa, 0xff}
	bentoBorder  = color.NRGBA{0xd6, 0xdb, 0xe2, 0xff}

	accentMint  = color.NRGBA{0x76, 0xd4, 0xa4, 0xff}
	accentSky   = color.NRGBA{0x7b, 0xb8, 0xf1, 0xff}
	accentCoral = color.NRGBA{0xf1, 0x98, 0x83, 0xff}
	accentSun   = color.NRGBA{0xf3, 0xc4, 0x6a, 0xff}
)

func bentoCard(title, subtitle string, accent color.NRGBA, body fyne.CanvasObject) fyne.CanvasObject {
	titleLabel := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	headerRow := container.NewHBox(bentoAccentDot(accent), titleLabel)

	headerItems := []fyne.CanvasObject{headerRow}
	if subtitle != "" {
		subtitleLabel := widget.NewLabel(subtitle)
		subtitleLabel.Wrapping = fyne.TextWrapWord
		subtitleLabel.Importance = widget.LowImportance
		headerItems = append(headerItems, subtitleLabel)
	}
	header := container.NewVBox(headerItems...)

	cardSurface := canvas.NewRectangle(blendColor(bentoSurface, accent, 0.08))
	cardSurface.StrokeColor = bentoBorder
	cardSurface.StrokeWidth = 1

	content := container.NewVBox(
		container.NewPadded(header),
		container.NewPadded(body),
	)

	return container.NewPadded(container.NewStack(cardSurface, content))
}

func bentoTile(accent color.NRGBA, body fyne.CanvasObject) fyne.CanvasObject {
	cardSurface := canvas.NewRectangle(blendColor(bentoSurface, accent, 0.06))
	cardSurface.StrokeColor = bentoBorder
	cardSurface.StrokeWidth = 1
	return container.NewStack(cardSurface, body)
}

func bentoAccentDot(accent color.NRGBA) fyne.CanvasObject {
	dot := canvas.NewCircle(accent)
	dot.StrokeWidth = 0
	return container.NewGridWrap(fyne.NewSize(10, 10), dot)
}

func newAmbientBackground() *canvas.Raster {
	warm := color.NRGBA{0xf8, 0xf2, 0xea, 0xff}
	cool := color.NRGBA{0xe8, 0xf1, 0xf6, 0xff}
	glow := color.NRGBA{0xff, 0xef, 0xdd, 0xff}
	return canvas.NewRasterWithPixels(func(x, y, w, h int) color.Color {
		if w == 0 || h == 0 {
			return warm
		}
		fx := float64(x) / float64(w)
		fy := float64(y) / float64(h)
		base := blendColor(warm, cool, (fx+fy)/2)
		dx := fx - 0.15
		dy := fy - 0.1
		dist := math.Sqrt(dx*dx + dy*dy)
		glowStrength := clamp01(1 - dist/0.6)
		return blendColor(base, glow, glowStrength*0.25)
	})
}

func blendColor(a, b color.NRGBA, t float64) color.NRGBA {
	t = clamp01(t)
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

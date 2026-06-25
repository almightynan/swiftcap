package uiapp

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var (
	bentoSurface = color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
	bentoBorder  = color.NRGBA{0x3e, 0x3e, 0x3e, 0xff}

	accentMint  = color.NRGBA{0x3d, 0xd6, 0x8c, 0xff}
	accentSky   = color.NRGBA{0x5a, 0xa8, 0xe6, 0xff}
	accentCoral = color.NRGBA{0xf1, 0x74, 0x63, 0xff}
	accentSun   = color.NRGBA{0xf5, 0xb9, 0x41, 0xff}
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

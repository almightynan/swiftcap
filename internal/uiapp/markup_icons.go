package uiapp

import "fyne.io/fyne/v2"

// Proper rotate / mirror glyphs for the editor toolbar. Plain white SVGs (these
// buttons are never in the accent "active" state, so they don't need theming —
// and a themed resource resolves to blank when built at package-init time).
func rawSVG(name, svg string) fyne.Resource {
	return fyne.NewStaticResource(name, []byte(svg))
}

var (
	// Counter-clockwise circular arrow.
	mkIconRotateLeft = rawSVG("rotate-left.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#ffffff" d="M12 5V2L8 6l4 4V7c3.31 0 6 2.69 6 6s-2.69 6-6 6-6-2.69-6-6H4c0 4.42 3.58 8 8 8s8-3.58 8-8-3.58-8-8-8z"/></svg>`)
	// Clockwise circular arrow.
	mkIconRotateRight = rawSVG("rotate-right.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#ffffff" d="M12 5V2l4 4-4 4V7c-3.31 0-6 2.69-6 6s2.69 6 6 6 6-2.69 6-6h2c0 4.42-3.58 8-8 8s-8-3.58-8-8 3.58-8 8-8z"/></svg>`)
	// Mirror across the vertical axis (left ↔ right): centre bar + outward triangles.
	mkIconFlipH = rawSVG("flip-h.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#ffffff" d="M11 3h2v18h-2z"/><path fill="#ffffff" d="M9 6 3 12l6 6z"/><path fill="#ffffff" d="m15 6 6 6-6 6z"/></svg>`)
	// Mirror across the horizontal axis (top ↕ bottom).
	mkIconFlipV = rawSVG("flip-v.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#ffffff" d="M3 11h18v2H3z"/><path fill="#ffffff" d="M6 9l6-6 6 6z"/><path fill="#ffffff" d="M6 15l6 6 6-6z"/></svg>`)
	// Crop marks (overlapping L-brackets) — reads as "crop", not "cut".
	mkIconCrop = rawSVG("crop.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#ffffff" d="M7 17V1H5v4H1v2h4v10c0 1.1.9 2 2 2h10v4h2v-4h4v-2H7zM17 15h2V7c0-1.1-.9-2-2-2H9v2h8v8z"/></svg>`)
)

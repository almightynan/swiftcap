package uiapp

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// hoverButton is a widget.Button that shows a pointer cursor on hover so buttons
// actually feel clickable. Fyne's stock button keeps the default arrow cursor.
type hoverButton struct {
	widget.Button
}

func newButton(label string, tapped func()) *hoverButton {
	b := &hoverButton{}
	b.Text = label
	b.OnTapped = tapped
	b.ExtendBaseWidget(b)
	return b
}

func newButtonWithIcon(label string, icon fyne.Resource, tapped func()) *hoverButton {
	b := &hoverButton{}
	b.Text = label
	b.Icon = icon
	b.OnTapped = tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *hoverButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

// iconButton is an icon-only button that shows a small text tooltip below itself
// on hover (Fyne has no built-in tooltips). tipHost is a non-interactive layer
// in the *content* tree (never the overlay stack — an overlay is modal and would
// steal the button's own mouse events, causing hover flicker + dead clicks).
type iconButton struct {
	widget.Button
	tip     string
	tipHost *fyne.Container
	tipObj  fyne.CanvasObject
}

func newIconButton(icon fyne.Resource, tip string, tapped func(), tipHost *fyne.Container) *iconButton {
	b := &iconButton{tip: tip, tipHost: tipHost}
	b.Icon = icon
	b.OnTapped = tapped
	b.Importance = widget.LowImportance
	b.ExtendBaseWidget(b)
	return b
}

func (b *iconButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *iconButton) MouseIn(e *desktop.MouseEvent) {
	b.Button.MouseIn(e)
	b.showTip()
}

func (b *iconButton) MouseOut() {
	b.Button.MouseOut()
	b.hideTip()
}

func (b *iconButton) Tapped(e *fyne.PointEvent) {
	b.hideTip()
	b.Button.Tapped(e)
}

func (b *iconButton) showTip() {
	if b.tip == "" || b.tipHost == nil {
		return
	}
	b.hideTip()

	txt := canvas.NewText(b.tip, color.NRGBA{0xff, 0xff, 0xff, 0xff})
	txt.TextSize = 12
	ts := fyne.MeasureText(txt.Text, txt.TextSize, txt.TextStyle)
	const padX, padY float32 = 9, 5
	w, h := ts.Width+padX*2, ts.Height+padY*2

	bg := canvas.NewRectangle(color.NRGBA{0x00, 0x00, 0x00, 0xe6})
	bg.CornerRadius = 6
	bg.Resize(fyne.NewSize(w, h))

	// AbsolutePositionForObject is canvas-relative; tipHost fills the canvas from
	// the origin, so these coordinates map straight into it.
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
	sz := b.Size()
	x := pos.X + (sz.Width-w)/2
	y := pos.Y + sz.Height + 6
	bg.Move(fyne.NewPos(x, y))
	txt.Resize(ts)
	txt.Move(fyne.NewPos(x+padX, y+padY))

	b.tipObj = container.NewWithoutLayout(bg, txt)
	b.tipHost.Add(b.tipObj)
}

func (b *iconButton) hideTip() {
	if b.tipObj != nil && b.tipHost != nil {
		b.tipHost.Remove(b.tipObj)
	}
	b.tipObj = nil
}

// solidButton is a chunky, bold, solid-filled button with a rounded background
// and a hover state — used where the stock flat button feels too weak (modals).
type solidButton struct {
	widget.BaseWidget
	label   string
	fill    color.NRGBA
	hover   color.NRGBA
	textCol color.NRGBA
	onTap   func()
	bg      *canvas.Rectangle
	txt     *canvas.Text
}

func newSolidButton(label string, fill, hover, textCol color.NRGBA, onTap func()) *solidButton {
	b := &solidButton{label: label, fill: fill, hover: hover, textCol: textCol, onTap: onTap}
	b.bg = canvas.NewRectangle(fill)
	b.bg.CornerRadius = 9
	b.txt = canvas.NewText(label, textCol)
	b.txt.TextStyle = fyne.TextStyle{Bold: true}
	b.txt.TextSize = 14
	b.txt.Alignment = fyne.TextAlignCenter
	b.ExtendBaseWidget(b)
	return b
}

func (b *solidButton) Tapped(*fyne.PointEvent)      { if b.onTap != nil { b.onTap() } }
func (b *solidButton) Cursor() desktop.Cursor       { return desktop.PointerCursor }
func (b *solidButton) MouseMoved(*desktop.MouseEvent) {}
func (b *solidButton) MouseIn(*desktop.MouseEvent) {
	b.bg.FillColor = b.hover
	canvas.Refresh(b.bg)
}
func (b *solidButton) MouseOut() {
	b.bg.FillColor = b.fill
	canvas.Refresh(b.bg)
}

func (b *solidButton) CreateRenderer() fyne.WidgetRenderer {
	return &solidButtonRenderer{b: b, objs: []fyne.CanvasObject{b.bg, b.txt}}
}

type solidButtonRenderer struct {
	b    *solidButton
	objs []fyne.CanvasObject
}

func (r *solidButtonRenderer) Layout(sz fyne.Size) {
	r.b.bg.Resize(sz)
	r.b.bg.Move(fyne.NewPos(0, 0))
	ts := r.b.txt.MinSize()
	r.b.txt.Resize(ts)
	r.b.txt.Move(fyne.NewPos((sz.Width-ts.Width)/2, (sz.Height-ts.Height)/2))
}
func (r *solidButtonRenderer) MinSize() fyne.Size {
	ts := r.b.txt.MinSize()
	return fyne.NewSize(ts.Width+30, 38)
}
func (r *solidButtonRenderer) Refresh()                     { canvas.Refresh(r.b.bg) }
func (r *solidButtonRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *solidButtonRenderer) Destroy()                     {}

// ── clean button ────────────────────────────────────────────────────────────
//
// cleanButton is a modern, lightweight icon+label button with a rounded
// background and a hover state. Used where the stock button looks too blocky:
// a primary variant carries a soft accent fill, secondary variants use a barely
// there "ghost" fill that brightens on hover.

func toNRGBA(c color.Color) color.NRGBA {
	r, g, b, a := c.RGBA()
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

func lighten(c color.NRGBA, d uint8) color.NRGBA {
	add := func(v uint8) uint8 {
		if int(v)+int(d) > 255 {
			return 255
		}
		return v + d
	}
	return color.NRGBA{add(c.R), add(c.G), add(c.B), c.A}
}

type cleanButton struct {
	widget.BaseWidget
	label   string
	fill    color.NRGBA
	hover   color.NRGBA
	textCol color.NRGBA
	onTap   func()
	bg      *canvas.Rectangle
	ic      *canvas.Image
	txt     *canvas.Text
}

func newCleanButton(icon fyne.Resource, label string, fill, hover, textCol color.NRGBA, onTap func()) *cleanButton {
	b := &cleanButton{label: label, fill: fill, hover: hover, textCol: textCol, onTap: onTap}
	b.bg = canvas.NewRectangle(fill)
	b.bg.CornerRadius = 10
	if icon != nil {
		b.ic = canvas.NewImageFromResource(icon)
		b.ic.FillMode = canvas.ImageFillContain
	}
	b.txt = canvas.NewText(label, textCol)
	b.txt.TextSize = 14
	b.ExtendBaseWidget(b)
	return b
}

func (b *cleanButton) Tapped(*fyne.PointEvent)        { if b.onTap != nil { b.onTap() } }
func (b *cleanButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (b *cleanButton) MouseMoved(*desktop.MouseEvent) {}
func (b *cleanButton) MouseIn(*desktop.MouseEvent) {
	b.bg.FillColor = b.hover
	canvas.Refresh(b.bg)
}
func (b *cleanButton) MouseOut() {
	b.bg.FillColor = b.fill
	canvas.Refresh(b.bg)
}

func (b *cleanButton) CreateRenderer() fyne.WidgetRenderer {
	objs := []fyne.CanvasObject{b.bg, b.txt}
	if b.ic != nil {
		objs = append(objs, b.ic)
	}
	return &cleanButtonRenderer{b: b, objs: objs}
}

type cleanButtonRenderer struct {
	b    *cleanButton
	objs []fyne.CanvasObject
}

const cleanIconSize, cleanIconGap = 16, 8

func (r *cleanButtonRenderer) Layout(sz fyne.Size) {
	r.b.bg.Resize(sz)
	r.b.bg.Move(fyne.NewPos(0, 0))
	tw := r.b.txt.MinSize().Width
	total := tw
	if r.b.ic != nil {
		total += cleanIconSize + cleanIconGap
	}
	x := (sz.Width - total) / 2
	if r.b.ic != nil {
		r.b.ic.Resize(fyne.NewSize(cleanIconSize, cleanIconSize))
		r.b.ic.Move(fyne.NewPos(x, (sz.Height-cleanIconSize)/2))
		x += cleanIconSize + cleanIconGap
	}
	th := r.b.txt.MinSize().Height
	r.b.txt.Resize(fyne.NewSize(tw, th))
	r.b.txt.Move(fyne.NewPos(x, (sz.Height-th)/2))
}
func (r *cleanButtonRenderer) MinSize() fyne.Size {
	w := r.b.txt.MinSize().Width + 32
	if r.b.ic != nil {
		w += cleanIconSize + cleanIconGap
	}
	return fyne.NewSize(w, 36)
}
func (r *cleanButtonRenderer) Refresh()                     { canvas.Refresh(r.b.bg) }
func (r *cleanButtonRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *cleanButtonRenderer) Destroy()                     {}

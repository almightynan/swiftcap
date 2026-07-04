package uiapp

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// SegItem is one tab in a SegControl.
type SegItem struct {
	Icon  fyne.Resource
	Label string
}

// SegControl is a pill-shaped segmented selector with a smoothly animated active indicator.
type SegControl struct {
	widget.BaseWidget

	Items    []SegItem
	Selected int
	OnChange func(int)

	indicX float32
	anim   *fyne.Animation
}

// NewSegControl creates a new SegControl. onChange receives the newly selected index.
func NewSegControl(items []SegItem, onChange func(int)) *SegControl {
	s := &SegControl{
		Items:    items,
		Selected: 0,
		OnChange: onChange,
	}
	s.ExtendBaseWidget(s)
	return s
}

// SelectTab animates the indicator to idx and fires OnChange.
func (s *SegControl) SelectTab(idx int) {
	if idx < 0 || idx >= len(s.Items) || idx == s.Selected {
		return
	}
	s.Selected = idx
	if s.OnChange != nil {
		s.OnChange(idx)
	}

	tabW := float32(130)
	if w := s.Size().Width; w > 0 {
		tabW = w / float32(len(s.Items))
	}
	targetX := float32(idx) * tabW
	startX := s.indicX

	if s.anim != nil {
		s.anim.Stop()
	}
	s.anim = fyne.NewAnimation(180*time.Millisecond, func(f float32) {
		s.indicX = startX + (targetX-startX)*f
		s.Refresh()
	})
	s.anim.Curve = fyne.AnimationEaseInOut
	s.anim.Start()
}

// Cursor shows a pointer so the segmented tabs feel clickable.
func (s *SegControl) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (s *SegControl) Tapped(ev *fyne.PointEvent) {
	if len(s.Items) == 0 {
		return
	}
	w := s.Size().Width
	if w == 0 {
		return
	}
	tabW := w / float32(len(s.Items))
	idx := int(ev.Position.X / tabW)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.Items) {
		idx = len(s.Items) - 1
	}
	s.SelectTab(idx)
}

func (s *SegControl) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(color.NRGBA{0x22, 0x22, 0x22, 0xff})
	track.CornerRadius = 14
	track.StrokeColor = color.NRGBA{0x3a, 0x3a, 0x3a, 0xff}
	track.StrokeWidth = 1

	indicator := canvas.NewRectangle(color.NRGBA{0x48, 0x48, 0x48, 0xff})
	indicator.CornerRadius = 11

	icons := make([]*widget.Icon, len(s.Items))
	labels := make([]*canvas.Text, len(s.Items))
	for i, item := range s.Items {
		icons[i] = widget.NewIcon(item.Icon)
		lbl := canvas.NewText(item.Label, color.NRGBA{0xcc, 0xcc, 0xcc, 0xff})
		lbl.TextSize = 12
		lbl.Alignment = fyne.TextAlignCenter
		if i == s.Selected {
			lbl.Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			lbl.TextStyle = fyne.TextStyle{Bold: true}
		}
		labels[i] = lbl
	}

	return &segRenderer{ctrl: s, track: track, indicator: indicator, icons: icons, labels: labels}
}

// ── renderer ──────────────────────────────────────────────────────────────────

type segRenderer struct {
	ctrl      *SegControl
	track     *canvas.Rectangle
	indicator *canvas.Rectangle
	icons     []*widget.Icon
	labels    []*canvas.Text
}

func (r *segRenderer) MinSize() fyne.Size {
	return fyne.NewSize(float32(len(r.ctrl.Items))*130, 64)
}

func (r *segRenderer) Layout(size fyne.Size) {
	r.track.Resize(size)
	r.track.Move(fyne.NewPos(0, 0))
	if r.ctrl.anim == nil && size.Width > 0 {
		tabW := size.Width / float32(len(r.ctrl.Items))
		r.ctrl.indicX = float32(r.ctrl.Selected) * tabW
	}
	r.placeIndicator(size)
	r.placeCells(size)
}

func (r *segRenderer) Refresh() {
	size := r.ctrl.Size()
	r.placeIndicator(size)
	for i, lbl := range r.labels {
		if i == r.ctrl.Selected {
			lbl.Color = color.NRGBA{0xff, 0xff, 0xff, 0xff}
			lbl.TextStyle = fyne.TextStyle{Bold: true}
		} else {
			lbl.Color = color.NRGBA{0xcc, 0xcc, 0xcc, 0xff}
			lbl.TextStyle = fyne.TextStyle{}
		}
		lbl.Refresh()
	}
	canvas.Refresh(r.indicator)
	canvas.Refresh(r.track)
}

func (r *segRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.track, r.indicator}
	for _, ic := range r.icons {
		objs = append(objs, ic)
	}
	for _, lbl := range r.labels {
		objs = append(objs, lbl)
	}
	return objs
}

func (r *segRenderer) Destroy() {
	if r.ctrl.anim != nil {
		r.ctrl.anim.Stop()
	}
}

func (r *segRenderer) placeIndicator(size fyne.Size) {
	if len(r.ctrl.Items) == 0 || size.Width == 0 {
		return
	}
	tabW := size.Width / float32(len(r.ctrl.Items))
	const inset = float32(4)
	r.indicator.Move(fyne.NewPos(r.ctrl.indicX+inset, inset))
	r.indicator.Resize(fyne.NewSize(tabW-inset*2, size.Height-inset*2))
}

func (r *segRenderer) placeCells(size fyne.Size) {
	if len(r.ctrl.Items) == 0 || size.Width == 0 {
		return
	}
	tabW := size.Width / float32(len(r.ctrl.Items))
	const iconSz = float32(22)
	const lblH = float32(16)
	const gap = float32(4)
	totalH := iconSz + gap + lblH
	topOff := (size.Height - totalH) / 2

	for i, ic := range r.icons {
		cx := float32(i)*tabW + tabW/2
		ic.Resize(fyne.NewSize(iconSz, iconSz))
		ic.Move(fyne.NewPos(cx-iconSz/2, topOff))
		r.labels[i].Move(fyne.NewPos(float32(i)*tabW, topOff+iconSz+gap))
		r.labels[i].Resize(fyne.NewSize(tabW, lblH))
	}
}

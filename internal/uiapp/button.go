package uiapp

import (
	"fyne.io/fyne/v2"
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

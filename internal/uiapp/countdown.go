package uiapp

import (
	"image/color"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type countdownOverlay struct {
	app  fyne.App
	win  fyne.Window
	done chan struct{}
	once sync.Once
}

func newCountdownOverlay(a fyne.App, seconds int, onComplete func(), onCancel func()) *countdownOverlay {
	c := &countdownOverlay{
		app:  a,
		done: make(chan struct{}),
	}
	win := a.NewWindow("")
	win.SetPadded(false)
	win.SetFixedSize(true)
	win.SetFullScreen(true)

	countText := canvas.NewText(strconv.Itoa(seconds), color.White)
	countText.TextSize = 140
	countText.Alignment = fyne.TextAlignCenter

	message := widget.NewLabel("Click anywhere to cancel countdown and abort recording")
	message.Alignment = fyne.TextAlignCenter
	message.Wrapping = fyne.TextWrapWord

	tap := newTapOverlay(func() {
		c.finish(onCancel)
	})

	centerBox := container.NewVBox(layout.NewSpacer(), countText, layout.NewSpacer())

	footerRect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 230})
	footerRect.StrokeWidth = 0
	footerRect.SetMinSize(fyne.NewSize(0, 60))
	footer := container.NewMax(footerRect, container.NewVBox(layout.NewSpacer(), message))

	content := container.NewMax(
		tap,
		centerBox,
		container.NewVBox(layout.NewSpacer(), footer),
	)
	win.SetContent(content)

	c.win = win
	win.Show()
	go c.begin(seconds, countText, onComplete)
	return c
}

func (c *countdownOverlay) begin(seconds int, text *canvas.Text, onComplete func()) {
	c.updateText(text, seconds)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	remaining := seconds
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			remaining--
			if remaining <= 0 {
				c.finish(onComplete)
				return
			}
			c.updateText(text, remaining)
		}
	}
}

func (c *countdownOverlay) updateText(text *canvas.Text, value int) {
	c.runOnMain(func() {
		text.Text = strconv.Itoa(value)
		text.Refresh()
	})
}

func (c *countdownOverlay) finish(cb func()) {
	c.once.Do(func() {
		close(c.done)
		c.runOnMain(func() {
			if c.win != nil {
				c.win.Close()
			}
		})
		if cb != nil {
			cb()
		}
	})
}

func (c *countdownOverlay) close() {
	c.finish(nil)
}

func (c *countdownOverlay) runOnMain(fn func()) {
	if fn == nil {
		return
	}
	fn()
}

type tapOverlay struct {
	widget.BaseWidget
	rect  *canvas.Rectangle
	onTap func()
}

func newTapOverlay(onTap func()) *tapOverlay {
	rect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 200})
	rect.StrokeWidth = 0
	rect.SetMinSize(fyne.NewSize(100, 100))

	t := &tapOverlay{
		rect:  rect,
		onTap: onTap,
	}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapOverlay) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.rect)
}

func (t *tapOverlay) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapOverlay) TappedSecondary(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *tapOverlay) Resize(size fyne.Size) {
	t.BaseWidget.Resize(size)
	t.rect.Resize(size)
}

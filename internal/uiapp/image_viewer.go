package uiapp

import (
	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

// imageViewer shows a screenshot with left/right navigation between captures.
// Changing image crossfades smoothly (via canvas.Image.Translucency) between a
// resting layer (imgA) and an incoming layer (imgB).
type imageViewer struct {
	ui    *RecordingUI
	paths []string
	idx   int

	imgA, imgB *canvas.Image
	prevBtn    *hoverButton
	nextBtn    *hoverButton

	anim      *fyne.Animation
	animating bool

	// onChange fires on the main thread with the newly-selected path.
	onChange func(path string)
}

func newImageViewer(ui *RecordingUI, paths []string, idx int) *imageViewer {
	v := &imageViewer{ui: ui, paths: paths, idx: idx}

	mk := func() *canvas.Image {
		im := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
		im.FillMode = canvas.ImageFillContain
		im.SetMinSize(fyne.NewSize(620, 360))
		return im
	}
	v.imgA = mk()
	v.imgB = mk()
	v.imgB.Translucency = 1 // hidden until a crossfade

	if idx >= 0 && idx < len(paths) {
		go func() {
			im := loadAnyImage(paths[idx])
			if im == nil {
				return
			}
			v.ui.runOnMain(func() {
				v.imgA.Image = im
				v.imgA.Refresh()
			})
		}()
	}

	v.prevBtn = newButtonWithIcon("", theme.NavigateBackIcon(), func() { v.navigate(-1) })
	v.nextBtn = newButtonWithIcon("", theme.NavigateNextIcon(), func() { v.navigate(1) })
	return v
}

func (v *imageViewer) object() fyne.CanvasObject {
	stack := container.NewStack(v.imgB, v.imgA)

	fsBtn := newButtonWithIcon("", theme.ViewFullScreenIcon(), func() {
		if im := v.imgA.Image; im != nil {
			showImageFullscreen(v.ui.app, im)
		}
	})
	topRight := container.NewVBox(
		container.NewHBox(layout.NewSpacer(), fsBtn),
		layout.NewSpacer(),
	)

	layers := []fyne.CanvasObject{stack, container.NewPadded(topRight)}
	if len(v.paths) > 1 {
		prevWrap := container.NewVBox(layout.NewSpacer(), v.prevBtn, layout.NewSpacer())
		nextWrap := container.NewVBox(layout.NewSpacer(), v.nextBtn, layout.NewSpacer())
		nav := container.NewBorder(nil, nil,
			container.NewPadded(prevWrap), container.NewPadded(nextWrap), nil)
		layers = append(layers, nav)
	}
	return container.NewStack(layers...)
}

func (v *imageViewer) navigate(delta int) {
	if v.animating || len(v.paths) <= 1 {
		return
	}
	n := len(v.paths)
	v.idx = (v.idx + delta + n) % n
	newPath := v.paths[v.idx]
	if v.onChange != nil {
		v.onChange(newPath)
	}
	go func() {
		im := loadAnyImage(newPath)
		if im == nil {
			return
		}
		v.ui.runOnMain(func() { v.crossfadeTo(im) })
	}()
}

func (v *imageViewer) crossfadeTo(im image.Image) {
	v.animating = true
	v.imgB.Image = im
	v.imgB.Translucency = 1
	canvas.Refresh(v.imgB)
	if v.anim != nil {
		v.anim.Stop()
	}
	v.anim = fyne.NewAnimation(220*time.Millisecond, func(f float32) {
		v.imgA.Translucency = float64(f)     // fade current out
		v.imgB.Translucency = float64(1 - f) // fade incoming in
		canvas.Refresh(v.imgA)
		canvas.Refresh(v.imgB)
		if f >= 1 {
			// Settle: the incoming image becomes the resting layer.
			v.imgA.Image = im
			v.imgA.Translucency = 0
			v.imgB.Translucency = 1
			canvas.Refresh(v.imgA)
			canvas.Refresh(v.imgB)
			v.animating = false
		}
	})
	v.anim.Curve = fyne.AnimationEaseInOut
	v.anim.Start()
}

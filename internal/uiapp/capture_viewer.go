package uiapp

import (
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// showCaptureViewer opens an in-app modal previewing a capture (screenshot or
// recording): the image with a fullscreen button pinned to its top-right, the
// file path beneath it, and Open File / Open Folder actions below that.
func (ui *RecordingUI) showCaptureViewer(path string) {
	if ui.mainWin == nil {
		return
	}
	cv := ui.mainWin.Canvas()
	isVideo := isVideoExt(strings.ToLower(filepath.Ext(path)))

	var overlay *fyne.Container
	var player *videoPlayer
	var iv *imageViewer
	var revertBtn *cleanButton
	// current is the path being shown; it changes as the user navigates between
	// screenshots, so the path label and Open actions read it live.
	current := path

	// closeViewer animates the modal out (startClose is wired up once the card and
	// backdrop exist below); closing guards against double-dismiss.
	var startClose func()
	closing := false
	closeViewer := func() {
		if closing {
			return
		}
		closing = true
		cv.SetOnTypedKey(nil)
		if startClose != nil {
			startClose()
		}
	}

	// Header + path labels (declared early so navigation can update them).
	nameLbl := widget.NewLabelWithStyle(filepath.Base(path), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	pathLbl := widget.NewLabelWithStyle(prettyPath(path), fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	closeBtn := newButtonWithIcon("", theme.CancelIcon(), closeViewer)
	closeBtn.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, nil, closeBtn, nameLbl)

	// Preview area: a video player for videos, or a navigable image viewer for
	// screenshots.
	var previewArea fyne.CanvasObject
	if isVideo {
		player = newVideoPlayer(ui, path, 640, 400)
		previewArea = player.object()
	} else {
		paths, startIdx := ui.imageCapturePaths(path)
		iv = newImageViewer(ui, paths, startIdx)
		iv.onChange = func(np string) {
			current = np
			nameLbl.SetText(filepath.Base(np))
			pathLbl.SetText(prettyPath(np))
			if revertBtn != nil {
				if hasEditBackup(np) {
					revertBtn.Show()
				} else {
					revertBtn.Hide()
				}
			}
		}
		previewArea = iv.object()
	}

	// Actions — clean, lightweight buttons: one soft-accent primary, the rest are
	// ghost buttons that brighten on hover.
	prim := toNRGBA(theme.PrimaryColor())
	primHover := lighten(prim, 0x18)
	ghost := color.NRGBA{0xff, 0xff, 0xff, 0x12}
	ghostHover := color.NRGBA{0xff, 0xff, 0xff, 0x26}
	white := color.NRGBA{0xff, 0xff, 0xff, 0xff}
	dim := color.NRGBA{0xcf, 0xcf, 0xd8, 0xff}

	openFileFn := func() {
		if err := openFile(current); err != nil {
			ui.showError("Open File", err.Error())
			return
		}
		closeViewer()
	}
	openFolderFn := func() {
		if err := openFolder(current); err != nil {
			ui.showError("Open Folder", err.Error())
			return
		}
		closeViewer()
	}

	cell := func(o fyne.CanvasObject) fyne.CanvasObject {
		return container.NewGridWrap(fyne.NewSize(146, 38), o)
	}

	var actions fyne.CanvasObject
	if isVideo {
		openFileBtn := newCleanButton(theme.FileIcon(), "Open File", prim, primHover, white, openFileFn)
		openFolderBtn := newCleanButton(theme.FolderOpenIcon(), "Open Folder", ghost, ghostHover, dim, openFolderFn)
		actions = container.NewCenter(container.NewHBox(cell(openFileBtn), cell(openFolderBtn)))
	} else {
		// Edit opens the markup editor; on return, reopen the viewer so it shows
		// the edited image (and the Revert button, if edits were saved).
		editBtn := newCleanButton(theme.DocumentCreateIcon(), "Edit", prim, primHover, white, func() {
			p := current
			closeViewer()
			showMarkupEditor(ui, p, func(saved bool) {
				if saved {
					ui.refreshRecordingsList()
				}
				ui.showCaptureViewer(p)
			})
		})
		openFileBtn := newCleanButton(theme.FileIcon(), "Open File", ghost, ghostHover, dim, openFileFn)
		openFolderBtn := newCleanButton(theme.FolderOpenIcon(), "Open Folder", ghost, ghostHover, dim, openFolderFn)
		revertBtn = newCleanButton(theme.ContentUndoIcon(), "Revert", ghost, ghostHover, dim, func() {
			p := current
			dialog.ShowConfirm("Revert edits?",
				"Restore the original image and discard the edits you saved?",
				func(ok bool) {
					if !ok {
						return
					}
					if err := revertEdit(p); err != nil {
						ui.showError("Revert", err.Error())
						return
					}
					closeViewer()
					ui.refreshRecordingsList()
					ui.showCaptureViewer(p)
				}, ui.mainWin)
		})
		if !hasEditBackup(current) {
			revertBtn.Hide()
		}
		// Revert stays natural width (unwrapped) so it collapses when hidden.
		actions = container.NewCenter(container.NewHBox(
			cell(editBtn), cell(openFileBtn), cell(openFolderBtn), revertBtn))
	}

	// Path shown as a subtle monospace chip with a small file icon, so it reads
	// as a distinct element instead of loose text.
	pathChipBg := canvas.NewRectangle(color.NRGBA{0x27, 0x27, 0x2d, 0xff})
	pathChipBg.CornerRadius = 9
	pathChip := container.NewStack(pathChipBg,
		container.NewPadded(container.NewHBox(widget.NewIcon(theme.FileIcon()), pathLbl)))
	pathRow := container.NewCenter(pathChip)

	inner := container.NewVBox(
		header,
		widget.NewSeparator(),
		previewArea,
		newHeightSpacer(2),
		pathRow,
		actions,
	)

	// Bordered, padded card. The generous inset keeps content clear of the border.
	cardBg := canvas.NewRectangle(color.NRGBA{0x1e, 0x1e, 0x1e, 0xff})
	cardBg.CornerRadius = 16
	cardBg.StrokeColor = color.NRGBA{0x5a, 0x5a, 0x5a, 0xff}
	cardBg.StrokeWidth = 1.5
	// A no-op tap wrapper absorbs clicks on the card so they don't fall through
	// to the backdrop (which closes the modal).
	card := newTapableContainer(container.NewStack(cardBg, insetBy(inner, 22)), func() {})
	// WithoutLayout lets us position the card manually so the open/close animation
	// can slide it (Center would keep re-centering it).
	cardLayer := container.NewWithoutLayout(card)

	// Frosted backdrop: a blurred snapshot of the app behind the modal. Tapping
	// it (outside the card) dismisses the modal.
	var bgFade func(vis float32)
	var bgObj fyne.CanvasObject
	if shot := cv.Capture(); shot != nil {
		blur := canvas.NewImageFromImage(blurredBackdrop(shot, 5))
		blur.FillMode = canvas.ImageFillStretch
		bgObj = blur
		bgFade = func(vis float32) { blur.Translucency = float64(1 - vis); canvas.Refresh(blur) }
	} else {
		rect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 0x99})
		bgObj = rect
		bgFade = func(vis float32) {
			rect.FillColor = color.NRGBA{0, 0, 0, uint8(0x99 * vis)}
			canvas.Refresh(rect)
		}
	}
	backdrop := newTapableContainer(bgObj, closeViewer)

	overlay = container.NewStack(backdrop, cardLayer)

	// place centers the card, offset down by yOff (for the slide).
	place := func(yOff float32) {
		sz := card.MinSize()
		cs := cv.Size()
		card.Resize(sz)
		card.Move(fyne.NewPos((cs.Width-sz.Width)/2, (cs.Height-sz.Height)/2+yOff))
	}

	// startClose stops playback immediately (so audio doesn't linger), then
	// animates the card out and removes the overlay.
	startClose = func() {
		if player != nil {
			player.destroy()
		}
		anim := fyne.NewAnimation(160*time.Millisecond, func(f float32) {
			bgFade(1 - f)
			place(24 * f)
			if f >= 1 {
				cv.Overlays().Remove(overlay)
			}
		})
		anim.Curve = fyne.AnimationEaseIn
		anim.Start()
	}

	// Pre-animation state before adding (no first-frame flash), then animate in.
	bgFade(0)
	place(24)
	cv.Overlays().Add(overlay)
	overlay.Resize(cv.Size())
	place(24)
	openAnim := fyne.NewAnimation(200*time.Millisecond, func(f float32) {
		bgFade(f)
		place(24 * (1 - f))
	})
	openAnim.Curve = fyne.AnimationEaseOut
	openAnim.Start()

	// Keyboard shortcuts. Clearing focus ensures the canvas key handler receives
	// them (a focused sidebar widget would otherwise swallow the key); the
	// handler is cleared on close.
	cv.Unfocus()
	cv.SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			closeViewer()
			return
		}
		switch {
		case player != nil:
			switch ev.Name {
			case fyne.KeySpace:
				player.togglePlay()
			case fyne.KeyLeft:
				player.arrowSeek(-1)
			case fyne.KeyRight:
				player.arrowSeek(1)
			}
		case iv != nil:
			switch ev.Name {
			case fyne.KeyLeft:
				iv.navigate(-1)
			case fyne.KeyRight:
				iv.navigate(1)
			}
		}
	})
}

// imageCapturePaths returns the ordered list of screenshot (non-video) capture
// paths and the index of current within it, for prev/next navigation.
func (ui *RecordingUI) imageCapturePaths(current string) ([]string, int) {
	videosDir, _ := ui.ensureVideosDir()
	screenshotsDir, _ := ui.ensureScreenshotsDir()
	items := loadItems(videosDir, screenshotsDir)

	var paths []string
	idx := 0
	for _, it := range items {
		if it.isVideo {
			continue
		}
		if it.path == current {
			idx = len(paths)
		}
		paths = append(paths, it.path)
	}
	if len(paths) == 0 {
		paths = []string{current}
	}
	return paths, idx
}

// insetBy wraps o with pad logical pixels of transparent margin on every side.
func insetBy(o fyne.CanvasObject, pad float32) fyne.CanvasObject {
	sp := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(pad, pad))
		return r
	}
	return container.NewBorder(sp(), sp(), sp(), sp(), o)
}

// blurredBackdrop produces a real frosted-glass blur of src: downscale a bit
// (for speed), apply a separable box blur three times (which approximates a
// Gaussian, so it looks genuinely blurred rather than pixelated), upscale
// smoothly, then darken for contrast. Larger radius = blurrier.
func blurredBackdrop(src image.Image, radius int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	const ds = 3
	sw, sh := w/ds, h/ds
	if sw < 1 {
		sw = 1
	}
	if sh < 1 {
		sh = 1
	}
	small := image.NewRGBA(image.Rect(0, 0, sw, sh))
	xdraw.CatmullRom.Scale(small, small.Bounds(), src, b, xdraw.Src, nil)
	for i := 0; i < 3; i++ {
		boxBlur(small, radius)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(out, out.Bounds(), small, small.Bounds(), xdraw.Src, nil)
	xdraw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{0, 0, 0, 0x59}), image.Point{}, xdraw.Over)
	return out
}

// boxBlur applies an in-place separable box blur of the given radius to img
// (assumed to have origin 0,0). Edges clamp to the nearest pixel.
func boxBlur(img *image.RGBA, radius int) {
	if radius < 1 {
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	win := 2*radius + 1
	tmp := make([]uint8, len(img.Pix))

	// Horizontal pass: img -> tmp.
	for y := 0; y < h; y++ {
		row := y * img.Stride
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa int
			for k := -radius; k <= radius; k++ {
				xx := x + k
				if xx < 0 {
					xx = 0
				} else if xx >= w {
					xx = w - 1
				}
				i := row + xx*4
				sr += int(img.Pix[i])
				sg += int(img.Pix[i+1])
				sb += int(img.Pix[i+2])
				sa += int(img.Pix[i+3])
			}
			o := row + x*4
			tmp[o] = uint8(sr / win)
			tmp[o+1] = uint8(sg / win)
			tmp[o+2] = uint8(sb / win)
			tmp[o+3] = uint8(sa / win)
		}
	}

	// Vertical pass: tmp -> img.
	for x := 0; x < w; x++ {
		col := x * 4
		for y := 0; y < h; y++ {
			var sr, sg, sb, sa int
			for k := -radius; k <= radius; k++ {
				yy := y + k
				if yy < 0 {
					yy = 0
				} else if yy >= h {
					yy = h - 1
				}
				i := yy*img.Stride + col
				sr += int(tmp[i])
				sg += int(tmp[i+1])
				sb += int(tmp[i+2])
				sa += int(tmp[i+3])
			}
			o := y*img.Stride + col
			img.Pix[o] = uint8(sr / win)
			img.Pix[o+1] = uint8(sg / win)
			img.Pix[o+2] = uint8(sb / win)
			img.Pix[o+3] = uint8(sa / win)
		}
	}
}

// previewImageFor returns a displayable image for a capture path: the image
// itself for screenshots, or a full-resolution frame for videos. (The tiny
// .thumb.jpg sidecar is only for the grid cards — upscaling it in the large
// modal looked blurry, so we pull a fresh full-size frame here.)
func previewImageFor(path string) image.Image {
	ext := strings.ToLower(filepath.Ext(path))
	if isVideoExt(ext) {
		return extractVideoThumb(path)
	}
	return loadAnyImage(path)
}

// showImageFullscreen now lives in fullscreen.go (zoom-capable viewer).

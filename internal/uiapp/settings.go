package uiapp

import (
	"image/color"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Plain-language explanations shown by the "?" tips next to each setting.
const (
	tipFPS       = "Frames captured per second. Higher looks smoother but uses more CPU and disk. 30 is fine for most recordings; use 60 for fast motion or gaming."
	tipBitrate   = "Target video quality, in kbit/s. Higher means better quality and larger files. Around 4000-8000 suits 1080p."
	tipAudio     = "Record audio alongside the video."
	tipCursor    = "Show the mouse cursor in the recording."
	tipContainer = "Output file format. MP4 is the most widely compatible. MKV is more robust: the file stays playable even if the recording is interrupted."
	tipMaxDur    = "Automatically stop recording after this many seconds. 0 means record until you press stop."
	tipThreads   = "How many CPU threads the encoder may use. 0 lets ffmpeg pick the best value (recommended)."
	tipQP        = "Constant Quantizer: fixes quality instead of bitrate. Lower values mean better quality and bigger files. 0 disables it and uses the bitrate above."
	tipNice      = "Process priority (the Linux 'nice' value). Higher numbers give other apps more CPU. 0 is normal; raise it if recording slows your system."
)

type settingsWindow struct {
	overlay *fyne.Container
	config  *RecordingConfig
	ui      *RecordingUI

	fpsEntry     *widget.Entry
	bitrateEntry *widget.Entry
	audioCheck   *widget.Check
	cursorCheck  *widget.Check
	containerSel *widget.Select
	maxDurEntry  *widget.Entry
	threadsEntry *widget.Entry
	qpEntry      *widget.Entry
	niceEntry    *widget.Entry

	saveBtn     *hoverButton
	dirtyText   *canvas.Text
	revertTimer *time.Timer
}

func (ui *RecordingUI) showSettings() {
	if ui.settingsWin != nil {
		return // already open as a modal
	}
	if ui.mainWin == nil {
		return
	}
	cv := ui.mainWin.Canvas()

	sw := &settingsWindow{config: ui.config, ui: ui}

	// All edits are staged: nothing touches the config until Save is pressed.
	// Every widget just re-evaluates the dirty state.

	sw.fpsEntry = widget.NewEntry()
	sw.fpsEntry.SetText(strconv.Itoa(sw.config.GetFPS()))
	sw.fpsEntry.SetPlaceHolder("30")

	sw.bitrateEntry = widget.NewEntry()
	sw.bitrateEntry.SetText(strconv.Itoa(sw.config.GetBitrate()))
	sw.bitrateEntry.SetPlaceHolder("4000")

	sw.audioCheck = widget.NewCheck("Record Audio", func(bool) { sw.refreshDirty() })
	sw.audioCheck.SetChecked(sw.config.GetAudio())

	sw.cursorCheck = widget.NewCheck("Show Cursor", func(bool) { sw.refreshDirty() })
	sw.cursorCheck.SetChecked(sw.config.GetCursor())

	sw.containerSel = widget.NewSelect([]string{"mp4", "mkv", "mov", "avi"}, func(string) { sw.refreshDirty() })
	sw.containerSel.SetSelected(sw.config.GetContainer())

	sw.maxDurEntry = widget.NewEntry()
	sw.maxDurEntry.SetText(strconv.Itoa(sw.config.GetMaxDur()))
	sw.maxDurEntry.SetPlaceHolder("0")

	sw.threadsEntry = widget.NewEntry()
	sw.threadsEntry.SetText(strconv.Itoa(sw.config.GetThreads()))
	sw.threadsEntry.SetPlaceHolder("0")

	sw.qpEntry = widget.NewEntry()
	sw.qpEntry.SetText(strconv.Itoa(sw.config.GetQP()))
	sw.qpEntry.SetPlaceHolder("0")

	sw.niceEntry = widget.NewEntry()
	sw.niceEntry.SetText(strconv.Itoa(sw.config.GetNice()))
	sw.niceEntry.SetPlaceHolder("0")

	// Wire entry edits to dirty tracking (assigned after SetText so the initial
	// values don't count as changes).
	for _, e := range []*widget.Entry{
		sw.fpsEntry, sw.bitrateEntry, sw.maxDurEntry, sw.threadsEntry, sw.qpEntry, sw.niceEntry,
	} {
		e.OnChanged = func(string) { sw.refreshDirty() }
	}

	// closeSettings animates the modal out (startClose is wired up once the card
	// and backdrop exist below); closing guards against double-dismiss.
	var startClose func()
	closing := false
	closeSettings := func() {
		if closing {
			return
		}
		closing = true
		if startClose != nil {
			startClose()
		}
	}

	sw.dirtyText = canvas.NewText("", color.NRGBA{0xf1, 0xc0, 0x5a, 0xff})
	sw.dirtyText.TextSize = 12

	sw.saveBtn = newButton("Save", func() { sw.onSave() })
	sw.saveBtn.Importance = widget.HighImportance
	sw.saveBtn.Disable() // nothing to save until something changes

	cancelBtn := newButton("Cancel", func() {
		if sw.isDirty() {
			dialog.ShowConfirm("Discard changes?",
				"You have unsaved changes. Discard them and close settings?",
				func(discard bool) {
					if discard {
						closeSettings()
					}
				}, ui.mainWin)
			return
		}
		closeSettings()
	})
	buttonRow := container.NewBorder(nil, nil,
		container.NewCenter(sw.dirtyText),
		container.NewHBox(cancelBtn, sw.saveBtn), nil)

	fields := container.NewVBox(
		sw.tipRow("FPS", tipFPS, sw.fpsEntry),
		sw.tipRow("Bitrate (kbit/s)", tipBitrate, sw.bitrateEntry),
		container.NewHBox(sw.audioCheck, sw.helpTip(tipAudio)),
		container.NewHBox(sw.cursorCheck, sw.helpTip(tipCursor)),
		sw.tipRow("Container", tipContainer, sw.containerSel),
		widget.NewSeparator(),
		sw.tipRow("Max Duration (s, 0 = unlimited)", tipMaxDur, sw.maxDurEntry),
		sw.tipRow("Threads (0 = auto)", tipThreads, sw.threadsEntry),
		sw.tipRow("QP (0 = use bitrate)", tipQP, sw.qpEntry),
		sw.tipRow("Nice Priority (0 = default)", tipNice, sw.niceEntry),
	)

	scroll := container.NewVScroll(fields)
	scroll.SetMinSize(fyne.NewSize(660, 540))

	title := widget.NewLabelWithStyle("Recording Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	cardInner := container.NewBorder(
		container.NewVBox(title, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), buttonRow),
		nil, nil, scroll)

	// Bordered, padded card.
	cardBg := canvas.NewRectangle(color.NRGBA{0x1e, 0x1e, 0x1e, 0xff})
	cardBg.CornerRadius = 16
	cardBg.StrokeColor = color.NRGBA{0x5a, 0x5a, 0x5a, 0xff}
	cardBg.StrokeWidth = 1.5
	card := newTapableContainer(container.NewStack(cardBg, insetBy(cardInner, 20)), func() {})
	// WithoutLayout lets us position the card manually so the open/close
	// animation can slide it (Center would keep re-centering it).
	cardLayer := container.NewWithoutLayout(card)

	// Frosted backdrop (blurrier than the preview modal). No-op tap target so the
	// modal blocks the app behind it; dismiss with Save or Cancel.
	var bgFade func(vis float32)
	var bgObj fyne.CanvasObject
	if shot := cv.Capture(); shot != nil {
		blur := canvas.NewImageFromImage(blurredBackdrop(shot, 9))
		blur.FillMode = canvas.ImageFillStretch
		bgObj = blur
		bgFade = func(vis float32) {
			blur.Translucency = float64(1 - vis)
			canvas.Refresh(blur)
		}
	} else {
		rect := canvas.NewRectangle(color.NRGBA{0, 0, 0, 0x99})
		bgObj = rect
		bgFade = func(vis float32) {
			rect.FillColor = color.NRGBA{0, 0, 0, uint8(0x99 * vis)}
			canvas.Refresh(rect)
		}
	}
	backdrop := newTapableContainer(bgObj, func() {})

	sw.overlay = container.NewStack(backdrop, cardLayer)

	// place positions the card centered, offset down by yOff (for the slide).
	place := func(yOff float32) {
		sz := card.MinSize()
		cs := cv.Size()
		card.Resize(sz)
		card.Move(fyne.NewPos((cs.Width-sz.Width)/2, (cs.Height-sz.Height)/2+yOff))
	}

	// startClose animates out, then removes the overlay and runs cleanup.
	startClose = func() {
		if sw.revertTimer != nil {
			sw.revertTimer.Stop()
		}
		anim := fyne.NewAnimation(160*time.Millisecond, func(f float32) {
			bgFade(1 - f)
			place(28 * f)
			if f >= 1 {
				cv.Overlays().Remove(sw.overlay)
				ui.settingsWin = nil
				ui.syncQuickControls()
				ui.refreshConfigSummary()
			}
		})
		anim.Curve = fyne.AnimationEaseIn
		anim.Start()
	}

	ui.settingsWin = sw

	// Set the pre-animation state before adding, so the first frame is already
	// positioned (no flash), then animate in: backdrop fades up, card slides.
	bgFade(0)
	place(28)
	cv.Overlays().Add(sw.overlay)
	sw.overlay.Resize(cv.Size())
	place(28)

	openAnim := fyne.NewAnimation(200*time.Millisecond, func(f float32) {
		bgFade(f)
		place(28 * (1 - f))
	})
	openAnim.Curve = fyne.AnimationEaseOut
	openAnim.Start()
}

// tipRow builds a "Label [?]  [field]" row.
func (sw *settingsWindow) tipRow(labelText, tip string, field fyne.CanvasObject) fyne.CanvasObject {
	left := container.NewHBox(widget.NewLabel(labelText), sw.helpTip(tip))
	return container.NewBorder(nil, nil, left, nil, field)
}

// helpTip returns a small "?" button that pops up a plain-language explanation.
func (sw *settingsWindow) helpTip(text string) fyne.CanvasObject {
	btn := newButtonWithIcon("", theme.QuestionIcon(), nil)
	btn.Importance = widget.LowImportance
	btn.OnTapped = func() {
		if sw.ui == nil || sw.ui.mainWin == nil {
			return
		}
		cv := sw.ui.mainWin.Canvas()
		lbl := widget.NewLabel(text)
		lbl.Wrapping = fyne.TextWrapWord

		bg := canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
		bg.CornerRadius = 8
		bg.StrokeColor = color.NRGBA{0x55, 0x55, 0x55, 0xff}
		bg.StrokeWidth = 1
		content := container.NewStack(bg, container.NewPadded(lbl))

		const w = float32(300)
		wrapped := container.NewGridWrap(fyne.NewSize(w, helpTipHeight(text, w)), content)
		pop := widget.NewPopUp(wrapped, cv)
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(btn)
		pop.ShowAtPosition(fyne.NewPos(pos.X+22, pos.Y))
	}
	return btn
}

// helpTipHeight estimates the height a wrapped tip needs at the given width.
func helpTipHeight(text string, width float32) float32 {
	const charW = 6.6 // ~ average glyph width at the label's text size
	perLine := int((width - 28) / charW)
	if perLine < 1 {
		perLine = 1
	}
	lines := (len(text) + perLine - 1) / perLine
	if lines < 1 {
		lines = 1
	}
	return float32(lines)*22 + 24
}

// isDirty reports whether any staged widget value differs from the saved config.
// Entry fields compare by their normalized string form, so invalid/partial text
// (e.g. "" or "3") counts as a pending change until saved.
func (sw *settingsWindow) isDirty() bool {
	c := sw.config
	return sw.fpsEntry.Text != strconv.Itoa(c.GetFPS()) ||
		sw.bitrateEntry.Text != strconv.Itoa(c.GetBitrate()) ||
		sw.maxDurEntry.Text != strconv.Itoa(c.GetMaxDur()) ||
		sw.threadsEntry.Text != strconv.Itoa(c.GetThreads()) ||
		sw.qpEntry.Text != strconv.Itoa(c.GetQP()) ||
		sw.niceEntry.Text != strconv.Itoa(c.GetNice()) ||
		sw.audioCheck.Checked != c.GetAudio() ||
		sw.cursorCheck.Checked != c.GetCursor() ||
		sw.containerSel.Selected != c.GetContainer()
}

// refreshDirty updates the Save button and the "unsaved changes" indicator. It's
// called on every edit, so any change also cancels a lingering "Saved!" flash.
func (sw *settingsWindow) refreshDirty() {
	// Guard against change callbacks firing during construction (e.g.
	// SetChecked/SetSelected before every widget exists).
	if sw.saveBtn == nil {
		return
	}
	if sw.revertTimer != nil {
		sw.revertTimer.Stop()
		sw.revertTimer = nil
	}
	dirty := sw.isDirty()
	sw.saveBtn.SetText("Save")
	sw.saveBtn.Importance = widget.HighImportance
	if dirty {
		sw.dirtyText.Text = "●  Unsaved changes"
		sw.saveBtn.Enable()
	} else {
		sw.dirtyText.Text = ""
		sw.saveBtn.Disable()
	}
	sw.dirtyText.Refresh()
	sw.saveBtn.Refresh()
}

// onSave applies + persists the settings, then flashes "Saved!" on the button
// (without closing the modal) and reverts it back to a disabled "Save".
func (sw *settingsWindow) onSave() {
	sw.save()
	if sw.ui != nil {
		sw.ui.persistConfig()
	}

	sw.dirtyText.Text = ""
	sw.dirtyText.Refresh()
	// Stay enabled during the flash so the success (green) color shows — a
	// disabled button renders grey regardless of importance. Re-tapping it just
	// re-saves the same values, which is harmless.
	sw.saveBtn.SetText("Saved!")
	sw.saveBtn.Importance = widget.SuccessImportance
	sw.saveBtn.Enable()
	sw.saveBtn.Refresh()

	if sw.revertTimer != nil {
		sw.revertTimer.Stop()
	}
	sw.revertTimer = time.AfterFunc(1300*time.Millisecond, func() {
		sw.ui.runOnMain(func() {
			sw.saveBtn.SetText("Save")
			sw.saveBtn.Importance = widget.HighImportance
			sw.saveBtn.Disable()
			sw.saveBtn.Refresh()
		})
	})
}

// save applies the staged widget values to the config and normalizes the entry
// text back to the validated values (so invalid input snaps back and the fields
// read as clean afterwards).
func (sw *settingsWindow) save() {
	c := sw.config
	if fps, err := strconv.Atoi(sw.fpsEntry.Text); err == nil && fps > 0 {
		c.SetFPS(fps)
	}
	if bitrate, err := strconv.Atoi(sw.bitrateEntry.Text); err == nil && bitrate > 0 {
		c.SetBitrate(bitrate)
	}
	if maxDur, err := strconv.Atoi(sw.maxDurEntry.Text); err == nil && maxDur >= 0 {
		c.SetMaxDur(maxDur)
	}
	if threads, err := strconv.Atoi(sw.threadsEntry.Text); err == nil && threads >= 0 {
		c.SetThreads(threads)
	}
	if qp, err := strconv.Atoi(sw.qpEntry.Text); err == nil && qp >= 0 {
		c.SetQP(qp)
	}
	if nice, err := strconv.Atoi(sw.niceEntry.Text); err == nil {
		c.SetNice(nice)
	}
	c.SetAudio(sw.audioCheck.Checked)
	c.SetCursor(sw.cursorCheck.Checked)
	if sw.containerSel.Selected != "" {
		c.SetContainer(sw.containerSel.Selected)
	}

	// Snap entries back to the validated config values (also clears dirtiness
	// from any rejected input). OnChanged handlers are inert here since the text
	// now matches the config.
	sw.fpsEntry.SetText(strconv.Itoa(c.GetFPS()))
	sw.bitrateEntry.SetText(strconv.Itoa(c.GetBitrate()))
	sw.maxDurEntry.SetText(strconv.Itoa(c.GetMaxDur()))
	sw.threadsEntry.SetText(strconv.Itoa(c.GetThreads()))
	sw.qpEntry.SetText(strconv.Itoa(c.GetQP()))
	sw.niceEntry.SetText(strconv.Itoa(c.GetNice()))

	if sw.ui != nil {
		sw.ui.syncQuickControls()
		sw.ui.refreshConfigSummary()
	}
}

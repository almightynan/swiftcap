package uiapp

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type settingsWindow struct {
	win    fyne.Window
	config *RecordingConfig
	ui     *RecordingUI

	fpsEntry       *widget.Entry
	bitrateEntry   *widget.Entry
	audioCheck     *widget.Check
	cursorCheck    *widget.Check
	containerSel   *widget.Select
	regionLabel    *widget.Label
	regionBtn      *widget.Button
	maxDurEntry    *widget.Entry
	threadsEntry   *widget.Entry
	qpEntry        *widget.Entry
	niceEntry      *widget.Entry
}

func (ui *RecordingUI) showSettings() {
	if ui.settingsWin != nil {
		ui.settingsWin.win.Show()
		ui.settingsWin.win.RequestFocus()
		return
	}

	sw := &settingsWindow{
		config: ui.config,
		ui:     ui,
	}

	win := ui.app.NewWindow("Recording Settings")
	win.Resize(fyne.NewSize(500, 600))
	win.SetFixedSize(false)

	// FPS
	fpsLabel := widget.NewLabel("FPS:")
	sw.fpsEntry = widget.NewEntry()
	sw.fpsEntry.SetText(strconv.Itoa(sw.config.GetFPS()))
	sw.fpsEntry.SetPlaceHolder("30")
	fpsRow := container.NewBorder(nil, nil, fpsLabel, nil, sw.fpsEntry)

	// Bitrate
	bitrateLabel := widget.NewLabel("Bitrate (kbit/s):")
	sw.bitrateEntry = widget.NewEntry()
	sw.bitrateEntry.SetText(strconv.Itoa(sw.config.GetBitrate()))
	sw.bitrateEntry.SetPlaceHolder("4000")
	bitrateRow := container.NewBorder(nil, nil, bitrateLabel, nil, sw.bitrateEntry)

	// Audio
	sw.audioCheck = widget.NewCheck("Record Audio", func(checked bool) {
		sw.config.SetAudio(checked)
	})
	sw.audioCheck.SetChecked(sw.config.GetAudio())

	// Cursor
	sw.cursorCheck = widget.NewCheck("Show Cursor", func(checked bool) {
		sw.config.SetCursor(checked)
	})
	sw.cursorCheck.SetChecked(sw.config.GetCursor())

	// Container
	containerLabel := widget.NewLabel("Container:")
	sw.containerSel = widget.NewSelect([]string{"mp4", "mkv"}, func(selected string) {
		sw.config.SetContainer(selected)
	})
	sw.containerSel.SetSelected(sw.config.GetContainer())
	containerRow := container.NewBorder(nil, nil, containerLabel, nil, sw.containerSel)

	// Region
	regionLabel := widget.NewLabel("Region:")
	sw.regionLabel = widget.NewLabel("Full Screen")
	sw.regionBtn = widget.NewButton("Select Region", func() {
		sw.selectRegion()
	})
	clearRegionBtn := widget.NewButton("Clear", func() {
		sw.config.SetRegion("")
		sw.updateRegionLabel()
	})
	regionRow := container.NewBorder(nil, nil, regionLabel, nil,
		container.NewHBox(sw.regionLabel, sw.regionBtn, clearRegionBtn))

	// Max Duration
	maxDurLabel := widget.NewLabel("Max Duration (seconds, 0 = unlimited):")
	sw.maxDurEntry = widget.NewEntry()
	sw.maxDurEntry.SetText(strconv.Itoa(sw.config.GetMaxDur()))
	sw.maxDurEntry.SetPlaceHolder("0")
	maxDurRow := container.NewBorder(nil, nil, maxDurLabel, nil, sw.maxDurEntry)

	// Threads
	threadsLabel := widget.NewLabel("Threads (0 = auto):")
	sw.threadsEntry = widget.NewEntry()
	sw.threadsEntry.SetText(strconv.Itoa(sw.config.GetThreads()))
	sw.threadsEntry.SetPlaceHolder("0")
	threadsRow := container.NewBorder(nil, nil, threadsLabel, nil, sw.threadsEntry)

	// QP
	qpLabel := widget.NewLabel("QP (0 = use bitrate):")
	sw.qpEntry = widget.NewEntry()
	sw.qpEntry.SetText(strconv.Itoa(sw.config.GetQP()))
	sw.qpEntry.SetPlaceHolder("0")
	qpRow := container.NewBorder(nil, nil, qpLabel, nil, sw.qpEntry)

	// Nice
	niceLabel := widget.NewLabel("Nice Priority (0 = default):")
	sw.niceEntry = widget.NewEntry()
	sw.niceEntry.SetText(strconv.Itoa(sw.config.GetNice()))
	sw.niceEntry.SetPlaceHolder("0")
	niceRow := container.NewBorder(nil, nil, niceLabel, nil, sw.niceEntry)

	// Buttons
	saveBtn := widget.NewButton("Save", func() {
		sw.save()
		win.Close()
	})
	cancelBtn := widget.NewButton("Cancel", func() {
		win.Close()
	})
	buttonRow := container.NewBorder(nil, nil, nil, container.NewHBox(cancelBtn, saveBtn), widget.NewLabel(""))

	content := container.NewVBox(
		widget.NewLabelWithStyle("Recording Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		fpsRow,
		bitrateRow,
		sw.audioCheck,
		sw.cursorCheck,
		containerRow,
		regionRow,
		maxDurRow,
		threadsRow,
		qpRow,
		niceRow,
		widget.NewSeparator(),
		buttonRow,
	)

	win.SetContent(container.NewPadded(content))
	sw.win = win
	ui.settingsWin = sw
	sw.updateRegionLabel()

	win.SetOnClosed(func() {
		ui.settingsWin = nil
	})

	win.Show()
}

func (sw *settingsWindow) updateRegionLabel() {
	region := sw.config.GetRegion()
	if region == "" {
		sw.regionLabel.SetText("Full Screen")
	} else {
		sw.regionLabel.SetText(region)
	}
}

func (sw *settingsWindow) selectRegion() {
	selector := newRegionSelector(sw.ui.app, func(region string) {
		sw.config.SetRegion(region)
		sw.updateRegionLabel()
	}, func() {
		// Cancelled
	})
	selector.Show()
}

func (sw *settingsWindow) save() {
	// FPS
	if fps, err := strconv.Atoi(sw.fpsEntry.Text); err == nil && fps > 0 {
		sw.config.SetFPS(fps)
	}

	// Bitrate
	if bitrate, err := strconv.Atoi(sw.bitrateEntry.Text); err == nil && bitrate > 0 {
		sw.config.SetBitrate(bitrate)
	}

	// Max Duration
	if maxDur, err := strconv.Atoi(sw.maxDurEntry.Text); err == nil && maxDur >= 0 {
		sw.config.SetMaxDur(maxDur)
	}

	// Threads
	if threads, err := strconv.Atoi(sw.threadsEntry.Text); err == nil && threads >= 0 {
		sw.config.SetThreads(threads)
	}

	// QP
	if qp, err := strconv.Atoi(sw.qpEntry.Text); err == nil && qp >= 0 {
		sw.config.SetQP(qp)
	}

	// Nice
	if nice, err := strconv.Atoi(sw.niceEntry.Text); err == nil {
		sw.config.SetNice(nice)
	}
}


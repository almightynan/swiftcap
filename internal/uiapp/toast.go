package uiapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type toastHandle struct {
	popup  *widget.PopUp
	cancel chan struct{}
}

func (ui *RecordingUI) showToast(path string) {
	ui.runOnMain(func() {
		if ui.mainWin == nil {
			return
		}
		if ui.toast != nil {
			close(ui.toast.cancel)
			ui.toast.popup.Hide()
			ui.toast = nil
		}

		title := widget.NewLabelWithStyle("Recording saved:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		pathLabel := widget.NewLabel(path)
		pathLabel.Wrapping = fyne.TextWrapWord

		openFolderBtn := widget.NewButton("Open Folder", func() {
			if err := openFolder(path); err != nil {
				ui.showError("Open Folder", err.Error())
			}
		})
		openFileBtn := widget.NewButton("Open File", func() {
			if err := openFile(path); err != nil {
				ui.showError("Open File", err.Error())
			}
		})

		buttonRow := container.NewHBox(openFolderBtn, openFileBtn)
		content := container.NewVBox(title, pathLabel, buttonRow)
		popup := widget.NewModalPopUp(content, ui.mainWin.Canvas())
		popup.Show()

		cancel := make(chan struct{})
		ui.toast = &toastHandle{popup: popup, cancel: cancel}
		go func() {
			select {
			case <-time.After(7 * time.Second):
				ui.runOnMain(func() {
					if ui.toast != nil && ui.toast.popup == popup {
						popup.Hide()
						ui.toast = nil
					}
				})
			case <-cancel:
			}
		}()
	})
}

func openFolder(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	target := path
	if !info.IsDir() {
		target = filepath.Dir(path)
	}
	return openPath(target)
}

func openFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return openPath(path)
}

func openPath(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}

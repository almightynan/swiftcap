package uiapp

import (
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type recordingsList struct {
	grid      *fyne.Container
	items     []recordingItem
	onSelect  func(path string)
	scroll    *container.Scroll
	app       fyne.App // Store app reference for refreshing
}

type recordingItem struct {
	name     string
	path     string
	size     string
	modified time.Time
	thumbnail string
}

func newRecordingsList(videosDir string, onSelect func(path string)) *recordingsList {
	rl := &recordingsList{
		onSelect: onSelect,
		items:    []recordingItem{},
	}
	
	rl.grid = container.NewGridWithColumns(4)
	rl.scroll = container.NewScroll(rl.grid)
	rl.scroll.SetMinSize(fyne.NewSize(0, 360))
	
	rl.refresh(videosDir)
	return rl
}

func (rl *recordingsList) refresh(videosDir string) {
	rl.items = []recordingItem{}
	
	if videosDir == "" {
		rl.grid.Objects = []fyne.CanvasObject{}
		rl.grid.Refresh()
		return
	}
	
	entries, err := os.ReadDir(videosDir)
	if err != nil {
		return
	}
	
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		name := entry.Name()
		if !isVideoFile(name) {
			continue
		}
		
		path := filepath.Join(videosDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		// Check for existing thumbnail first
		thumbPath := path + ".thumb.jpg"
		thumbnail := ""
		if _, err := os.Stat(thumbPath); err == nil {
			thumbnail = thumbPath
		} else {
			// Generate thumbnail (async, will be empty initially)
			generateThumbnail(path)
		}
		
		item := recordingItem{
			name:      name,
			path:      path,
			size:      formatSize(info.Size()),
			modified:  info.ModTime(),
			thumbnail: thumbnail,
		}
		rl.items = append(rl.items, item)
	}
	
	// Sort by modified time (newest first)
	sort.Slice(rl.items, func(i, j int) bool {
		return rl.items[i].modified.After(rl.items[j].modified)
	})
	
	// Clear and rebuild grid
	rl.grid.Objects = []fyne.CanvasObject{}
	
	for _, item := range rl.items {
		card := rl.createCard(item)
		rl.grid.Add(card)
	}
	
	rl.grid.Refresh()
}

func (rl *recordingsList) createCard(item recordingItem) fyne.CanvasObject {
	// Thumbnail image - only load if file exists
	var img *canvas.Image
	if item.thumbnail != "" {
		if _, err := os.Stat(item.thumbnail); err == nil {
			// File exists, load it
			img = canvas.NewImageFromFile(item.thumbnail)
		} else {
			// File doesn't exist yet, use placeholder
			img = canvas.NewImageFromResource(nil)
		}
	} else {
		// No thumbnail path, use placeholder
		img = canvas.NewImageFromResource(nil)
	}
	img.FillMode = canvas.ImageFillStretch
	img.SetMinSize(fyne.NewSize(180, 102))
	thumbBg := canvas.NewRectangle(blendColor(bentoSurface, accentSky, 0.06))
	thumbBg.StrokeColor = bentoBorder
	thumbBg.StrokeWidth = 1
	thumb := container.NewMax(thumbBg, img)
	
	nameLabel := widget.NewLabelWithStyle(item.name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	nameLabel.Wrapping = fyne.TextWrapOff
	nameLabel.Truncation = fyne.TextTruncateEllipsis
	
	timeLabel := widget.NewLabel(formatTime(item.modified))
	timeLabel.Importance = widget.LowImportance
	sizeLabel := widget.NewLabel(item.size)
	sizeLabel.Importance = widget.LowImportance
	metaRow := container.NewHBox(timeLabel, layout.NewSpacer(), sizeLabel)

	infoSection := container.NewVBox(
		nameLabel,
		metaRow,
	)

	cardContent := container.NewVBox(
		thumb,
		container.NewPadded(infoSection),
	)

	card := bentoTile(accentSky, cardContent)
	
	// Create a tapable container
	tapContainer := newTapableContainer(card, func() {
		if rl.onSelect != nil {
			rl.onSelect(item.path)
		}
	})
	
	// Minimal padding for compact, modern cards
	return container.NewPadded(tapContainer)
}

func (rl *recordingsList) getContainer() fyne.CanvasObject {
	return rl.scroll
}

func generateThumbnail(videoPath string) string {
	// Check if thumbnail already exists
	thumbPath := videoPath + ".thumb.jpg"
	if _, err := os.Stat(thumbPath); err == nil {
		return thumbPath
	}
	
	// Check if video file exists before trying to generate thumbnail
	if _, err := os.Stat(videoPath); err != nil {
		return "" // Video doesn't exist, can't generate thumbnail
	}
	
	// Generate thumbnail using ffmpeg
	// Extract frame at 1 second (or first frame if video is shorter)
	// Scale to smaller size for compact cards
	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-ss", "00:00:01",
		"-vframes", "1",
		"-vf", "scale=180:102:force_original_aspect_ratio=increase,crop=180:102",
		"-q:v", "2", // High quality
		"-y", // Overwrite
		thumbPath,
	)
	
	// Run in background, don't wait - but capture errors
	cmd.Stdout = nil
	cmd.Stderr = nil
	go func() {
		if err := cmd.Run(); err == nil {
			// Thumbnail generated successfully, refresh the list
			// Note: This would require access to the recordingsList instance
			// For now, thumbnails will appear on next refresh
		}
	}()
	
	// Return empty string since thumbnail doesn't exist yet
	// The UI will show placeholder until it's generated
	return ""
}

func isVideoFile(name string) bool {
	ext := filepath.Ext(name)
	videoExts := []string{".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv"}
	for _, e := range videoExts {
		if ext == e {
			return true
		}
	}
	return false
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// tapableContainer is a container that handles tap events
type tapableContainer struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTapableContainer(content fyne.CanvasObject, onTap func()) *tapableContainer {
	t := &tapableContainer{
		content: content,
		onTap:   onTap,
	}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tapableContainer) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(t.content)
}

func (t *tapableContainer) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}

func formatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)
	
	if diff < time.Minute {
		return "Just now"
	}
	if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	return t.Format("Jan 2, 2006")
}

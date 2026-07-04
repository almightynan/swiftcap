package uiapp

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ─── data model ──────────────────────────────────────────────────────────────

type recordingItem struct {
	name      string
	path      string
	size      string
	modified  time.Time
	isVideo   bool
	thumbPath string // .thumb.jpg sidecar if available
}

// ─── list controller ─────────────────────────────────────────────────────────

type recordingsList struct {
	box      *fyne.Container
	scroll   *container.Scroll
	onSelect func(string)
}

func newRecordingsList(videosDir, screenshotsDir string, onSelect func(string)) *recordingsList {
	rl := &recordingsList{onSelect: onSelect}
	rl.box = container.NewGridWithColumns(3)
	rl.scroll = container.NewVScroll(rl.box)
	rl.scroll.SetMinSize(fyne.NewSize(0, 220))
	rl.refresh(videosDir, screenshotsDir)
	return rl
}

func (rl *recordingsList) refresh(dirs ...string) {
	items := loadItems(dirs...)
	rl.box.Objects = nil
	if len(items) == 0 {
		empty := canvas.NewText("No captures yet", color.NRGBA{0x58, 0x58, 0x58, 0xff})
		empty.TextSize = 13
		empty.Alignment = fyne.TextAlignCenter
		rl.box.Add(container.NewCenter(empty))
	} else {
		for _, item := range items {
			item := item
			card := newCaptureCard(item, func() {
				if rl.onSelect != nil {
					rl.onSelect(item.path)
				}
			})
			rl.box.Add(card)
		}
	}
	rl.box.Refresh()
}

func (rl *recordingsList) getContainer() fyne.CanvasObject { return rl.scroll }

// ─── item loader ─────────────────────────────────────────────────────────────

// maxRecentItems caps how many capture cards the "Recent Captures" panel builds.
// Each card decodes and downscales its source image on a goroutine, so an
// unbounded list (e.g. a Pictures folder with thousands of files) would spawn
// thousands of full-resolution decodes and freeze the UI. This is a "recent"
// panel, not a gallery — the most recent handful is all it needs to show.
const maxRecentItems = 60

func loadItems(dirs ...string) []recordingItem {
	seen := make(map[string]bool)
	var items []recordingItem
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !isCaptureFile(ext) {
				continue
			}
			// Only list SwiftCap's own captures. Otherwise a shared folder like
			// ~/Pictures (which can hold thousands of unrelated images) would all
			// get loaded here — the source of the app's startup/screenshot lag.
			if !isSwiftCapCapture(name) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			path := filepath.Join(dir, name)
			if seen[path] {
				continue
			}
			seen[path] = true
			item := recordingItem{
				name:     name,
				path:     path,
				size:     formatSize(info.Size()),
				modified: info.ModTime(),
				isVideo:  isVideoExt(ext),
			}
			if tp := path + ".thumb.jpg"; fileExists(tp) {
				item.thumbPath = tp
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].modified.After(items[j].modified)
	})
	if len(items) > maxRecentItems {
		items = items[:maxRecentItems]
	}
	return items
}

// isSwiftCapCapture reports whether a filename is one of SwiftCap's own final
// captures: screenshots (swiftcap_*, swiftcap_markup_*) or recordings
// (recording_*). Recording intermediates (segments, concat lists, temp
// backgrounds) and generated video thumbnail sidecars are excluded so they
// never show up as their own cards.
func isSwiftCapCapture(name string) bool {
	if strings.HasSuffix(name, ".thumb.jpg") ||
		strings.Contains(name, "_segment_") ||
		strings.HasPrefix(name, "swiftcap_segment_") ||
		strings.HasPrefix(name, "swiftcap_concat_") ||
		strings.HasPrefix(name, "swiftcap_rec_bg_") {
		return false
	}
	return strings.HasPrefix(name, "swiftcap_") || strings.HasPrefix(name, "recording_")
}

func isCaptureFile(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv",
		".png", ".jpg", ".jpeg", ".webp", ".bmp":
		return true
	}
	return false
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".webm", ".flv":
		return true
	}
	return false
}


// ─── capture card widget ─────────────────────────────────────────────────────

const (
	cardThumbH = float32(110) // thumbnail height (card width is grid-determined)
	cardH      = float32(184) // total: 2×inset + 2×pad + thumb + gap + name + gap + meta + pad
	cardInset  = float32(6)   // gap between grid cells (half-gap on each side)
	cardPad    = float32(7)   // inner padding between card border and content
)

type captureCard struct {
	widget.BaseWidget
	item  recordingItem
	onTap func()

	mu      sync.Mutex
	hovered bool

	// Populated async for screenshots/video thumbs.
	thumbImg image.Image
	thumbLoaded bool
}

func newCaptureCard(item recordingItem, onTap func()) *captureCard {
	c := &captureCard{item: item, onTap: onTap}
	c.ExtendBaseWidget(c)
	// Load thumbnail asynchronously.
	go c.loadThumb()
	return c
}

func (c *captureCard) loadThumb() {
	var img image.Image
	if c.item.thumbPath != "" {
		img = loadAnyImage(c.item.thumbPath)
	} else if !c.item.isVideo {
		img = loadAnyImage(c.item.path)
	} else {
		// For videos with no sidecar, generate one if possible.
		thumb := c.item.path + ".thumb.jpg"
		if err := generateThumb(c.item.path, thumb); err == nil {
			img = loadAnyImage(thumb)
		}
	}
	// Downscale to a small thumbnail ONCE here (off the UI thread). Screenshots
	// load at full resolution (e.g. 2560×1440); keeping them full-size means Fyne
	// re-resamples the whole image every time a card resizes (e.g. on every sidebar
	// toggle), which is what made toggling take seconds. A small image resizes instantly.
	img = downscaleThumb(img, 360, 220)

	c.mu.Lock()
	c.thumbImg = img
	c.thumbLoaded = true
	c.mu.Unlock()
	c.Refresh()
}

// downscaleThumb shrinks src to fit within maxW×maxH (preserving aspect ratio),
// returning src unchanged if it already fits. Runs off the UI thread.
func downscaleThumb(src image.Image, maxW, maxH int) image.Image {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || (sw <= maxW && sh <= maxH) {
		return src
	}
	scale := float64(maxW) / float64(sw)
	if s := float64(maxH) / float64(sh); s < scale {
		scale = s
	}
	dw := int(float64(sw) * scale)
	dh := int(float64(sh) * scale)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func (c *captureCard) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.NRGBA{0x2a, 0x2a, 0x2a, 0xff})
	bg.CornerRadius = 10
	bg.StrokeColor = color.NRGBA{0x42, 0x42, 0x42, 0xff}
	bg.StrokeWidth = 1.5

	thumbBg := canvas.NewRectangle(color.NRGBA{0x12, 0x12, 0x12, 0xff})
	thumbBg.CornerRadius = 8

	thumbImg := canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	thumbImg.FillMode = canvas.ImageFillContain
	thumbImg.ScaleMode = canvas.ImageScaleFastest // thumbnails are small; skip costly smooth resampling

	placeholder := canvas.NewText("", color.NRGBA{0x44, 0x44, 0x44, 0xff})
	placeholder.TextSize = 24
	placeholder.Alignment = fyne.TextAlignCenter

	nameText := canvas.NewText("", color.NRGBA{0xee, 0xee, 0xee, 0xff})
	nameText.TextSize = 12
	nameText.TextStyle = fyne.TextStyle{Bold: true}

	metaText := canvas.NewText("", color.NRGBA{0x70, 0x70, 0x70, 0xff})
	metaText.TextSize = 11

	badgeBg := canvas.NewRectangle(color.NRGBA{0, 0, 0, 0})
	badgeBg.CornerRadius = 4
	badgeText := canvas.NewText("", color.NRGBA{0xff, 0xff, 0xff, 0xff})
	badgeText.TextSize = 10
	badgeText.TextStyle = fyne.TextStyle{Bold: true}
	badgeText.Alignment = fyne.TextAlignCenter

	// Precompute everything static — done ONCE here, never per Layout.
	displayName := cleanCaptureName(c.item.name)
	metaText.Text = formatTime(c.item.modified) + "  ·  " + c.item.size

	badgeLabel := "Video"
	badgeColor := color.NRGBA{0x22, 0x44, 0x77, 0xd8}
	badgeFg := color.NRGBA{0xaa, 0xcc, 0xff, 0xff}
	placeholderSym := "▶"
	if !c.item.isVideo {
		badgeLabel = "Photo"
		badgeColor = color.NRGBA{0x1e, 0x55, 0x35, 0xd8}
		badgeFg = color.NRGBA{0x88, 0xdd, 0xaa, 0xff}
		placeholderSym = "✦"
	}
	badgeBg.FillColor = badgeColor
	badgeText.Text = badgeLabel
	badgeText.Color = badgeFg

	r := &captureCardRenderer{
		c:            c,
		bg:           bg,
		thumbBg:      thumbBg,
		thumbImg:     thumbImg,
		placeholder:  placeholder,
		nameText:     nameText,
		metaText:     metaText,
		badgeBg:      badgeBg,
		badgeText:    badgeText,
		displayName:  displayName,
		placeholderS: placeholderSym,
		hoverApplied: -1,
		thumbApplied: -1,
		lastW:        -1,
	}
	r.objs = []fyne.CanvasObject{
		bg, thumbBg, thumbImg, placeholder,
		badgeBg, badgeText,
		nameText, metaText,
	}
	return r
}

func (c *captureCard) MinSize() fyne.Size { return fyne.NewSize(150, cardH) }

func (c *captureCard) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}
func (c *captureCard) TappedSecondary(*fyne.PointEvent) {}

// Cursor makes the capture tiles show a pointer, signalling they're clickable.
func (c *captureCard) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (c *captureCard) MouseIn(*desktop.MouseEvent) {
	c.mu.Lock()
	c.hovered = true
	c.mu.Unlock()
	c.Refresh()
}
func (c *captureCard) MouseOut() {
	c.mu.Lock()
	c.hovered = false
	c.mu.Unlock()
	c.Refresh()
}
func (c *captureCard) MouseMoved(*desktop.MouseEvent) {}

// ─── card renderer ────────────────────────────────────────────────────────────

type captureCardRenderer struct {
	c           *captureCard
	bg          *canvas.Rectangle
	thumbBg     *canvas.Rectangle
	thumbImg    *canvas.Image
	placeholder *canvas.Text
	nameText    *canvas.Text
	metaText    *canvas.Text
	badgeBg     *canvas.Rectangle
	badgeText   *canvas.Text
	objs        []fyne.CanvasObject

	// Cached so Layout stays pure-geometry. These only change on hover, thumb
	// load, or a real width change — never on a plain grid re-layout (e.g. when
	// the sidebar toggles). Sentinels start at -1 so the first Layout applies all.
	displayName  string
	placeholderS string
	hoverApplied int
	thumbApplied int
	lastW        float32
}

func (r *captureCardRenderer) Layout(size fyne.Size) {
	c := r.c
	c.mu.Lock()
	hovered := c.hovered
	thumbLoaded := c.thumbLoaded
	thumbImg := c.thumbImg
	c.mu.Unlock()

	// ── Geometry only (cheap Move/Resize, no rasterization) ──────────────────
	bgX := cardInset
	bgY := cardInset
	bgW := size.Width - cardInset*2
	bgH := size.Height - cardInset*2
	r.bg.Move(fyne.NewPos(bgX, bgY))
	r.bg.Resize(fyne.NewSize(bgW, bgH))

	cX := bgX + cardPad
	cY := bgY + cardPad
	cW := bgW - cardPad*2

	r.thumbBg.Move(fyne.NewPos(cX, cY))
	r.thumbBg.Resize(fyne.NewSize(cW, cardThumbH))

	r.thumbImg.Move(fyne.NewPos(cX, cY))
	r.thumbImg.Resize(fyne.NewSize(cW, cardThumbH))
	r.placeholder.Move(fyne.NewPos(cX, cY+cardThumbH/2-16))
	r.placeholder.Resize(fyne.NewSize(cW, 32))

	const badgeW, badgeH = float32(44), float32(17)
	badgeX := cX + cW - badgeW - 5
	badgeY := cY + 5
	r.badgeBg.Move(fyne.NewPos(badgeX, badgeY))
	r.badgeBg.Resize(fyne.NewSize(badgeW, badgeH))
	r.badgeText.Move(fyne.NewPos(badgeX, badgeY+2))
	r.badgeText.Resize(fyne.NewSize(badgeW, 13))

	infoY := cY + cardThumbH + 8
	infoX := cX + 2
	infoW := cW - 4
	r.nameText.Move(fyne.NewPos(infoX, infoY))
	r.nameText.Resize(fyne.NewSize(infoW, 16))
	r.metaText.Move(fyne.NewPos(infoX, infoY+19))
	r.metaText.Resize(fyne.NewSize(infoW, 14))

	// ── State-gated content updates (only when something actually changed) ───
	hv := 0
	if hovered {
		hv = 1
	}
	if r.hoverApplied != hv {
		r.hoverApplied = hv
		if hovered {
			r.bg.FillColor = color.NRGBA{0x35, 0x35, 0x35, 0xff}
			r.bg.StrokeColor = color.NRGBA{0x58, 0x58, 0x58, 0xff}
		} else {
			r.bg.FillColor = color.NRGBA{0x2a, 0x2a, 0x2a, 0xff}
			r.bg.StrokeColor = color.NRGBA{0x42, 0x42, 0x42, 0xff}
		}
		canvas.Refresh(r.bg)
	}

	ts := 0
	if thumbLoaded && thumbImg != nil {
		ts = 1
	}
	if r.thumbApplied != ts {
		r.thumbApplied = ts
		if ts == 1 {
			r.thumbImg.Image = thumbImg
			r.thumbImg.Show()
			r.placeholder.Hide()
			canvas.Refresh(r.thumbImg)
		} else {
			r.placeholder.Text = r.placeholderS
			r.thumbImg.Hide()
			r.placeholder.Show()
			canvas.Refresh(r.placeholder)
		}
	}

	// Name truncation depends on width — recompute only when the width changes,
	// and only re-raster if the truncated string actually differs (short names
	// that never truncate cost nothing on a sidebar toggle).
	if size.Width != r.lastW {
		r.lastW = size.Width
		if nt := truncateText(r.displayName, infoW, r.nameText.TextSize); nt != r.nameText.Text {
			r.nameText.Text = nt
			canvas.Refresh(r.nameText)
		}
	}
}

func (r *captureCardRenderer) MinSize() fyne.Size           { return fyne.NewSize(150, cardH) }
func (r *captureCardRenderer) Refresh()                     { r.Layout(r.c.Size()) }
func (r *captureCardRenderer) Destroy()                     {}
func (r *captureCardRenderer) Objects() []fyne.CanvasObject { return r.objs }

// ─── helpers ──────────────────────────────────────────────────────────────────

// cleanCaptureName turns a raw filename into a human-readable display name.
func cleanCaptureName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	// Strip common app prefixes (space and underscore variants).
	for _, pfx := range []string{
		"recording_", "screenshot_", "swiftcap_", "markup_", "markup ",
	} {
		base = strings.TrimPrefix(base, pfx)
	}
	// YYYYMMDD_HHmmss (e.g. recording_20240115_143022).
	if len(base) >= 15 {
		if t, err := time.Parse("20060102_150405", base[:15]); err == nil {
			return t.Format("Jan 2 · 3:04 PM")
		}
	}
	// Pure digits → Unix nanosecond timestamp (≥18 digits) or millisecond (13-17).
	if isAllDigits(base) && len(base) >= 13 {
		if ns, err := strconv.ParseInt(base, 10, 64); err == nil {
			var t time.Time
			if len(base) >= 18 {
				t = time.Unix(ns/1_000_000_000, ns%1_000_000_000)
			} else {
				t = time.UnixMilli(ns)
			}
			if y := t.Year(); y >= 2020 && y <= 2040 {
				return t.Format("Jan 2 · 3:04 PM")
			}
		}
	}
	// Fallback: clean up underscores.
	base = strings.ReplaceAll(base, "_", " ")
	return base
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// truncateText shortens s so it fits within maxW logical units at the given
// font size, appending "…" when truncated. Uses a simple character-based
// estimate (average 7px per char at size 13).
func truncateText(s string, maxW, fontSize float32) string {
	charsPerPx := fontSize * 0.54 // rough em width
	maxChars := int(maxW / charsPerPx)
	if maxChars < 4 {
		return "…"
	}
	if len([]rune(s)) <= maxChars {
		return s
	}
	return string([]rune(s)[:maxChars-1]) + "…"
}

func generateThumb(videoPath, thumbPath string) error {
	return exec.Command("ffmpeg",
		"-y", "-i", videoPath,
		"-ss", "00:00:01", "-vframes", "1",
		"-vf", "scale=216:124:force_original_aspect_ratio=increase,crop=216:124",
		"-q:v", "3", thumbPath,
	).Run()
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

func formatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)
	switch {
	case diff < time.Minute:
		return "Just now"
	case diff < time.Hour:
		m := int(diff.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case diff < 24*time.Hour:
		h := int(diff.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case diff < 7*24*time.Hour:
		d := int(diff.Hours() / 24)
		if d == 1 {
			return "Yesterday"
		}
		return fmt.Sprintf("%d days ago", d)
	default:
		return t.Format("Jan 2, 2006")
	}
}

// tapableContainer wraps arbitrary content (kept for callers outside this file).
type tapableContainer struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTapableContainer(content fyne.CanvasObject, onTap func()) *tapableContainer {
	t := &tapableContainer{content: content, onTap: onTap}
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
func (t *tapableContainer) TappedSecondary(*fyne.PointEvent) {}

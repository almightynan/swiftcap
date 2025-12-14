package uiapp

import "sync"

type RecordingConfig struct {
	mu sync.RWMutex

	FPS       int
	Bitrate   int
	Audio     bool
	Cursor    bool
	Container string
	Region    string // WxH+X+Y format or empty for full screen
	MaxDur    int    // seconds, 0 = unlimited
	Threads   int
	QP        int
	Nice      int
}

func NewRecordingConfig() *RecordingConfig {
	return &RecordingConfig{
		FPS:       30,
		Bitrate:   4000,
		Audio:     true,
		Cursor:    true,
		Container: "mp4",
		Region:    "",
		MaxDur:    0,
		Threads:   0,
		QP:        0,
		Nice:      0,
	}
}

func (c *RecordingConfig) GetFPS() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.FPS
}

func (c *RecordingConfig) SetFPS(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.FPS = v
}

func (c *RecordingConfig) GetBitrate() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Bitrate
}

func (c *RecordingConfig) SetBitrate(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Bitrate = v
}

func (c *RecordingConfig) GetAudio() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Audio
}

func (c *RecordingConfig) SetAudio(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Audio = v
}

func (c *RecordingConfig) GetCursor() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Cursor
}

func (c *RecordingConfig) SetCursor(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Cursor = v
}

func (c *RecordingConfig) GetContainer() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Container
}

func (c *RecordingConfig) SetContainer(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Container = v
}

func (c *RecordingConfig) GetRegion() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Region
}

func (c *RecordingConfig) SetRegion(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Region = v
}

func (c *RecordingConfig) GetMaxDur() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.MaxDur
}

func (c *RecordingConfig) SetMaxDur(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.MaxDur = v
}

func (c *RecordingConfig) GetThreads() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Threads
}

func (c *RecordingConfig) SetThreads(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Threads = v
}

func (c *RecordingConfig) GetQP() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.QP
}

func (c *RecordingConfig) SetQP(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.QP = v
}

func (c *RecordingConfig) GetNice() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Nice
}

func (c *RecordingConfig) SetNice(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Nice = v
}


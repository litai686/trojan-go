package trojan

import (
	"strings"
	"sync"
	"time"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

const (
	defaultMaxLogLines = 2000
	maxLogLineBytes    = 64 * 1024
)

// LogHub keeps a ring buffer of trojan-go output and emits batched events.
type LogHub struct {
	mu      sync.Mutex
	lines   []store.LogEntry
	pending []store.LogEntry
	max     int
	seq     int64
	emit    func(name string, data any)
	sink    func(entry store.LogEntry)
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	started bool
}

// NewLogHub creates a hub. emit may be nil (useful in tests).
func NewLogHub(emit func(name string, data any), maxLines int) *LogHub {
	if maxLines < 100 {
		maxLines = defaultMaxLogLines
	}
	return &LogHub{
		max:  maxLines,
		emit: emit,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// Start begins the batch flush loop.
func (h *LogHub) Start() {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	h.mu.Unlock()

	go func() {
		defer close(h.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.flush()
			case <-h.stop:
				h.flush()
				return
			}
		}
	}()
}

// Stop flushes pending lines and stops the loop.
func (h *LogHub) Stop() {
	h.once.Do(func() {
		h.mu.Lock()
		started := h.started
		h.mu.Unlock()
		close(h.stop)
		if started {
			<-h.done
		}
	})
}

// SetSink installs an optional persistence callback for every log entry.
func (h *LogHub) SetSink(sink func(entry store.LogEntry)) {
	h.mu.Lock()
	h.sink = sink
	h.mu.Unlock()
}

// SetMax changes the buffer capacity.
func (h *LogHub) SetMax(maxLines int) {
	if maxLines < 100 {
		maxLines = defaultMaxLogLines
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.max = maxLines
	if len(h.lines) > h.max {
		h.lines = append([]store.LogEntry(nil), h.lines[len(h.lines)-h.max:]...)
	}
}

// Add appends a stdout/stderr line.
func (h *LogHub) Add(stream, raw string) {
	h.add(stream, raw, false)
}

// AddSystem appends a desktop-generated line.
func (h *LogHub) AddSystem(text string) {
	h.add("desktop", text, true)
}

func (h *LogHub) add(stream, raw string, system bool) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if raw == "" {
		return
	}
	if len(raw) > maxLogLineBytes {
		raw = raw[:maxLogLineBytes] + " …(truncated)"
	}

	level := "SYSTEM"
	text := raw
	if !system {
		level, text = ParseLogLine(raw)
	}
	if text == "" {
		text = raw
	}

	h.mu.Lock()
	h.seq++
	entry := store.LogEntry{
		Seq:    h.seq,
		Time:   time.Now().UnixMilli(),
		Stream: stream,
		Level:  level,
		Text:   text,
	}
	h.lines = append(h.lines, entry)
	if len(h.lines) > h.max {
		overflow := len(h.lines) - h.max
		h.lines = append([]store.LogEntry(nil), h.lines[overflow:]...)
	}
	h.pending = append(h.pending, entry)
	sink := h.sink
	h.mu.Unlock()
	if sink != nil {
		sink(entry)
	}
}

func (h *LogHub) flush() {
	h.mu.Lock()
	if len(h.pending) == 0 {
		h.mu.Unlock()
		return
	}
	batch := h.pending
	h.pending = nil
	h.mu.Unlock()

	if h.emit != nil {
		h.emit("runtime:log", map[string]any{"lines": batch})
	}
}

// Snapshot returns the last limit lines (all when limit <= 0).
func (h *LogHub) Snapshot(limit int) []store.LogEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit > len(h.lines) {
		limit = len(h.lines)
	}
	out := append([]store.LogEntry(nil), h.lines[len(h.lines)-limit:]...)
	return out
}

// Clear drops all buffered lines.
func (h *LogHub) Clear() {
	h.mu.Lock()
	h.lines = nil
	h.pending = nil
	h.mu.Unlock()
	if h.emit != nil {
		h.emit("runtime:cleared", nil)
	}
}

package alert

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// HistoryFile reads and appends triggered alerts as NDJSON. It is
// concurrency-safe; multiple goroutines may call Append. Missing file
// is treated as empty.
type HistoryFile struct {
	mu   sync.Mutex
	path string
	max  int // upper bound when trimming on LoadAll; 0 means unlimited
}

// NewHistoryFile constructs a HistoryFile at path. max bounds the number
// of entries returned by LoadAll (most recent retained); 0 disables the
// bound.
func NewHistoryFile(path string, max int) *HistoryFile {
	return &HistoryFile{path: path, max: max}
}

// Path returns the underlying file path.
func (h *HistoryFile) Path() string { return h.path }

// LoadAll reads the file and returns triggered alerts in chronological
// order, trimmed to the configured max. Missing file returns (nil, nil).
// Malformed lines are skipped silently.
func (h *HistoryFile) LoadAll() ([]TriggeredAlert, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("alert history: open %s: %w", h.path, err)
	}
	defer f.Close()
	var out []TriggeredAlert
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var a TriggeredAlert
		if err := json.Unmarshal(sc.Bytes(), &a); err != nil {
			continue
		}
		out = append(out, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("alert history: scan %s: %w", h.path, err)
	}
	if h.max > 0 && len(out) > h.max {
		out = out[len(out)-h.max:]
	}
	return out, nil
}

// Append writes a triggered alert as one JSON line. Creates the file if
// missing (mode 0o600 — alert history may include symbol metadata).
func (h *HistoryFile) Append(a TriggeredAlert) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("alert history: open %s: %w", h.path, err)
	}
	defer f.Close()
	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("alert history: marshal: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("alert history: write %s: %w", h.path, err)
	}
	return nil
}

// HistoryNotifier persists triggered alerts via a HistoryFile.
type HistoryNotifier struct {
	file *HistoryFile
}

// NewHistoryNotifier constructs a Notifier that appends to the given file.
func NewHistoryNotifier(file *HistoryFile) *HistoryNotifier {
	return &HistoryNotifier{file: file}
}

// Name implements Notifier.
func (h *HistoryNotifier) Name() string { return "history" }

// Notify implements Notifier.
func (h *HistoryNotifier) Notify(_ context.Context, a TriggeredAlert) error {
	return h.file.Append(a)
}

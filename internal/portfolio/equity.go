package portfolio

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// EquityMark is a single recorded portfolio value at a point in time.
type EquityMark struct {
	Time          time.Time `json:"time"`
	PortfolioName string    `json:"portfolio"`
	Value         float64   `json:"value"`
}

// EquityFile reads and appends portfolio equity marks as NDJSON. It is
// concurrency-safe; multiple goroutines may call Append.
type EquityFile struct {
	mu   sync.Mutex
	path string
	max  int // when LoadAll returns more than max, the most recent are kept; 0 = unlimited
}

// NewEquityFile constructs an EquityFile at path. max bounds the number
// of entries returned by LoadAll (most recent retained); 0 disables it.
func NewEquityFile(path string, max int) *EquityFile {
	return &EquityFile{path: path, max: max}
}

// Path returns the underlying file path.
func (e *EquityFile) Path() string { return e.path }

// LoadAll reads the file and returns marks in chronological order,
// trimmed to the configured max. Missing file returns (nil, nil).
// Malformed lines are skipped.
func (e *EquityFile) LoadAll() ([]EquityMark, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f, err := os.Open(e.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("equity: open %s: %w", e.path, err)
	}
	defer f.Close()
	var out []EquityMark
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var m EquityMark
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("equity: scan %s: %w", e.path, err)
	}
	if e.max > 0 && len(out) > e.max {
		out = out[len(out)-e.max:]
	}
	return out, nil
}

// LoadByName returns LoadAll bucketed by PortfolioName.
func (e *EquityFile) LoadByName() (map[string][]EquityMark, error) {
	all, err := e.LoadAll()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]EquityMark, 8)
	for _, m := range all {
		out[m.PortfolioName] = append(out[m.PortfolioName], m)
	}
	return out, nil
}

// Append writes a single mark as one JSON line.
func (e *EquityFile) Append(m EquityMark) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	f, err := os.OpenFile(e.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("equity: open %s: %w", e.path, err)
	}
	defer f.Close()
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("equity: marshal: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("equity: write %s: %w", e.path, err)
	}
	return nil
}

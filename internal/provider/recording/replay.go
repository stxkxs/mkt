package recording

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// Mode controls how Replay paces quote emission.
type Mode int

const (
	// ModeBurst emits all quotes as fast as the consumer can read them.
	ModeBurst Mode = iota
	// ModeRealtime sleeps between quotes to match the recorded inter-quote
	// intervals.
	ModeRealtime
)

// Replay implements provider.QuoteProvider by reading an NDJSON file
// previously written by Recording.
type Replay struct {
	path string
	mode Mode
}

// NewReplay constructs a Replay reading from path. The file is opened
// when Subscribe is called.
func NewReplay(path string, mode Mode) *Replay {
	return &Replay{path: path, mode: mode}
}

// Name implements provider.QuoteProvider.
func (r *Replay) Name() string { return "replay(" + r.path + ")" }

// Supports always returns true; the symbol filter passed to Subscribe
// is the actual gate. This lets Replay stand in for any other provider
// when used alone with a hub.
func (r *Replay) Supports(_ string) bool { return true }

// Subscribe reads the recording file and emits quotes to out. Malformed
// lines are logged and skipped. With ModeRealtime, emission is paced by
// the recorded inter-quote intervals.
func (r *Replay) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	f, err := os.Open(r.path)
	if err != nil {
		return fmt.Errorf("replay: open %s: %w", r.path, err)
	}
	defer f.Close()

	filter := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		filter[s] = struct{}{}
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lastTS time.Time
	var lineNo int

	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		q, err := decode(line)
		if err != nil {
			log.Printf("replay %s:%d: %v", r.path, lineNo, err)
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[q.Symbol]; !ok {
				continue
			}
		}
		if r.mode == ModeRealtime && !lastTS.IsZero() {
			delta := q.Timestamp.Sub(lastTS)
			if delta > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delta):
				}
			}
		}
		lastTS = q.Timestamp
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- q:
		}
	}
	return sc.Err()
}

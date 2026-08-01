package chart

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider"
)

// genCandles builds a deterministic candle series with enough shape to
// exercise every indicator: trend reversals, varying ranges and volume.
func genCandles(n int) []provider.OHLCV {
	out := make([]provider.OHLCV, n)
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	price := 100.0
	for i := range out {
		price += math.Sin(float64(i)*0.7)*1.5 + math.Cos(float64(i)*0.23)*0.8
		open := price
		closeP := price + math.Sin(float64(i)*1.3)
		high := math.Max(open, closeP) + 0.5
		low := math.Min(open, closeP) - 0.5
		out[i] = provider.OHLCV{
			Time:   start.Add(time.Duration(i) * 24 * time.Hour),
			Open:   open,
			High:   high,
			Low:    low,
			Close:  closeP,
			Volume: 1000 + float64((i*37)%500),
		}
	}
	return out
}

// fakeProvider is a HistoryProvider whose answers are scripted per
// interval and whose calls are observable.
type fakeProvider struct {
	mu    sync.Mutex
	calls []provider.HistoryParams
	ctxs  []context.Context

	// respond returns the series for a request. nil means "one candle
	// per interval index", which lets a test tell responses apart.
	respond func(p provider.HistoryParams) ([]provider.OHLCV, error)

	// gate, when non-nil, blocks each call until it is closed or the
	// request context is cancelled.
	gate chan struct{}
}

func (f *fakeProvider) History(ctx context.Context, p provider.HistoryParams) ([]provider.OHLCV, error) {
	f.mu.Lock()
	f.calls = append(f.calls, p)
	f.ctxs = append(f.ctxs, ctx)
	gate := f.gate
	respond := f.respond
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if respond != nil {
		return respond(p)
	}
	return genCandles(60), nil
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeProvider) intervals() []provider.Interval {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]provider.Interval, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Interval
	}
	return out
}

// key builds a KeyPressMsg for a single-rune key.
func key(k string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips SGR sequences so tests can assert on rendered text.
func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// readoutValue pulls a labelled indicator value out of a rendered chart,
// e.g. readoutValue(view, "EMA") for "EMA:123.45".
func readoutValue(t *testing.T, view, label string) (float64, bool) {
	t.Helper()
	re := regexp.MustCompile(label + `:(-?[0-9]+\.[0-9]+)`)
	mm := re.FindStringSubmatch(plain(view))
	if mm == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(mm[1], 64)
	if err != nil {
		t.Fatalf("parse %s value %q: %v", label, mm[1], err)
	}
	return v, true
}

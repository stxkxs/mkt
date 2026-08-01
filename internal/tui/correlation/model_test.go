package correlation

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

// push writes a price series into the cache on a fixed cadence starting
// at base, which is what the alignment helpers key off.
func push(c *market.Cache, sym string, base time.Time, step time.Duration, prices []float64) {
	for i, p := range prices {
		c.Push(provider.Quote{Symbol: sym, Price: p, Timestamp: base.Add(time.Duration(i) * step)})
	}
}

// Correlating raw price LEVELS makes any two symbols that merely drifted
// the same way read as near-perfectly correlated. These two series both
// rise from 100 to ~160 but their period-to-period moves are opposite,
// so on returns they must be strongly negative.
func TestCorrelatesReturnsNotLevels(t *testing.T) {
	cache := market.NewCache(120)
	base := time.Now().Add(-time.Hour).Truncate(time.Minute)

	var up, zig []float64
	a, b := 100.0, 100.0
	for i := range 40 {
		wiggle := 0.02
		if i%2 == 1 {
			wiggle = -0.01
		}
		a *= 1 + wiggle
		b *= 1 - wiggle + 0.011
		up = append(up, a)
		zig = append(zig, b)
	}
	push(cache, "AAA", base, time.Minute, up)
	push(cache, "BBB", base, time.Minute, zig)

	m := New([]string{"AAA", "BBB"}, cache)
	m.SetSize(120, 40)
	m.bucketI = 1 // one-minute buckets, matching the sample cadence

	syms, _ := m.window()
	matrix := m.matrixFor(syms)
	got := matrix[0][1]
	if math.IsNaN(got) {
		t.Fatal("correlation is NaN for two fully sampled series")
	}
	if got > -0.5 {
		t.Errorf("correlation = %.3f; levels-based correlation would be near +1, "+
			"returns-based must be strongly negative", got)
	}
}

// Two symbols sampled at different rates must be compared over the same
// spans of time, not slot-for-slot in the ring buffer.
func TestDifferentSampleRatesAlign(t *testing.T) {
	cache := market.NewCache(240)
	base := time.Now().Add(-2 * time.Hour).Truncate(time.Minute)

	var fast, slow []float64
	for i := range 200 {
		fast = append(fast, 100+10*math.Sin(float64(i)/20))
	}
	for i := range 50 {
		slow = append(slow, 50+5*math.Sin(float64(i*4)/20))
	}
	push(cache, "FAST", base, 5*time.Second, fast)
	push(cache, "SLOW", base, 20*time.Second, slow)

	m := New([]string{"FAST", "SLOW"}, cache)
	m.SetSize(120, 40)
	syms, _ := m.window()
	got := m.matrixFor(syms)[0][1]
	if math.IsNaN(got) {
		t.Fatal("aligned correlation is NaN")
	}
	if got < 0.5 {
		t.Errorf("correlation = %.3f, want strongly positive for the same wave "+
			"sampled at 5s and 20s", got)
	}
}

func symbols(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("SYM%02d", i)
	}
	return out
}

// The tab used to silently drop everything past (width-8)/7 symbols with
// no indication and no way to see the rest.
func TestTruncationIsVisibleAndScrollable(t *testing.T) {
	m := New(symbols(30), market.NewCache(60))
	m.SetSize(80, 20)

	syms, start := m.window()
	if len(syms) >= 30 {
		t.Fatalf("expected the window to clip 30 symbols, got %d", len(syms))
	}
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
	out := plain(m.View())
	if !strings.Contains(out, fmt.Sprintf("showing 1-%d of 30 symbols", len(syms))) {
		t.Errorf("truncation not surfaced:\n%s", out)
	}

	m = press(m, "G")
	syms, start = m.window()
	if start+len(syms) != 30 {
		t.Errorf("G did not scroll to the end: start=%d len=%d", start, len(syms))
	}
	if !strings.Contains(plain(m.View()), "SYM29") {
		t.Error("last symbol unreachable after scrolling to the end")
	}

	m = press(m, "g")
	if _, start = m.window(); start != 0 {
		t.Errorf("g did not rewind: start=%d", start)
	}
}

func TestScrollClamps(t *testing.T) {
	m := New(symbols(30), market.NewCache(60))
	m.SetSize(80, 20)
	for range 200 {
		m = press(m, "l")
	}
	_, start := m.window()
	if start+m.visible() != 30 {
		t.Errorf("scrolled past the end: start=%d visible=%d", start, m.visible())
	}
	for range 200 {
		m = press(m, "h")
	}
	if m.offset != 0 {
		t.Errorf("offset = %d, want 0", m.offset)
	}
}

func TestBucketCycles(t *testing.T) {
	m := New(symbols(4), market.NewCache(60))
	m.SetSize(80, 20)
	first := m.Bucket()
	seen := map[time.Duration]bool{first: true}
	for range len(buckets) - 1 {
		m = press(m, "b")
		seen[m.Bucket()] = true
	}
	if len(seen) != len(buckets) {
		t.Errorf("cycled through %d buckets, want %d", len(seen), len(buckets))
	}
	m = press(m, "b")
	if m.Bucket() != first {
		t.Errorf("bucket did not wrap: %v != %v", m.Bucket(), first)
	}
}

func TestNoTruncationHintWhenEverythingFits(t *testing.T) {
	m := New(symbols(4), market.NewCache(60))
	m.SetSize(120, 30)
	if strings.Contains(plain(m.View()), "showing") {
		t.Error("truncation hint shown when all symbols fit")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	cache := market.NewCache(60)
	base := time.Now().Add(-time.Hour)
	for _, s := range symbols(30) {
		push(cache, s, base, 30*time.Second, []float64{1, 2, 3, 4, 5, 6})
	}
	keys := []string{"h", "l", "[", "]", "g", "G", "b", "pgup", "pgdown", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := New(symbols(30), cache)
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
			empty := New(nil, cache)
			empty.SetSize(w, h)
			empty = press(empty, "l", "G", "b")
			_ = empty.View()
		}
	}
}

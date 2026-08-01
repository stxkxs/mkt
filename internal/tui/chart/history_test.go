package chart

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider"
)

// intervalMarker encodes an interval into a series so a test can tell
// which request a response answers.
func intervalMarker(iv provider.Interval) []provider.OHLCV {
	n := 30
	for i, cand := range intervals {
		if cand == iv {
			n = 30 + i
		}
	}
	return genCandles(n)
}

func markerInterval(data []provider.OHLCV) provider.Interval {
	idx := len(data) - 30
	if idx < 0 || idx >= len(intervals) {
		return ""
	}
	return intervals[idx]
}

func newFetchModel(fp *fakeProvider) Model {
	m := New(fp)
	m.SetSize(120, 40)
	return m
}

func TestStaleHistoryResponseIsIgnored(t *testing.T) {
	// Press ] twice quickly. Whichever response lands last used to win,
	// so a slow 1h answer could overwrite a 4h chart and sit under a
	// "4h" label.
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		return intervalMarker(p.Interval), nil
	}}
	m := newFetchModel(fp)

	cmd1 := m.SetSymbol("AAPL") // 1d
	var cmd2, cmd3 tea.Cmd
	m, cmd2 = m.Update(key("]")) // 1w
	m, cmd3 = m.Update(key("[")) // back to 1d, a third distinct request

	if cmd1 == nil || cmd2 == nil || cmd3 == nil {
		t.Fatal("interval changes did not issue requests")
	}

	// Deliver them out of order: newest first, then the two stale ones.
	for _, cmd := range []tea.Cmd{cmd3, cmd2, cmd1} {
		m, _ = m.Update(cmd())
	}

	if m.dataInterval != provider.Interval1d {
		t.Fatalf("chart shows %s data after the newest request was for 1d", m.dataInterval)
	}
	if got := markerInterval(m.data); got != provider.Interval1d {
		t.Fatalf("chart holds %s bars while labelled %s", got, m.dataInterval)
	}
	if !strings.Contains(plain(m.View()), "1d") {
		t.Fatalf("header does not say 1d:\n%s", plain(m.View()))
	}
}

func TestStaleHistoryErrorIsIgnored(t *testing.T) {
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		if p.Interval == provider.Interval1d {
			return nil, errors.New("boom")
		}
		return intervalMarker(p.Interval), nil
	}}
	m := newFetchModel(fp)

	cmd1 := m.SetSymbol("AAPL") // 1d, will fail
	var cmd2 tea.Cmd
	m, cmd2 = m.Update(key("]")) // 1w, will succeed

	m, _ = m.Update(cmd2())
	m, _ = m.Update(cmd1()) // stale failure must not blank the chart

	if m.errMsg != "" {
		t.Fatalf("stale error surfaced: %q", m.errMsg)
	}
	if len(m.data) == 0 {
		t.Fatal("stale error cleared the loaded data")
	}
}

func TestResponseForAnotherSymbolIsIgnored(t *testing.T) {
	fp := &fakeProvider{}
	m := newFetchModel(fp)
	cmd := m.SetSymbol("AAPL")
	msg := cmd().(historyLoadedMsg)

	m.symbol = "MSFT"
	m, _ = m.Update(msg)
	if len(m.data) != 0 {
		t.Fatal("accepted a response for a symbol the chart has moved off")
	}
}

func TestIntervalSwitchCancelsTheRequestInFlight(t *testing.T) {
	fp := &fakeProvider{gate: make(chan struct{})}
	m := newFetchModel(fp)

	cmd := m.SetSymbol("AAPL")
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	// Wait for the provider to have entered the call.
	deadline := time.After(2 * time.Second)
	for fp.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("provider never called")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// A newer request supersedes it, and the old context is cancelled.
	m, _ = m.Update(key("]"))

	select {
	case msg := <-done:
		if _, ok := msg.(historyErrorMsg); !ok {
			t.Fatalf("cancelled request returned %T, want historyErrorMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superseded request was not cancelled")
	}
	_ = m
}

func TestHistoryIsCachedAcrossIntervalSwitches(t *testing.T) {
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		return intervalMarker(p.Interval), nil
	}}
	m := newFetchModel(fp)

	run := func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("no command issued")
		}
		m, _ = m.Update(cmd())
	}

	run(m.SetSymbol("AAPL")) // 1d
	var cmd tea.Cmd
	m, cmd = m.Update(key("]")) // 1w
	run(cmd)
	m, cmd = m.Update(key("[")) // 1d again — must come from the cache
	run(cmd)

	if got := fp.callCount(); got != 2 {
		t.Fatalf("provider called %d times for 2 distinct intervals: %v", got, fp.intervals())
	}
	if m.dataInterval != provider.Interval1d {
		t.Fatalf("dataInterval = %s, want 1d", m.dataInterval)
	}
	if m.loading {
		t.Fatal("a cache hit left the chart in the loading state")
	}
}

func TestHistoryCacheExpiresAndEvicts(t *testing.T) {
	now := time.Unix(0, 0)
	c := newHistoryCache()
	c.now = func() time.Time { return now }
	c.max = 3

	data := genCandles(5)
	c.put("A", provider.Interval1d, data)
	if _, ok := c.get("A", provider.Interval1d); !ok {
		t.Fatal("fresh entry missing")
	}

	now = now.Add(historyCacheTTL)
	if _, ok := c.get("A", provider.Interval1d); ok {
		t.Fatal("expired entry served")
	}
	if c.size() != 0 {
		t.Fatalf("expired entry not dropped, size = %d", c.size())
	}

	for _, sym := range []string{"A", "B", "C", "D"} {
		c.put(sym, provider.Interval1d, data)
	}
	if c.size() != 3 {
		t.Fatalf("cache size = %d, want 3", c.size())
	}
	if _, ok := c.get("A", provider.Interval1d); ok {
		t.Fatal("oldest entry was not evicted")
	}
	for _, sym := range []string{"B", "C", "D"} {
		if _, ok := c.get(sym, provider.Interval1d); !ok {
			t.Fatalf("%s evicted too early", sym)
		}
	}
}

func TestHistoryCacheIgnoresEmptyAndNilReceiver(t *testing.T) {
	var nilCache *historyCache
	nilCache.put("A", provider.Interval1d, genCandles(3))
	if _, ok := nilCache.get("A", provider.Interval1d); ok {
		t.Fatal("nil cache returned a hit")
	}
	if nilCache.size() != 0 {
		t.Fatal("nil cache reports entries")
	}

	c := newHistoryCache()
	c.put("A", provider.Interval1d, nil)
	if _, ok := c.get("A", provider.Interval1d); ok {
		t.Fatal("an empty answer was cached; it must be retried")
	}
}

// servedProvider reports that it serves 4h requests as 1h bars, the way
// Yahoo does.
type servedProvider struct{ fakeProvider }

func (s *servedProvider) ServedInterval(_ string, requested provider.Interval) provider.Interval {
	if requested == provider.Interval4h {
		return provider.Interval1h
	}
	return requested
}

func TestHeaderLabelsTheIntervalTheProviderActuallyServes(t *testing.T) {
	sp := &servedProvider{}
	m := New(sp)
	m.SetSize(120, 40)
	m.intervalIdx = 4 // 4h
	cmd := m.SetSymbol("AAPL")
	m, _ = m.Update(cmd())

	view := plain(m.View())
	if !strings.Contains(view, "4h (1h bars)") {
		t.Fatalf("header hides the interval substitution:\n%s", view)
	}
}

func TestHeaderKeepsRequestedIntervalWhenServedMatches(t *testing.T) {
	sp := &servedProvider{}
	m := New(sp)
	m.SetSize(120, 40)
	cmd := m.SetSymbol("AAPL") // 1d, served natively
	m, _ = m.Update(cmd())

	view := plain(m.View())
	if strings.Contains(view, "bars)") {
		t.Fatalf("header annotated an interval that was served as asked:\n%s", view)
	}
}

func TestNoProviderIssuesNoRequest(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 40)
	if cmd := m.SetSymbol("AAPL"); cmd != nil {
		t.Fatal("a nil provider still issued a fetch")
	}
}

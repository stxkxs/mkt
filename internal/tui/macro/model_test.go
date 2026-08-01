package macro

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	"github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/defillama"
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

func loaded() Model {
	m := New()
	m.SetSize(100, 24)
	m.UpdateQuotes([]provider.Quote{
		{Symbol: "^TNX", Price: 4.7450, ChangePct: 0.4},
		{Symbol: "^IRX", Price: 3.6820, ChangePct: -0.2},
		{Symbol: "^VIX", Price: 15.2, ChangePct: -3.1},
		{Symbol: "DX-Y.NYB", Price: 104.2, ChangePct: 0.1},
		{Symbol: "GC=F", Price: 2400, ChangePct: 0.8},
		{Symbol: "CL=F", Price: 78.1, ChangePct: -1.4},
		{Symbol: "^GSPC", Price: 5500, ChangePct: 0.6},
		{Symbol: "BTC-USD", Price: 62000, ChangePct: 2.2},
	})
	m.UpdateFutures([]binance.FuturesSnapshot{
		{Symbol: "BTCUSDT", MarkPrice: 62010, FundingRate: 0.0001, OpenInterest: 1.2e9},
	})
	m.UpdateDeFi([]defillama.TVLSnapshot{
		{Chain: "Ethereum", TVL: 5e10, Change1d: 1.2, Change7d: -0.4},
	})
	m.UpdateEvents([]calendar.Event{
		{Title: "FOMC Rate Decision", Time: time.Now().Add(72 * time.Hour)},
	})
	return m
}

// ^IRX is the 13-week bill, so ^TNX - ^IRX is the 10Y-3M spread, not the
// 2s10s. The label used to claim the wrong indicator.
func TestSpreadIsLabelled10Y3M(t *testing.T) {
	out := plain(loaded().View())
	if !strings.Contains(out, "10Y-3M Spread") {
		t.Errorf("spread not labelled 10Y-3M:\n%s", out)
	}
	if strings.Contains(out, "2s10s") {
		t.Errorf("stale 2s10s label still present:\n%s", out)
	}
	if !strings.Contains(out, "1.063%") {
		t.Errorf("spread value wrong (want 4.7450 - 3.6820 = 1.063):\n%s", out)
	}
}

func TestSpreadOmittedWithoutBothLegs(t *testing.T) {
	m := New()
	m.SetSize(100, 60)
	m.UpdateQuotes([]provider.Quote{{Symbol: "^TNX", Price: 4.5}})
	if strings.Contains(plain(m.View()), "Spread") {
		t.Error("spread rendered with only one leg quoted")
	}
}

// Without an Update the tab was entirely non-interactive, so anything
// past the fold was unreachable.
func TestScrollReachesTheBottom(t *testing.T) {
	m := loaded()
	m.SetSize(100, 12)

	total := len(m.contentLines())
	if total <= m.visibleRows() {
		t.Fatalf("test data (%d rows) fits in the frame; nothing to scroll", total)
	}
	last := m.contentLines()[total-1]

	if strings.Contains(m.View(), last) {
		t.Fatal("last row already visible before scrolling")
	}
	m = press(m, "G")
	if !strings.Contains(m.View(), last) {
		t.Error("G did not scroll to the last row")
	}
	m = press(m, "g")
	if strings.Contains(m.View(), last) {
		t.Error("g did not scroll back to the top")
	}
}

func TestScrollClampsBothEnds(t *testing.T) {
	m := loaded()
	m.SetSize(100, 12)
	for range 500 {
		m = press(m, "j")
	}
	if m.scroll != m.clampScroll(1<<20) {
		t.Errorf("scroll = %d, want the clamped maximum %d", m.scroll, m.clampScroll(1<<20))
	}
	for range 500 {
		m = press(m, "k")
	}
	if m.scroll != 0 {
		t.Errorf("scroll = %d, want 0", m.scroll)
	}
}

func TestViewNeverExceedsFrame(t *testing.T) {
	m := loaded()
	for _, h := range sweepHeights {
		m.SetSize(100, h)
		if lines := strings.Count(m.View(), "\n") + 1; lines > h && h > 3 {
			t.Errorf("height %d rendered %d lines", h, lines)
		}
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	keys := []string{"j", "k", "g", "G", "pgup", "pgdown", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := loaded()
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
			empty := New()
			empty.SetSize(w, h)
			empty = press(empty, "j", "G")
			_ = empty.View()
		}
	}
}

package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

type fakeHistory struct{}

func (fakeHistory) History(context.Context, provider.HistoryParams) ([]provider.OHLCV, error) {
	return nil, nil
}

func newTestApp() *App {
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL", "BTC-USD"}}}
	cache := market.NewCache(30)
	engine := alert.NewEngine(0, nil)
	return NewApp(groups, cache, fakeHistory{}, nil, engine, yahoo.New(0), coinbase.New())
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// TestAppTabSwitchAndRender drives the root Update router across every tab
// and renders each — smoke coverage for the message router and every tab's
// View, which previously had none.
func TestAppTabSwitchAndRender(t *testing.T) {
	a := newTestApp()
	m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = m.(*App)
	if !a.ready {
		t.Fatal("app not ready after WindowSizeMsg")
	}

	wantTabs := []struct {
		key string
		tab Tab
	}{
		{"1", TabWatchlist},
		{"2", TabPortfolio},
		{"3", TabAlerts},
		{"4", TabChart},
		{"5", TabMacro},
		{"6", TabNews},
		{"7", TabHeatmap},
		{"8", TabOptions},
		{"9", TabCorrel},
	}
	for _, wt := range wantTabs {
		m, _ = a.Update(keyPress(wt.key))
		a = m.(*App)
		if a.activeTab != wt.tab {
			t.Errorf("key %q: activeTab = %v, want %v", wt.key, a.activeTab, wt.tab)
		}
		// Rendering must not panic for any tab.
		if got := a.View(); got.AltScreen != true {
			t.Errorf("key %q: expected AltScreen view", wt.key)
		}
	}
}

// TestAppQuotePropagation verifies a QuoteUpdateMsg reaches the watchlist.
func TestAppQuotePropagation(t *testing.T) {
	a := newTestApp()
	m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = m.(*App)

	m, _ = a.Update(QuoteUpdateMsg{Quote: provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: 1.2}})
	a = m.(*App)

	if got := a.watchlist.CurrentPrice("AAPL"); got != 201.5 {
		t.Errorf("watchlist price for AAPL = %v, want 201.5", got)
	}
}

// TestAppTabCycle verifies tab/shift+tab wraparound via the arrow aliases.
func TestAppTabCycle(t *testing.T) {
	a := newTestApp()
	m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	a = m.(*App)
	if a.activeTab != TabWatchlist {
		t.Fatalf("initial tab = %v", a.activeTab)
	}
	// "right" advances to the next tab.
	m, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyRight, Text: "right"})
	a = m.(*App)
	if a.activeTab != TabPortfolio {
		t.Errorf("after right: activeTab = %v, want TabPortfolio", a.activeTab)
	}
}

package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/news"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	"github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/defillama"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// unknownMsg stands in for the async result types the sub-models keep
// private: it exercises the router's default branch, which is where the
// Options tab and the detail panel used to lose their loads.
type unknownMsg struct{}

// dataBattery is one of every message the data plane sends, so a sweep
// covers the fan-out paths as well as the render paths.
func dataBattery() []tea.Msg {
	now := time.Now()
	return []tea.Msg{
		SpinnerTickMsg{},
		QuoteUpdateMsg{Quote: provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: 1.2, Timestamp: now}},
		QuoteUpdateMsg{Quote: provider.Quote{Symbol: "BTC-USD", Price: 60000, ChangePct: -3.4, Timestamp: now}},
		MacroUpdateMsg{Quotes: []provider.Quote{{Symbol: "^GSPC", Price: 5000, ChangePct: 0.4}}},
		NewsUpdateMsg{Headlines: []news.Headline{{Title: "something happened", Source: "wire", PubTime: now}}},
		DeFiUpdateMsg{Chains: []defillama.TVLSnapshot{{Chain: "Ethereum", TVL: 1e9}}},
		FuturesUpdateMsg{Snapshots: []binance.FuturesSnapshot{{Symbol: "BTCUSDT", MarkPrice: 60000}}},
		CalendarUpdateMsg{Events: []calendar.Event{{Title: "CPI", Time: now, Importance: 3}}},
		EquityMarkMsg{Mark: portfolio.EquityMark{PortfolioName: "Default", Value: 1000, Time: now}},
		AlertTriggeredMsg{Alert: alert.TriggeredAlert{Rule: alert.Rule{Symbol: "AAPL"}, Price: 201.5, Message: "AAPL above 200", Timestamp: now}},
		ConnectionStatusMsg{Provider: "coinbase", Connected: true},
		ConnectionStatusMsg{Provider: "yahoo", Connected: false},
		UnroutableSymbolsMsg{Symbols: []string{"NOTREAL"}},
		ConfigStatusMsg{Status: ConfigStatus{Degraded: true, Path: "config.yaml", Line: 9, WritesDisabled: true}},
		unknownMsg{},
		theme.ChangedMsg{Name: theme.CurrentName},
	}
}

// sweepKeys covers every documented binding plus the ones that open a
// modal or a full-screen overlay, so the sweep visits those layouts too.
var sweepKeys = []string{
	"j", "k", "g", "G", "h", "l", "[", "]", "s", "t", "d", "y", "n",
	"b", "f", "m", "+", "-", "a", "c", "C", "A", "i", "O", "?", "T",
}

// TestSizeSweepNoPanic renders every tab at every awkward frame size,
// with the full message battery applied and the degraded-config banner
// up (it changes the layout). The tab packages sweep their own views;
// this is the app-level equivalent, covering the chrome — tab bar,
// notice rows, panel borders, status bar — and the mouse translation
// that is derived from them.
func TestSizeSweepNoPanic(t *testing.T) {
	widths := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 29, 30, 31, 120}
	heights := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 15, 40}
	battery := dataBattery()

	for _, w := range widths {
		for _, h := range heights {
			for tab := range tabNames {
				a := newTestApp()
				a, _ = send(t, a, tea.WindowSizeMsg{Width: w, Height: h})
				a.activeTab = Tab(tab)
				for _, msg := range battery {
					a, _ = send(t, a, msg)
					a.activeTab = Tab(tab)
					_ = a.View()
				}
				// Mouse events at and past every edge of the frame.
				for _, y := range []int{0, 1, 2, 3, h - 1, h, h + 1} {
					for _, x := range []int{0, 1, w - 1, w, w + 1} {
						a, _ = send(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
						a, _ = send(t, a, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
						a, _ = send(t, a, tea.MouseMotionMsg{X: x, Y: y})
					}
				}
				_ = a.View()
			}
		}
	}
}

// TestKeySweepNoPanic drives every binding on every tab at a few
// representative frame sizes, including the sizes where the panel border
// appears and disappears.
func TestKeySweepNoPanic(t *testing.T) {
	sizes := []struct{ w, h int }{{0, 0}, {8, 4}, {30, 15}, {29, 14}, {120, 40}}
	battery := dataBattery()

	for _, sz := range sizes {
		for tab := range tabNames {
			a := newTestApp()
			a, _ = send(t, a, tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			for _, msg := range battery {
				a, _ = send(t, a, msg)
			}
			a.activeTab = Tab(tab)
			for _, k := range sweepKeys {
				a, _ = send(t, a, keyPress(k))
				_ = a.View()
				// Some of these keys raise a modal or a full-screen
				// overlay, which changes which layer owns the mouse.
				// Clicking right after each one covers those layers as
				// well as the plain tab layout.
				for _, y := range []int{0, 1, sz.h - 1, sz.h} {
					a, _ = send(t, a, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
					a, _ = send(t, a, tea.MouseWheelMsg{X: 1, Y: y, Button: tea.MouseWheelUp})
					_ = a.View()
				}
			}
			// Special keys that have no single-rune form.
			for _, code := range []rune{tea.KeyEnter, tea.KeyEsc, tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd, tea.KeyTab} {
				a, _ = send(t, a, keyCode(code))
				_ = a.View()
			}
		}
	}
	// The sweep presses 'T', which mutates the process-wide theme.
	theme.Apply("tokyonight")
}

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

// Independent end-to-end check: drive the ROOT model, which is what a real
// terminal renders, and assert every emitted line fits the frame. The tab
// packages assert against the width they are handed; this asserts against
// the width the user actually has, so it also covers the panel border,
// the tab bar, the status bar and the notice rows.
func TestEveryFrameLineFitsTheTerminal(t *testing.T) {
	hostile := []string{
		"AAPL", "BTC-USD", "LINK-USD",
		"VERYLONGTICKERNAME", "1️⃣2️⃣3️⃣",
		"A☂️B", "日本語です", "TICḰER",
		"\U0001F468\u200d\U0001F469\u200d\U0001F467", "",
	}
	groups := []watchlist.Group{{Name: "❤️ Hostile", Symbols: hostile}}

	for w := 1; w <= 200; w++ {
		for _, h := range []int{6, 12, 40} {
			a := newTestApp()
			a.SetWatchlistGroups(groups)
			m, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: h})
			a = m.(*App)
			for _, q := range hostile {
				if q == "" {
					continue
				}
				a.Update(QuoteUpdateMsg{Quote: provider.Quote{
					Symbol: q, Price: 123456789.12345, ChangePct: -12.3456, Volume: 9.87e9,
					High24h: 2e9, Low24h: -1, Change: -5e8,
				}})
			}
			for tab := 0; tab < 9; tab++ {
				a.activeTab = Tab(tab)
				lines := strings.Split(a.View().Content, "\n")
				worst, worstIdx := 0, -1
				for i, line := range lines {
					// JoinVertical pads every line out to the widest, so
					// measure the content and not the pad.
					line = strings.TrimRight(line, " ")
					if got := lipgloss.Width(line); got > worst {
						worst, worstIdx = got, i
					}
				}
				if worst > w {
					t.Fatalf("tab=%d w=%d (a.width=%d) h=%d: widest line %d of %d measures %d cells:\n%q",
						tab, w, a.width, h, worstIdx, len(lines), worst, strings.TrimRight(lines[worstIdx], " "))
				}
			}
		}
	}
}

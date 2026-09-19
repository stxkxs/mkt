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
		// A tab measures zero cells and expands to the next stop; a
		// newline splits one data row into two screen rows.
		"TAB\tSYM", "NEW\nLINE", "CR\rSYM",
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

// The overlays are composited at a fixed origin over the frame rather than
// laid out inside it, so one wider than the terminal overflows the right
// edge instead of being clipped. Each is opened and measured at the same
// widths as the tabs.
func TestEveryOverlayFitsTheTerminal(t *testing.T) {
	long := "VERYLONGTICKERNAMETHATKEEPSGOING"
	overlays := []struct {
		name string
		open func(a *App)
	}{
		{"help", func(a *App) { a.help.Open(tabNames[a.activeTab]) }},
		{"palette", func(a *App) { a.palette.Open() }},
		{"alertdialog", func(a *App) { a.alertDialog.Open(long, 187.42) }},
		{"symbolinfo", func(a *App) { a.symbolInfo.Open(long) }},
	}

	for _, ov := range overlays {
		for w := 1; w <= 200; w++ {
			a := newTestApp()
			a.SetWatchlistGroups([]watchlist.Group{{Name: "G", Symbols: []string{long, "AAPL"}}})
			m, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: 24})
			a = m.(*App)
			ov.open(a)

			for i, line := range strings.Split(a.View().Content, "\n") {
				line = strings.TrimRight(line, " ")
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("%s overlay at w=%d: line %d measures %d cells:\n%q",
						ov.name, w, i, got, line)
				}
			}
		}
	}
}

// A newline in free text ends the line, so one data row becomes two screen
// rows. That is the same harm an over-wide row causes — a click is mapped
// from a screen row straight to a data-row index — but it is reached
// without ever exceeding the frame, so a width assertion cannot see it.
// The frame's shape must not depend on what the data contains.
func TestRowCountIsIndependentOfCellContent(t *testing.T) {
	render := func(symbols []string) int {
		a := newTestApp()
		a.SetWatchlistGroups([]watchlist.Group{{Name: "G", Symbols: symbols}})
		m, _ := a.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		a = m.(*App)
		for _, s := range symbols {
			a.Update(QuoteUpdateMsg{Quote: provider.Quote{Symbol: s, Price: 1, ChangePct: 1}})
		}
		return len(strings.Split(a.View().Content, "\n"))
	}

	want := render([]string{"AAA", "BBB", "CCC"})
	for _, hostile := range [][]string{
		{"A\nA", "BBB", "CCC"},
		{"AAA", "B\tB", "CCC"},
		{"AAA", "BBB", "C\rC"},
		{"A\nA", "B\tB", "C\rC"},
	} {
		if got := render(hostile); got != want {
			t.Errorf("symbols %q render %d lines, benign data renders %d", hostile, got, want)
		}
	}
}

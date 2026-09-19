package watchlist

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
)

func newTestModel() Model {
	m := New([]Group{{Name: "Test", Symbols: []string{"AAA", "BBB", "CCC"}}}, market.NewCache(60))
	m.SetSize(100, 40)
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 10, ChangePct: 1.0, Volume: 300})
	m.UpdateQuote(provider.Quote{Symbol: "BBB", Price: 30, ChangePct: 5.0, Volume: 100})
	m.UpdateQuote(provider.Quote{Symbol: "CCC", Price: 20, ChangePct: -2.0, Volume: 200})
	return m
}

func press(m Model, key string) Model {
	m, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return m
}

func displayOrder(m Model) []string {
	var out []string
	for _, i := range m.order() {
		out = append(out, m.symbols[i])
	}
	return out
}

func TestSortCycle(t *testing.T) {
	m := newTestModel()

	want := map[string][]string{
		"config": {"AAA", "BBB", "CCC"},
		"change": {"BBB", "AAA", "CCC"},
		"volume": {"AAA", "CCC", "BBB"},
		"price":  {"BBB", "CCC", "AAA"},
	}

	if got := displayOrder(m); !equal(got, want["config"]) {
		t.Fatalf("initial order = %v, want %v", got, want["config"])
	}
	for _, mode := range []string{"change", "volume", "price", "config"} {
		m = press(m, "s")
		if got := displayOrder(m); !equal(got, want[mode]) {
			t.Errorf("sort %s: order = %v, want %v", mode, got, want[mode])
		}
	}
}

func TestSortKeepsSelection(t *testing.T) {
	m := newTestModel()
	m = press(m, "j") // select BBB (config order)
	if got := m.SelectedSymbol(); got != "BBB" {
		t.Fatalf("selected = %s, want BBB", got)
	}
	m = press(m, "s") // change% desc: BBB moves to position 0
	if got := m.SelectedSymbol(); got != "BBB" {
		t.Errorf("selection lost across re-sort: %s", got)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestUnquotedSymbolsSortLast(t *testing.T) {
	m := New([]Group{{Name: "Test", Symbols: []string{"NOQ", "AAA"}}}, market.NewCache(60))
	m.SetSize(100, 40)
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 10, ChangePct: -9.0})
	m = press(m, "s") // change% desc; AAA quoted, NOQ not
	if got := displayOrder(m); !equal(got, []string{"AAA", "NOQ"}) {
		t.Errorf("order = %v, want quoted symbols first", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

func bigModel(n int) Model {
	syms := make([]string, n)
	for i := range syms {
		syms[i] = fmt.Sprintf("SYM%03d", i)
	}
	m := New([]Group{{Name: "Big", Symbols: syms}}, market.NewCache(60))
	for i, s := range syms {
		m.UpdateQuote(provider.Quote{Symbol: s, Price: float64(i + 1), ChangePct: float64(i%7) - 3})
	}
	return m
}

// A content area shorter than the header rows made maxRows negative,
// and the "< 1 means unlimited" branch then painted the whole list
// straight through the bottom of the frame.
func TestShortFrameDoesNotPaintWholeList(t *testing.T) {
	m := bigModel(120)
	for _, h := range []int{0, 1, 2, 3} {
		m.SetSize(100, h)
		rows := strings.Count(m.View(), "\n")
		if rows > 6 {
			t.Errorf("height %d rendered %d rows for 120 symbols", h, rows)
		}
	}
}

func TestVisibleRowsNeverExceedsFrame(t *testing.T) {
	m := bigModel(120)
	for _, h := range sweepHeights {
		m.SetSize(100, h)
		if got := m.visibleRows(); got > 120 {
			t.Errorf("height %d: visibleRows = %d", h, got)
		}
		if got := m.visibleRows(); got < 1 {
			t.Errorf("height %d: visibleRows = %d, want at least 1", h, got)
		}
	}
}

func TestCursorStaysVisibleInShortFrame(t *testing.T) {
	m := bigModel(120)
	m.SetSize(100, 12)
	for range 119 {
		m = press(m, "j")
	}
	out := m.View()
	if !strings.Contains(out, "SYM119") {
		t.Error("cursor row SYM119 not rendered")
	}
	if strings.Contains(out, "SYM000") {
		t.Error("top of list still rendered after scrolling to the bottom")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	keys := []string{"j", "k", "g", "G", "s", "[", "]", "/", "a", "enter", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := bigModel(30)
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseClickMsg{X: 1, Y: h})
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
		}
	}
}

func TestEmptyGroupSurvivesEverySize(t *testing.T) {
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := New(nil, market.NewCache(60))
			m.SetSize(w, h)
			m = press(m, "j")
			m = press(m, "G")
			_ = m.View()
			m = press(m, "/")
			_ = m.View()
		}
	}
}

// sweepCache holds enough history for every sweep symbol to draw a
// sparkline, so the trend column is exercised at its real glyph width.
func sweepCache(syms []string) *market.Cache {
	c := market.NewCache(60)
	for _, s := range syms {
		for i := range 40 {
			c.Push(provider.Quote{Symbol: s, Price: float64(100 + i%11), Timestamp: time.Now()})
		}
	}
	return c
}

// sweepStates returns the watchlist in each state whose chrome differs:
// the plain table, the sort hint, the group switcher, search with matches
// and without, and an empty group.
func sweepStates(w, h int) []Model {
	syms := []string{"BTC-USD", "AAPL", "FRED:UNRATE", "VERYLONGSYMBOLNAME", "NOQUOTE"}
	base := New([]Group{
		{Name: "Crypto and Equities", Symbols: syms},
		{Name: "Second", Symbols: []string{"AAA"}},
	}, sweepCache(syms))
	base.SetSize(w, h)
	base.UpdateQuote(provider.Quote{Symbol: "BTC-USD", Price: 123456.78, ChangePct: 12.34, Volume: 9876543210, High24h: 130000, Low24h: 120000})
	base.UpdateQuote(provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: -1.25, Volume: 5000000, High24h: 205, Low24h: 199})
	base.UpdateQuote(provider.Quote{Symbol: "FRED:UNRATE", Price: 4.1, ChangePct: 0, High24h: 4.1, Low24h: 4.1})
	base.UpdateQuote(provider.Quote{Symbol: "VERYLONGSYMBOLNAME", Price: 0.00001234, ChangePct: -123.456, Volume: 1, High24h: 9, Low24h: 1})

	empty := New(nil, market.NewCache(60))
	empty.SetSize(w, h)

	search := press(base, "/")
	long := search
	for _, k := range "abcdefghijklmnopqrstuvwxyz0123456789" {
		long = press(long, string(k))
	}
	return []Model{
		base,
		press(base, "s"),
		press(base, "]"),
		press(base, "G"),
		empty,
		search,
		press(search, "a"),
		press(search, "z"),
		long,
	}
}

// The content panel word-wraps a line it cannot fit: a row that wraps to
// two screen lines pushes the last row out of the panel and past the
// status bar, and desynchronizes the click hit-test, which maps a mouse
// row straight to a data-row index. Every line the watchlist emits —
// rows, header, rule and chrome alike — stays within the frame it was
// given.
func TestEveryLineFitsTheFrame(t *testing.T) {
	for w := 1; w <= 200; w++ {
		for _, h := range []int{3, 20} {
			for state, m := range sweepStates(w, h) {
				for i, line := range strings.Split(m.View(), "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("width %d height %d state %d: line %d measures %d cells: %q",
							w, h, state, i, got, line)
					}
				}
			}
		}
	}
}

// Columns are shed lowest priority first, so a frame too narrow for the
// whole table loses the sparkline before the price and the price last.
func TestNarrowFrameShedsColumns(t *testing.T) {
	cases := []struct {
		width int
		want  []string
		gone  []string
	}{
		{100, []string{"SYMBOL", "PRICE", "CHANGE", "VOL", "RANGE", "TREND"}, nil},
		{53, []string{"SYMBOL", "PRICE", "CHANGE", "VOL", "RANGE", "TREND"}, nil},
		{52, []string{"SYMBOL", "PRICE", "CHANGE", "VOL", "RANGE"}, []string{"TREND"}},
		{43, []string{"SYMBOL", "PRICE", "CHANGE", "VOL"}, []string{"RANGE", "TREND"}},
		{34, []string{"SYMBOL", "PRICE", "CHANGE"}, []string{"VOL", "RANGE", "TREND"}},
		{20, []string{"SYMBOL", "PRICE"}, []string{"CHANGE", "VOL", "RANGE", "TREND"}},
	}
	for _, tc := range cases {
		m := newTestModel()
		m.SetSize(tc.width, 20)
		header := strings.Split(m.View(), "\n")[0]
		for _, label := range tc.want {
			if !strings.Contains(header, label) {
				t.Errorf("width %d: header %q dropped %s", tc.width, header, label)
			}
		}
		for _, label := range tc.gone {
			if strings.Contains(header, label) {
				t.Errorf("width %d: header %q still carries %s", tc.width, header, label)
			}
		}
	}
}

// Below the width that fits the symbol column the tab prints its own
// too-narrow line, clipped to the frame, instead of a partial table.
func TestTooNarrowPrintsNoTable(t *testing.T) {
	m := newTestModel()
	for w := 1; w < 8; w++ {
		m.SetSize(w, 20)
		out := m.View()
		if strings.Contains(out, "SYMBOL") {
			t.Errorf("width %d: rendered a table: %q", w, out)
		}
		if strings.Contains(out, "\n") {
			t.Errorf("width %d: too-narrow notice spans rows: %q", w, out)
		}
		if got := lipgloss.Width(out); got > w {
			t.Errorf("width %d: notice measures %d cells: %q", w, got, out)
		}
	}
	m.SetSize(8, 20)
	if !strings.Contains(m.View(), "SYMBOL") {
		t.Errorf("width 8 renders no table: %q", m.View())
	}
}

// Free text reaches the table from three directions: a symbol carried in
// from the config watchlist, the group name on the hint line, and the
// query typed into search. A grapheme cluster spends cells that a rune
// count does not predict — a keycap sequence spends two across three
// runes — so the frame guarantee is held against the clusters that
// disagree with a rune tally rather than against tidy tickers.
func TestEveryLineFitsTheFrameWithUnicodeFreeText(t *testing.T) {
	syms := []string{"1️⃣2️⃣3️⃣4️⃣5️⃣6️⃣", "中文中文中文中文", "👨‍👩‍👧‍👦🇺🇸👍🏽", "❤️✔️⚠️ℹ️", "PLAIN", ""}
	for w := 1; w <= 200; w++ {
		base := New([]Group{
			{Name: "1️⃣ 中文 ❤️ named group", Symbols: syms},
			{Name: "Second", Symbols: []string{"PLAIN"}},
		}, sweepCache(syms))
		base.SetSize(w, 20)
		base.UpdateQuote(provider.Quote{Symbol: syms[0], Price: 123456.78, ChangePct: 12.34, Volume: 9876543210, High24h: 130000, Low24h: 120000})
		base.UpdateQuote(provider.Quote{Symbol: syms[1], Price: 0.00001234, ChangePct: -123.456, Volume: 1, High24h: 9, Low24h: 1})
		base.UpdateQuote(provider.Quote{Symbol: syms[3], Price: 4.1, ChangePct: 0, High24h: 4.1, Low24h: 4.1})

		sorted := base
		sorted.sortMode = sortChange

		search := base
		search.searching = true
		search.searchQuery = "1️⃣2️⃣3️⃣❤️中文"
		search.filtered = search.computeFiltered(search.searchQuery)

		noMatch := base
		noMatch.searching = true
		noMatch.searchQuery = "zzqq"
		noMatch.filtered = noMatch.computeFiltered("zzqq")

		for state, m := range []Model{base, sorted, search, noMatch} {
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("width %d state %d: line %d measures %d cells: %q", w, state, i, got, line)
				}
			}
		}
	}
}

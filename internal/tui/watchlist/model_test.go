package watchlist

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

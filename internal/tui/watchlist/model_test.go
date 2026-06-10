package watchlist

import (
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

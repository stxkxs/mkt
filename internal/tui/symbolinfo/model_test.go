package symbolinfo

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

func TestInactiveRendersNothing(t *testing.T) {
	if New(nil).View() != "" {
		t.Error("inactive overlay rendered content")
	}
}

func TestEscCloses(t *testing.T) {
	m := New(nil)
	m.active = true
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "esc"})
	if m.Active() {
		t.Error("overlay stayed open after esc")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	summary := yahoo.SymbolSummary{
		Symbol: "NVDA", MarketCap: 3.2e12, PE: 55.1, ForwardPE: 40.2,
		EPS: 2.5, DivYield: 0.03, Week52High: 150, Week52Low: 70,
		Sector: "Technology", Industry: "Semiconductors",
	}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := New(nil)
			m.SetSize(w, h)
			m.active = true
			_ = m.View()

			m.loading = true
			_ = m.View()

			m, _ = m.Update(symbolInfoErrorMsg{err: errors.New("boom")})
			_ = m.View()

			m, _ = m.Update(symbolInfoLoadedMsg{summary: summary})
			_ = m.View()
		}
	}
}

func TestFormatLargeNum(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "—"},
		{1.5e12, "$1.50T"},
		{2.25e9, "$2.25B"},
		{7e6, "$7.00M"},
		{1234, "$1234"},
	}
	for _, tt := range tests {
		if got := formatLargeNum(tt.in); got != tt.want {
			t.Errorf("formatLargeNum(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

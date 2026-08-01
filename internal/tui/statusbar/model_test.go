package statusbar

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

var sweepWidths = []int{-4, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}

func populated() Model {
	m := New()
	m.SetProviderStatus("coinbase", true)
	m.SetProviderStatus("yahoo", false)
	m.SetLastUpdate(time.Now().Add(-3 * time.Second))
	m.SetAlertCount(4)
	m.SetThemeName("tokyonight")
	m.SetSearchQuery("btc")
	return m
}

func TestViewSurvivesEveryWidth(t *testing.T) {
	for _, w := range sweepWidths {
		m := populated()
		m.SetWidth(w)
		out := m.View()
		if w <= 0 && out != "" {
			t.Errorf("width %d rendered %q, want empty", w, out)
		}
	}
}

func TestBarFillsItsWidth(t *testing.T) {
	m := populated()
	m.SetWidth(200)
	if got := lipgloss.Width(m.View()); got != 200 {
		t.Errorf("bar is %d cells wide, want 200", got)
	}
}

func TestProviderStatusUpdatesInPlace(t *testing.T) {
	m := New()
	m.SetProviderStatus("coinbase", false)
	m.SetProviderStatus("coinbase", true)
	if len(m.providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(m.providers))
	}
	if !m.providers[0].Connected {
		t.Error("provider status not updated in place")
	}
}

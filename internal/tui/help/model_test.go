package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

func TestInactiveRendersNothing(t *testing.T) {
	m := New()
	if m.View() != "" {
		t.Error("inactive help rendered content")
	}
}

func TestAnyKeyCloses(t *testing.T) {
	m := New()
	m.Open("Watch")
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Active() {
		t.Error("help stayed open after a key press")
	}
}

func TestEveryTabHasACard(t *testing.T) {
	for tab := range tabBindings {
		m := New()
		m.Open(tab)
		if !strings.Contains(m.View(), "Global") {
			t.Errorf("tab %q card is missing the global section", tab)
		}
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	tabs := []string{"Watch", "Portfolio", "Alerts", "Chart", "Macro", "News",
		"Heatmap", "Options", "Correl", "Unknown Tab", ""}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, tab := range tabs {
				m := New()
				m.SetSize(w, h)
				m.Open(tab)
				_ = m.View()
			}
		}
	}
}

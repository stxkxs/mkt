package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

// SetSize feeds the model a frame width; a card that ignores it is
// composited at x=0 and runs off the right edge of a narrow terminal.
func TestHelpCardFitsNarrowTerminal(t *testing.T) {
	for _, w := range []int{30, 40, 58, 100} {
		m := New()
		m.SetSize(w, 24)
		m.Open("Watch")
		for _, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Fatalf("width %d: rendered line is %d cells: %q", w, got, line)
			}
		}
	}
}

var allTabs = []string{"Watch", "Portfolio", "Alerts", "Chart", "Macro", "News",
	"Heatmap", "Options", "Correl", "Compare", "Detail"}

// scrollAll pages through the card from the top and returns every row
// it showed, so a test sees the whole card however short the frame.
func scrollAll(t *testing.T, w, h int, tab string) []string {
	t.Helper()
	m := New()
	m.SetSize(w, h)
	m.Open(tab)
	var seen []string
	for range 200 {
		seen = append(seen, strings.Split(ansi.Strip(m.View()), "\n")...)
		body, rows := m.layout()
		if rows == 0 || m.offset >= len(body)-rows {
			return seen
		}
		m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		if !m.Active() {
			t.Fatalf("%dx%d %s: j closed a card that scrolls", w, h, tab)
		}
	}
	t.Fatalf("%dx%d %s: card never reached its last row", w, h, tab)
	return nil
}

// The card is the one place every binding is spelled out, so nothing on it
// may be cut: every key and description is on its rows, words broken only
// where a word is wider than its column, scrolling reaches the last row,
// and no row carries an ellipsis, at every frame the card renders in.
func TestCardShowsEveryBindingAndNoEllipsis(t *testing.T) {
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, w := range []int{17, 20, 28, 40, 44, 58, 80, 120} {
		for _, h := range []int{8, 12, 16, 24, 40} {
			for _, tab := range allTabs {
				m := New()
				m.SetSize(w, h)
				m.Open(tab)
				body, rows := m.layout()
				if rows < 0 {
					if m.View() != "" {
						t.Fatalf("%dx%d %s: a card too short to fit still drew", w, h, tab)
					}
					continue
				}
				shown := strings.Join(scrollAll(t, w, h, tab), "\n")
				if strings.Contains(shown, "…") {
					t.Fatalf("%dx%d %s: card cuts text with an ellipsis:\n%s", w, h, tab, shown)
				}
				text := squash(ansi.Strip(strings.Join(body, "\n")))
				for _, b := range append(tabBindings[tab], globalBindings...) {
					if !strings.Contains(text, squash(b.key)) || !strings.Contains(text, squash(b.desc)) {
						t.Fatalf("%dx%d %s: %q / %q is not on the card:\n%s",
							w, h, tab, b.key, b.desc, ansi.Strip(strings.Join(body, "\n")))
					}
				}
			}
		}
	}
}

// A card taller than its frame is composited at y=0 and its bottom rows
// fall off the screen, so the card scrolls instead of growing past it.
func TestCardFitsTheFrameHeight(t *testing.T) {
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			if w == 0 || h == 0 {
				continue // unsized: the card takes its preferred size
			}
			for _, tab := range allTabs {
				m := New()
				m.SetSize(w, h)
				m.Open(tab)
				v := m.View()
				if v == "" {
					continue
				}
				if got := lipgloss.Height(v); got > h && h > panelChrome+2 {
					t.Fatalf("%dx%d %s: card is %d rows", w, h, tab, got)
				}
				for _, line := range strings.Split(v, "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("%dx%d %s: line is %d cells: %q", w, h, tab, got, line)
					}
				}
			}
		}
	}
}

func TestScrollKeysMoveAndOtherKeysClose(t *testing.T) {
	m := New()
	m.SetSize(80, 12)
	m.Open("Watch")
	first := m.View()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !m.Active() || m.offset != 1 || m.View() == first {
		t.Fatalf("j did not scroll: active=%v offset=%d", m.Active(), m.offset)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	body, rows := m.layout()
	if m.offset != len(body)-rows {
		t.Fatalf("G: offset %d, want %d", m.offset, len(body)-rows)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.offset != len(body)-rows {
		t.Fatalf("j past the end moved to %d", m.offset)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.offset != 0 {
		t.Fatalf("g: offset %d", m.offset)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.Active() {
		t.Fatal("x did not close a scrolling card")
	}
	m.Open("Watch")
	if m.offset != 0 {
		t.Fatal("reopening kept the old scroll offset")
	}
}

// A card that fits has nothing to scroll, so every key closes it,
// j included, as its footer says.
func TestFittingCardClosesOnJ(t *testing.T) {
	m := New()
	m.SetSize(120, 60)
	m.Open("Portfolio")
	if !strings.Contains(ansi.Strip(m.View()), closeFooter) {
		t.Fatal("a card that fits does not say any key closes it")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.Active() {
		t.Fatal("j did not close a card that fits")
	}
}

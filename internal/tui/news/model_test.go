package news

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	inews "github.com/stxkxs/mkt/internal/news"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

func headlines(n int) []inews.Headline {
	out := make([]inews.Headline, n)
	for i := range out {
		out[i] = inews.Headline{
			Source:  fmt.Sprintf("SRC%02d", i),
			Title:   fmt.Sprintf("Headline number %d about the market", i),
			Link:    "https://example.invalid",
			PubTime: time.Now().Add(-time.Duration(i) * time.Minute),
		}
		if i%3 == 0 {
			out[i].Category = "8-K"
		}
	}
	return out
}

func newTestModel(n int) Model {
	m := New()
	m.SetSize(100, 24)
	m.UpdateHeadlines(headlines(n))
	return m
}

// The hint used to be appended after a divider already padded to the
// full frame width, so it wrapped onto its own row and pushed the feed
// down by one.
func TestSectionHeaderAndHintShareOneRow(t *testing.T) {
	m := newTestModel(10)
	first := strings.SplitN(m.View(), "\n", 2)[0]
	if !strings.Contains(plain(first), "f:filter") {
		t.Errorf("hint not on the header row: %q", plain(first))
	}
	if lipgloss.Width(first) > 100 {
		t.Errorf("header row is %d cells wide in a 100-cell frame", lipgloss.Width(first))
	}
}

func TestFeedIsWindowed(t *testing.T) {
	m := newTestModel(60)
	m.SetSize(100, 20)
	if lines := strings.Count(m.View(), "\n"); lines > 20 {
		t.Errorf("view is %d lines tall in a 20-line frame", lines)
	}
}

func TestCursorStaysVisible(t *testing.T) {
	m := newTestModel(60)
	m.SetSize(100, 20)
	m = press(m, "G")
	out := plain(m.View())
	if !strings.Contains(out, "Headline number 59") {
		t.Error("last headline not rendered after G")
	}
	if strings.Contains(out, "Headline number 0 ") {
		t.Error("first headline still rendered after G")
	}
}

func TestFilterCycles(t *testing.T) {
	m := newTestModel(9)
	want := []Filter{FilterNews, FilterFilings, FilterAll}
	for _, f := range want {
		m = press(m, "f")
		if m.filter != f {
			t.Fatalf("filter = %v, want %v", m.filter, f)
		}
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	keys := []string{"j", "k", "g", "G", "f", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := New()
				m.SetSize(w, h)
				m.UpdateHeadlines(headlines(20))
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseClickMsg{X: 1, Y: h})
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
		}
	}
}

func TestEmptyFeedSurvivesEverySize(t *testing.T) {
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := New()
			m.SetSize(w, h)
			m = press(m, "j", "G", "f")
			_ = m.View()
		}
	}
}

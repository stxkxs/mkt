package theme

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// sweepWidths covers the narrow frames where layout arithmetic goes
// negative, plus a few realistic terminal widths.
var sweepWidths = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}

func TestSectionHeaderNeverExceedsWidth(t *testing.T) {
	for _, w := range sweepWidths {
		got := SectionHeader("Sector Heatmap", w)
		if strings.Contains(got, "\n") {
			t.Errorf("width %d: header spans multiple lines", w)
		}
		if lipgloss.Width(got) > w && w > 6 {
			t.Errorf("width %d: header is %d cells wide", w, lipgloss.Width(got))
		}
	}
}

// The hint used to be appended by the caller after a divider that had
// already padded to full width, so it always wrapped onto the next row.
func TestSectionHeaderHintStaysOnOneRow(t *testing.T) {
	const hint = "j/k/h/l:nav  enter:drill down"
	for _, w := range sweepWidths {
		got := SectionHeaderHint("Sector Heatmap", hint, w)
		if strings.Contains(got, "\n") {
			t.Fatalf("width %d: header spans multiple lines", w)
		}
		if w > 6 && lipgloss.Width(got) > w {
			t.Errorf("width %d: header+hint is %d cells wide, want <= %d",
				w, lipgloss.Width(got), w)
		}
	}
}

func TestSectionHeaderHintShownWhenItFits(t *testing.T) {
	const hint = "j/k:nav"
	wide := SectionHeaderHint("News Feed", hint, 80)
	if !strings.Contains(wide, hint) {
		t.Errorf("hint dropped at width 80: %q", wide)
	}
	narrow := SectionHeaderHint("News Feed", hint, 20)
	if strings.Contains(narrow, hint) {
		t.Errorf("hint kept at width 20 where it does not fit: %q", narrow)
	}
	if !strings.Contains(narrow, "News Feed") {
		t.Errorf("title dropped at width 20: %q", narrow)
	}
}

func TestSectionHeaderEmptyHintMatchesPlain(t *testing.T) {
	for _, w := range sweepWidths {
		if SectionHeader("Alert Rules", w) != SectionHeaderHint("Alert Rules", "", w) {
			t.Errorf("width %d: SectionHeader and empty-hint form disagree", w)
		}
	}
}

func TestRenderPanelSurvivesEveryWidth(t *testing.T) {
	content := "line one\nline two\n"
	for _, w := range sweepWidths {
		out := RenderPanel("Help — Watch", content, w)
		if w < 3 && out != "" {
			t.Errorf("width %d: expected no panel, got %q", w, out)
		}
	}
}

func TestHeatmapColorClamps(t *testing.T) {
	for _, pct := range []float64{-1e9, -100, -5, 0, 5, 100, 1e9} {
		if HeatmapColor(pct) == nil {
			t.Errorf("HeatmapColor(%v) returned nil", pct)
		}
	}
}

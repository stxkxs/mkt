package theme

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// A row of independently styled cells, each closed by its own reset, is
// what every list renders. The band has to survive every one of those
// resets, not only the first: no visible text may follow a reset before
// the band is re-opened.
func TestHighlightRowBandSurvivesEveryCellReset(t *testing.T) {
	Apply("tokyonight")
	open := sgrOpen(StyleCursorRow)
	if open == "" {
		t.Fatal("cursor row style opens no sequence")
	}
	row := "▎ " + StyleSymbol.Render("AAPL") + " " + StyleUp.Render("+1.20%") + " " + StyleDim.Render("vol")
	got := HighlightRow(row, 40)

	if !strings.HasPrefix(got, open) {
		t.Fatalf("band does not open the row: %q", got)
	}
	segs := strings.Split(strings.TrimSuffix(got, "\x1b[m"), "\x1b[m")
	if len(segs) < 4 {
		t.Fatalf("want a segment per styled cell, got %d: %q", len(segs), got)
	}
	for i, s := range segs[1:] {
		if !strings.HasPrefix(s, open) {
			t.Errorf("segment %d after a reset is not re-banded: %q", i+1, s)
		}
	}
	if w := lipgloss.Width(got); w != 40 {
		t.Errorf("band is %d cells wide, want 40", w)
	}
}

func TestHighlightRowNeverShortensAWideRow(t *testing.T) {
	Apply("tokyonight")
	row := strings.Repeat("x", 50)
	if w := lipgloss.Width(HighlightRow(row, 40)); w != 50 {
		t.Errorf("width %d, want the row's own 50", w)
	}
}

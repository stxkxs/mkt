package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// An instruction is wrapped, never cut: every row fits, none carries an
// ellipsis, continuation rows hang under the first, and no word is lost.
func TestNoteWrapsAndHangs(t *testing.T) {
	text := "  Select a symbol on the Watchlist tab and press 'O' to load its options chain."
	for w := 1; w <= 100; w++ {
		rows := NoteLines(text, w)
		for i, r := range rows {
			plain := ansi.Strip(r)
			if ansi.StringWidth(plain) > w || strings.Contains(plain, "…") {
				t.Fatalf("width %d row %d: %q", w, i, plain)
			}
			if w >= 20 && i > 0 && !strings.HasPrefix(plain, "    ") {
				t.Fatalf("width %d row %d does not hang under the first: %q", w, i, plain)
			}
		}
		joined := strings.Join(strings.Fields(ansi.Strip(strings.Join(rows, " "))), "")
		if joined != strings.Join(strings.Fields(text), "") {
			t.Fatalf("width %d lost text: %q", w, rows)
		}
	}
	if got := ansi.Strip(Note("  short", 80)); got != "  short" {
		t.Errorf("Note kept no lead: %q", got)
	}
}

// A narrow header shortens its title before it drops the hint, and drops
// the hint before it cuts the shortest title.
func TestSectionHeaderTitlesShortensBeforeDropping(t *testing.T) {
	titles := []string{"Correlation Matrix (watchlist)", "Correlation Matrix", "Correlation"}
	hint := "log returns @ 1m"
	for w := 1; w <= 120; w++ {
		got := ansi.Strip(SectionHeaderTitles(titles, hint, w))
		if ansi.StringWidth(got) > w {
			t.Fatalf("width %d: %d cells: %q", w, ansi.StringWidth(got), got)
		}
		hasHint := strings.Contains(got, hint)
		if w >= headerLead+headerGap+len("Correlation")+headerMinRule+headerHintGap+len(hint)+1 && !hasHint {
			t.Fatalf("width %d: dropped the hint while a shorter title would fit: %q", w, got)
		}
		if strings.Contains(got, "…") && w > headerLead+headerGap+len("Correlation") {
			t.Fatalf("width %d: cut a title while one fits: %q", w, got)
		}
	}
	if got := ansi.Strip(SectionHeaderTitles(nil, "", 10)); got != strings.Repeat("─", 10) {
		t.Errorf("no titles: %q", got)
	}
}

package heatmap

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider"
)

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
	sweepKeys    = []string{"enter", "j", "k", "h", "l", "esc", "g", "G", "tab", "1", "?"}
)

// ansiRE strips SGR sequences. The treemap styles every grid cell
// individually, so a label's characters are not adjacent in the raw
// output and only the plain text can be searched.
var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

func key(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

// The overview was guarded against a narrow frame; the drilldown was
// not, and strings.Repeat("─", width-4) panicked at any width below 4.
func TestDrilldownNarrowFrameDoesNotPanic(t *testing.T) {
	m := New()
	m.SetSize(3, 20)
	m = press(m, "enter")
	if m.sectorIdx < 0 {
		t.Fatal("enter did not drill in")
	}
	_ = m.View()
}

func TestViewSurvivesEverySize(t *testing.T) {
	base := New()
	base.UpdateQuote(provider.Quote{Symbol: "BTC-USD", Price: 60000, ChangePct: 2.5, Volume: 1e6})
	base.UpdateQuote(provider.Quote{Symbol: "AAPL", Price: 200, ChangePct: -1.25, Volume: 5e6})

	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range sweepKeys {
				m := base
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				// And again after drilling in, which is the path that
				// used to panic.
				m = press(m, "enter", k)
				_ = m.View()
			}
		}
	}
}

func TestMouseSurvivesEverySize(t *testing.T) {
	base := New()
	base.UpdateQuote(provider.Quote{Symbol: "BTC-USD", Price: 60000, ChangePct: 2.5})
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := base
			m.SetSize(w, h)
			for _, y := range []int{0, 1, 3, 5, 40} {
				m, _ = m.Update(tea.MouseClickMsg{X: 0, Y: y})
				m, _ = m.Update(tea.MouseClickMsg{X: w, Y: y})
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
			}
			_ = m.View()
		}
	}
}

func TestSetSectorsReplacesTiles(t *testing.T) {
	m := New()
	m.SetSectors([]Sector{
		{Name: "Mine", Symbols: []string{"AAA", "BBB"}},
		{Name: "Yours", Symbols: []string{"CCC"}},
	})
	if len(m.sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(m.sectors))
	}
	m.SetSize(80, 24)
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 10, ChangePct: 3})
	if !strings.Contains(plain(m.View()), "Mine") {
		t.Error("config-derived sector name missing from view")
	}
}

func TestSetSectorsDropsEmptyGroupsAndFallsBack(t *testing.T) {
	m := New()
	m.SetSectors([]Sector{{Name: "Empty"}, {Name: "Also empty", Symbols: []string{}}})
	if len(m.sectors) != len(DefaultSectors) {
		t.Errorf("all-empty input should fall back to DefaultSectors, got %d sectors", len(m.sectors))
	}

	m.SetSectors([]Sector{{Name: "Empty"}, {Name: "Real", Symbols: []string{"AAA"}}})
	if len(m.sectors) != 1 || m.sectors[0].Name != "Real" {
		t.Errorf("empty group not dropped: %+v", m.sectors)
	}
}

// Replacing sectors while drilled into one must not leave a dangling
// index behind that indexes past the new slice.
func TestSetSectorsWhileDrilledIn(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m = press(m, "j", "j", "j", "enter")
	if m.sectorIdx < 0 {
		t.Fatal("expected to be drilled in")
	}
	m.SetSectors([]Sector{{Name: "Only", Symbols: []string{"AAA"}}})
	if m.sectorIdx != -1 {
		t.Errorf("sectorIdx = %d, want -1 after SetSectors", m.sectorIdx)
	}
	_ = m.View()
	m = press(m, "j", "enter", "l")
	_ = m.View()
}

// A sector with no quotes at all averaged to exactly 0.00%, which paints
// the same neutral tile as a sector that genuinely did not move.
func TestUnquotedSectorReadsAsNoData(t *testing.T) {
	m := New()
	m.SetSectors([]Sector{{Name: "Quiet", Symbols: []string{"ZZZ"}}})
	m.SetSize(80, 24)
	m.UpdateQuote(provider.Quote{Symbol: "OTHER", Price: 1, ChangePct: 1})

	chg, quoted := m.sectorChange(m.sectors[0])
	if quoted != 0 || chg != 0 {
		t.Fatalf("sectorChange = (%v, %d), want (0, 0)", chg, quoted)
	}
	if !strings.Contains(plain(m.View()), "n/a") {
		t.Error("unquoted sector not marked n/a in the overview")
	}

	m = press(m, "enter")
	if !strings.Contains(plain(m.View()), "n/a") {
		t.Error("unquoted drilldown not marked n/a")
	}
}

func TestSectorChangeAveragesQuotedOnly(t *testing.T) {
	m := New()
	m.SetSectors([]Sector{{Name: "Mixed", Symbols: []string{"AAA", "BBB", "CCC"}}})
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 10, ChangePct: 4})
	m.UpdateQuote(provider.Quote{Symbol: "BBB", Price: 10, ChangePct: 2})
	// CCC never quoted, and a zero-priced quote does not count either.
	m.UpdateQuote(provider.Quote{Symbol: "CCC", Price: 0, ChangePct: -100})

	chg, quoted := m.sectorChange(m.sectors[0])
	if quoted != 2 {
		t.Errorf("quoted = %d, want 2", quoted)
	}
	if chg != 3 {
		t.Errorf("change = %v, want 3", chg)
	}
}

// Sector names come from user config now, so a multibyte name must not
// be sliced mid-rune when a tile is too narrow for it.
func TestMultibyteSectorNameTruncation(t *testing.T) {
	m := New()
	m.SetSectors([]Sector{{Name: "Société Générale Énergie", Symbols: []string{"AAA"}}})
	m.UpdateQuote(provider.Quote{Symbol: "AAA", Price: 10, ChangePct: 1})
	for _, w := range sweepWidths {
		m.SetSize(w, 24)
		if !isValidUTF8(m.View()) {
			t.Fatalf("width %d produced invalid UTF-8", w)
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

func TestLayoutTreemapDegenerate(t *testing.T) {
	for _, n := range []int{0, 1, 2, 18} {
		for _, w := range []int{-5, 0, 1, 200} {
			for _, h := range []int{-5, 0, 1, 60} {
				rects := layoutTreemap(n, w, h)
				if n > 0 && w > 0 && h > 0 && len(rects) != n {
					t.Errorf("layoutTreemap(%d,%d,%d) = %d rects", n, w, h, len(rects))
				}
			}
		}
	}
}

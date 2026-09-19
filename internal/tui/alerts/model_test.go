package alerts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/tui/format"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	engine := alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {})
	engine.SetRules([]alert.Rule{
		{Symbol: "BTC-USD", Condition: alert.CondAbove, Value: 100000, Enabled: true},
		{Symbol: "ETH-USD", Condition: alert.CondBelow, Value: 2000, Enabled: true},
	})
	m := New(engine)
	m.SetSize(100, 40)
	return m
}

func press(m Model, key string) Model {
	m, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
	return m
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	m := newTestModel(t)

	m = press(m, "d")
	if got := len(m.engine.Rules()); got != 2 {
		t.Fatalf("rule deleted without confirmation: %d rules left", got)
	}
	if m.confirmDelete != 0 {
		t.Fatalf("confirmDelete = %d, want 0", m.confirmDelete)
	}

	m = press(m, "y")
	if got := len(m.engine.Rules()); got != 1 {
		t.Fatalf("rules after confirm = %d, want 1", got)
	}
	if m.engine.Rules()[0].Symbol != "ETH-USD" {
		t.Errorf("wrong rule deleted: remaining %s", m.engine.Rules()[0].Symbol)
	}
	if m.confirmDelete != -1 {
		t.Errorf("confirmDelete not reset: %d", m.confirmDelete)
	}
}

func TestDeleteCancelledByOtherKey(t *testing.T) {
	m := newTestModel(t)

	m = press(m, "d")
	m = press(m, "j")
	if got := len(m.engine.Rules()); got != 2 {
		t.Fatalf("rule deleted after cancel: %d rules left", got)
	}
	if m.confirmDelete != -1 {
		t.Errorf("confirmDelete not reset on cancel: %d", m.confirmDelete)
	}
	// The cancelling key is consumed, not applied: cursor stays put.
	if m.cursor != 0 {
		t.Errorf("cancelling key moved cursor to %d", m.cursor)
	}
}

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

func manyRules(n int) *alert.Engine {
	engine := alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {})
	rules := make([]alert.Rule, n)
	for i := range rules {
		rules[i] = alert.Rule{
			Symbol:    fmt.Sprintf("SYM%03d", i),
			Condition: alert.CondAbove,
			Value:     float64(i),
			Enabled:   true,
		}
	}
	engine.SetRules(rules)
	return engine
}

// The rules table used to render every rule regardless of height, so
// anything past the frame overflowed and was unreachable.
func TestRulesTableIsWindowed(t *testing.T) {
	m := New(manyRules(40))
	m.SetSize(100, 20)

	lines := strings.Count(m.View(), "\n")
	if lines > 20 {
		t.Errorf("view is %d lines tall in a 20-line frame", lines)
	}
	ruleRows, _ := m.layout(40)
	if ruleRows >= 40 {
		t.Errorf("ruleRows = %d, want fewer than the 40 rules", ruleRows)
	}
}

// Scrolling past the window must bring the cursor's rule into view.
func TestCursorStaysVisible(t *testing.T) {
	m := New(manyRules(40))
	m.SetSize(100, 20)
	for range 39 {
		m = press(m, "j")
	}
	if m.cursor != 39 {
		t.Fatalf("cursor = %d, want 39", m.cursor)
	}
	out := m.View()
	if !strings.Contains(out, "SYM039") {
		t.Error("cursor rule SYM039 is not rendered after scrolling to the end")
	}
	if strings.Contains(out, "SYM000") {
		t.Error("first rule still rendered after scrolling to the end")
	}
}

func TestScrollIndicatorAppearsOnlyWhenClipped(t *testing.T) {
	m := New(manyRules(40))
	m.SetSize(100, 20)
	if !strings.Contains(m.View(), "of 40") {
		t.Error("clipped rules table has no scroll indicator")
	}
	m.SetSize(100, 60)
	if strings.Contains(m.View(), "of 40") {
		t.Error("scroll indicator shown when the whole table fits")
	}
}

func TestHistoryShareOfTheFrame(t *testing.T) {
	m := New(manyRules(40))
	m.SetSize(100, 20)
	for i := range 30 {
		m.AddTriggered(alert.TriggeredAlert{
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("trigger %d", i),
		})
	}
	if lines := strings.Count(m.View(), "\n"); lines > 20 {
		t.Errorf("rules + history render %d lines in a 20-line frame", lines)
	}
	ruleRows, histRows := m.layout(40)
	if histRows == 0 {
		t.Error("history got no rows at all")
	}
	if ruleRows == 0 {
		t.Error("rules got no rows at all")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	keys := []string{"j", "k", "t", "d", "y", "n", "g", "G", "esc"}
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range keys {
				m := New(manyRules(12))
				m.SetSize(w, h)
				m.AddTriggered(alert.TriggeredAlert{Timestamp: time.Now(), Message: "fired"})
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseClickMsg{X: 1, Y: h})
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
		}
	}
}

func TestEmptyEngineSurvivesEverySize(t *testing.T) {
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			m := New(alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {}))
			m.SetSize(w, h)
			m = press(m, "j")
			m = press(m, "d")
			_ = m.View()
		}
	}
}

// sweepEngine covers the shapes a rule can take in one table: a long
// symbol, a period-qualified condition, a volume threshold in the
// billions, the MACD rule that has no value, a disabled rule and a
// compound one.
func sweepEngine() *alert.Engine {
	engine := alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {})
	engine.SetRules([]alert.Rule{
		{Symbol: "BTC-USD", Condition: alert.CondAbove, Value: 100000, Enabled: true},
		{Symbol: "VERYLONGSYMBOL-USD", Condition: alert.CondSMACrossAbove, Value: 0.00012345, Period: 200, Enabled: true},
		{Symbol: "AAPL", Condition: alert.CondVolumeAbove, Value: 5e9, Enabled: true},
		{Symbol: "ETH-USD", Condition: alert.CondMACDCross, Enabled: false},
		{Symbol: "FRED:DGS10", Condition: alert.CondRSIBelow, Value: 30, Period: 14, Enabled: false},
		{Symbol: "SOL-USD", Match: alert.MatchAll, Enabled: true, Conditions: []alert.SubCondition{
			{Type: alert.CondAbove, Value: 250},
			{Type: alert.CondRSIAbove, Value: 70, Period: 14},
		}},
	})
	return engine
}

// sweepModels is every layout state the tab can be in at one frame size:
// the table, the table with a delete pending, a clipped table, history
// beside the table, history alone, and the empty placeholder.
func sweepModels(w, h int) []Model {
	fired := alert.TriggeredAlert{
		Timestamp: time.Now(),
		Message:   "VERYLONGSYMBOL-USD crossed above its 200-period SMA at 0.00012345",
	}

	table := New(sweepEngine())
	table.SetSize(w, h)

	pending := New(sweepEngine())
	pending.SetSize(w, h)
	pending.cursor = 5 // the compound rule, whose prompt is the longest
	pending = press(pending, "d")

	clipped := New(manyRules(40))
	clipped.SetSize(w, h)
	clipped.cursor = 39

	withHistory := New(sweepEngine())
	withHistory.SetSize(w, h)

	historyOnly := New(alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {}))
	historyOnly.SetSize(w, h)

	for range 12 {
		withHistory.AddTriggered(fired)
		historyOnly.AddTriggered(fired)
	}

	empty := New(alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {}))
	empty.SetSize(w, h)

	return []Model{table, pending, clipped, withHistory, historyOnly, empty}
}

// Every line the tab paints must fit the frame it was given. The content
// panel word-wraps a line it cannot fit, and a row on two screen lines
// pushes the last row past the status bar and desynchronizes the click
// hit-test, which maps a mouse row straight to a data-row index.
func TestEveryLineFitsTheFrame(t *testing.T) {
	for w := 1; w <= 200; w++ {
		for i, m := range sweepModels(w, 24) {
			for n, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Fatalf("width %d: model %d line %d is %d cells: %q",
						w, i, n, got, line)
				}
			}
		}
	}
}

// Below the width the narrowest column needs, the tab says so rather
// than painting a table that cannot hold a rule.
func TestTooNarrowReplacesTheTable(t *testing.T) {
	for w := 1; w <= 200; w++ {
		m := New(sweepEngine())
		m.SetSize(w, 24)
		out := m.View()
		if m.fit() == nil {
			if strings.Contains(out, "BTC-USD") {
				t.Fatalf("width %d: rules rendered with no fitting layout", w)
			}
			if !strings.Contains(out, format.Truncate(tooNarrow, w)) {
				t.Fatalf("width %d: no too-narrow line and no table: %q", w, out)
			}
			continue
		}
		if !strings.Contains(out, "BTC") {
			t.Fatalf("width %d: fitting layout rendered no rule: %q", w, out)
		}
	}
}

// The header and its cells share one plan, so STATUS sits over ON/OFF.
func TestHeaderAndCellsShareTheLayout(t *testing.T) {
	m := New(sweepEngine())
	m.SetSize(100, 24)
	lines := strings.Split(m.View(), "\n")

	var header, row string
	for _, line := range lines {
		if strings.Contains(line, "STATUS") {
			header = line
		}
		if strings.Contains(line, "BTC-USD") {
			row = line
		}
	}
	if header == "" || row == "" {
		t.Fatalf("header or row missing from view:\n%s", strings.Join(lines, "\n"))
	}
	if lipgloss.Width(header) != lipgloss.Width(row) {
		t.Errorf("header is %d cells, row is %d cells", lipgloss.Width(header), lipgloss.Width(row))
	}
}

// A threshold is truncated to its column, never written past it.
func TestValueFitsItsColumn(t *testing.T) {
	cases := []struct {
		rule  alert.Rule
		cells int
	}{
		{alert.Rule{Condition: alert.CondVolumeAbove, Value: 5e9}, 12},
		{alert.Rule{Condition: alert.CondVolumeAbove, Value: 5e9}, 8},
		{alert.Rule{Condition: alert.CondAbove, Value: -1234567890123}, 8},
		{alert.Rule{Condition: alert.CondAbove, Value: 100000}, 12},
		{alert.Rule{Condition: alert.CondMACDCross}, 8},
	}
	for _, c := range cases {
		got := valueText(c.rule, c.cells)
		if lipgloss.Width(got) > c.cells {
			t.Errorf("value %g in %d cells rendered %q (%d cells)",
				c.rule.Value, c.cells, got, lipgloss.Width(got))
		}
	}
}

// adversarialEngine is the rules table at its most hostile: an empty
// symbol, a fifteen-digit and a negative threshold, an instrument name
// longer than any frame, and free text whose grapheme clusters measure
// more cells than their runes do — a base glyph plus U+FE0F, a keycap
// sequence, a regional-indicator pair and a ZWJ family.
func adversarialEngine() *alert.Engine {
	engine := alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {})
	engine.SetRules([]alert.Rule{
		{Symbol: "", Condition: alert.CondAbove, Enabled: true},
		{Symbol: "A™️B-USD", Condition: alert.CondAbove, Value: 123456789012345, Enabled: true},
		{Symbol: "AB1️⃣CD", Condition: alert.CondAbove, Value: -0.000000001, Enabled: true},
		{Symbol: "x❤️y", Condition: alert.CondBelow, Value: -98765.4321, Enabled: false},
		{Symbol: "日本語トークン-USD", Condition: alert.CondRSIAbove, Value: 1e18, Period: 14, Enabled: true},
		{Symbol: "🇺🇸🇺🇸🇺🇸", Condition: alert.CondVolumeAbove, Value: -1e15, Enabled: true},
		{Symbol: "👨‍👩‍👧‍👦", Condition: alert.CondMACDCross, Enabled: false},
		{Symbol: strings.Repeat("LONGSYM", 12), Condition: alert.CondSMACrossAbove, Value: 0.000000123456789, Period: 200, Enabled: true},
		{Symbol: "⚠️ALERT⚠️", Match: alert.MatchAll, Enabled: true, Conditions: []alert.SubCondition{
			{Type: alert.CondAbove, Value: 250},
		}},
	})
	return engine
}

// The frame is budgeted in display cells, and a cluster is the unit that
// spends them: a per-rune budget under-counts a base glyph followed by a
// variation selector, so the cell it was cut for measures one more than
// the column that holds it and the row overruns the frame.
func TestEveryLineFitsTheFrameOnAdversarialData(t *testing.T) {
	messages := []string{
		"",
		"a™️b",
		"AB1️⃣CD crossed above 123456789012345.0000",
		"日本語のアラート " + strings.Repeat("長", 40),
		"🇺🇸 " + strings.Repeat("👨‍👩‍👧‍👦", 20),
		strings.Repeat("A", 300),
	}
	for _, h := range []int{1, 3, 8, 24} {
		for w := 1; w <= 200; w++ {
			models := []Model{New(adversarialEngine())}
			for cursor := range 9 {
				pending := New(adversarialEngine())
				pending.SetSize(w, h)
				pending.cursor = cursor
				models = append(models, press(pending, "d"))
			}
			for _, msg := range messages {
				withHistory := New(adversarialEngine())
				historyOnly := New(alert.NewEngine(time.Minute, func(alert.TriggeredAlert) {}))
				for range 12 {
					fired := alert.TriggeredAlert{Timestamp: time.Now(), Message: msg}
					withHistory.AddTriggered(fired)
					historyOnly.AddTriggered(fired)
				}
				models = append(models, withHistory, historyOnly)
			}
			for i, m := range models {
				m.SetSize(w, h)
				for n, line := range strings.Split(m.View(), "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("height %d width %d: model %d line %d is %d cells: %q",
							h, w, i, n, got, line)
					}
				}
			}
		}
	}
}

// The hit-test maps a mouse row straight to a data-row index, so it holds
// only over the rows the table paints. The hint line, the blank line under
// the table and the whole recent-alerts block sit below the last rule on
// screen: a click there must not move the cursor to a rule the viewport is
// not showing.
func TestClickBelowTheTableIsInert(t *testing.T) {
	m := New(manyRules(40))
	m.SetSize(100, 20)
	for range 6 {
		m.AddTriggered(alert.TriggeredAlert{Timestamp: time.Now(), Message: "fired"})
	}
	ruleRows, histRows := m.layout(40)
	if ruleRows == 0 || histRows == 0 {
		t.Fatalf("frame does not paint both blocks: ruleRows=%d histRows=%d", ruleRows, histRows)
	}

	painted := strings.Count(m.View(), "\n")
	for y := rulesHeaderLines + ruleRows; y <= painted+2; y++ {
		click := m
		click, _ = click.Update(tea.MouseClickMsg{X: 1, Y: y})
		if click.cursor != m.cursor {
			t.Errorf("click on row %d, below the %d painted rules, moved the cursor to %d",
				y, ruleRows, click.cursor)
		}
	}

	// The rows the table does paint still select the rule under them.
	for row := range ruleRows {
		click := m
		click, _ = click.Update(tea.MouseClickMsg{X: 1, Y: rulesHeaderLines + row})
		if want := format.ViewportStart(m.cursor, 40, ruleRows) + row; click.cursor != want {
			t.Errorf("click on painted row %d selected rule %d, want %d", row, click.cursor, want)
		}
	}
}

package alerts

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
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

package alerts

import (
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

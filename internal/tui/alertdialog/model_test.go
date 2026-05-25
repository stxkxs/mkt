package alertdialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
)

func newDialog(t *testing.T) (Model, *alert.Engine) {
	t.Helper()
	engine := alert.NewEngine(0, nil)
	return New(engine), engine
}

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func digit(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// indexOf returns the slot in AllConditions() that holds c. Lets tests
// stay readable when alert.Condition ordering shifts.
func indexOf(c alert.Condition) int {
	for i, x := range alert.AllConditions() {
		if x == c {
			return i
		}
	}
	return -1
}

func TestNewIsInactive(t *testing.T) {
	m, _ := newDialog(t)
	if m.Active() {
		t.Error("new dialog should not be active")
	}
}

func TestOpenInitializesState(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 200.0)

	if !m.Active() {
		t.Fatal("Open: should be active")
	}
	if m.symbol != "AAPL" {
		t.Errorf("symbol: got %q, want AAPL", m.symbol)
	}
	if m.currentPrice != 200.0 {
		t.Errorf("price: got %v, want 200", m.currentPrice)
	}
	if m.valueInput != "200.00" {
		t.Errorf("valueInput: got %q, want 200.00", m.valueInput)
	}
	if m.step != stepCondition {
		t.Errorf("step: got %d, want stepCondition", m.step)
	}
	if m.condIdx != 0 {
		t.Errorf("condIdx: got %d, want 0", m.condIdx)
	}
}

func TestEscClosesDialog(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	m, _ = m.Update(key(tea.KeyEscape))
	if m.Active() {
		t.Error("esc: dialog should be closed")
	}
}

func TestArrowsCycleConditionsWithWraparound(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	n := len(alert.AllConditions())

	// right cycles forward
	m, _ = m.Update(key(tea.KeyRight))
	if m.condIdx != 1 {
		t.Errorf("right from 0: got %d, want 1", m.condIdx)
	}
	// left from 1 → 0
	m, _ = m.Update(key(tea.KeyLeft))
	if m.condIdx != 0 {
		t.Errorf("left from 1: got %d, want 0", m.condIdx)
	}
	// left from 0 wraps to last
	m, _ = m.Update(key(tea.KeyLeft))
	if m.condIdx != n-1 {
		t.Errorf("left wrap: got %d, want %d", m.condIdx, n-1)
	}
	// right from last wraps to 0
	m, _ = m.Update(key(tea.KeyRight))
	if m.condIdx != 0 {
		t.Errorf("right wrap: got %d, want 0", m.condIdx)
	}
}

func TestEnterAdvancesFromConditionToValue(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	// condIdx 0 = CondAbove (non-MACD) → advances to value step
	m, _ = m.Update(key(tea.KeyEnter))
	if m.step != stepValue {
		t.Errorf("step after enter: got %d, want stepValue", m.step)
	}
}

func TestMACDCrossSkipsValueStep(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	// Cycle to MACD cross.
	for i := 0; i < indexOf(alert.CondMACDCross); i++ {
		m, _ = m.Update(key(tea.KeyRight))
	}
	if m.conditions[m.condIdx] != alert.CondMACDCross {
		t.Fatalf("setup: expected MACDCross at idx %d, got %v", m.condIdx, m.conditions[m.condIdx])
	}

	m, _ = m.Update(key(tea.KeyEnter))
	if m.step != stepConfirm {
		t.Errorf("MACD enter should skip stepValue: got step %d, want stepConfirm", m.step)
	}
	if m.valueInput != "0" {
		t.Errorf("MACD valueInput: got %q, want 0", m.valueInput)
	}
}

func TestDigitsAppendInValueStep(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	m, _ = m.Update(key(tea.KeyEnter)) // → stepValue, valueInput is "100.00"
	if m.step != stepValue {
		t.Fatalf("setup: not at stepValue, got %d", m.step)
	}
	for _, r := range "55" {
		m, _ = m.Update(digit(r))
	}
	if m.valueInput != "100.0055" {
		t.Errorf("after appending '55': got %q, want 100.0055", m.valueInput)
	}
}

func TestBackspaceRemovesCharInValueStep(t *testing.T) {
	m, _ := newDialog(t)
	m.Open("AAPL", 100)
	m, _ = m.Update(key(tea.KeyEnter)) // → stepValue, valueInput "100.00"
	m, _ = m.Update(key(tea.KeyBackspace))
	if m.valueInput != "100.0" {
		t.Errorf("after backspace: got %q, want 100.0", m.valueInput)
	}
}

func TestConfirmAddsRuleAndCloses(t *testing.T) {
	m, engine := newDialog(t)
	m.Open("AAPL", 100)

	// stepCondition → stepValue → stepConfirm → save
	m, _ = m.Update(key(tea.KeyEnter))
	m, _ = m.Update(key(tea.KeyEnter))
	if m.step != stepConfirm {
		t.Fatalf("setup: not at stepConfirm, got %d", m.step)
	}
	m, _ = m.Update(key(tea.KeyEnter))
	if m.Active() {
		t.Error("after save: dialog should be closed")
	}
	rules := engine.Rules()
	if len(rules) != 1 {
		t.Fatalf("rules: got %d, want 1", len(rules))
	}
	r := rules[0]
	if r.Symbol != "AAPL" || r.Condition != alert.CondAbove || r.Value != 100.0 || !r.Enabled {
		t.Errorf("rule: %+v", r)
	}
}

func TestConfirmWithBadValueDoesNotSave(t *testing.T) {
	m, engine := newDialog(t)
	m.Open("AAPL", 100)
	m, _ = m.Update(key(tea.KeyEnter)) // → stepValue
	// Wipe valueInput, then leave empty.
	for range len(m.valueInput) {
		m, _ = m.Update(key(tea.KeyBackspace))
	}
	m, _ = m.Update(key(tea.KeyEnter)) // → stepConfirm
	m, _ = m.Update(key(tea.KeyEnter)) // attempt save with empty input
	if !m.Active() {
		t.Error("dialog should stay open on parse failure")
	}
	if len(engine.Rules()) != 0 {
		t.Errorf("rules: got %d, want 0", len(engine.Rules()))
	}
}

func TestRSIConditionGetsDefaultPeriod(t *testing.T) {
	m, engine := newDialog(t)
	m.Open("AAPL", 100)
	for i := 0; i < indexOf(alert.CondRSIAbove); i++ {
		m, _ = m.Update(key(tea.KeyRight))
	}
	m, _ = m.Update(key(tea.KeyEnter)) // → stepValue
	m, _ = m.Update(key(tea.KeyEnter)) // → stepConfirm
	m, _ = m.Update(key(tea.KeyEnter)) // save
	rules := engine.Rules()
	if len(rules) != 1 || rules[0].Period != 14 {
		t.Errorf("RSI rule period: got %+v, want period=14", rules)
	}
}

func TestSMACrossConditionGetsDefaultPeriod(t *testing.T) {
	m, engine := newDialog(t)
	m.Open("AAPL", 100)
	for i := 0; i < indexOf(alert.CondSMACrossAbove); i++ {
		m, _ = m.Update(key(tea.KeyRight))
	}
	m, _ = m.Update(key(tea.KeyEnter)) // → stepValue
	m, _ = m.Update(key(tea.KeyEnter)) // → stepConfirm
	m, _ = m.Update(key(tea.KeyEnter)) // save
	rules := engine.Rules()
	if len(rules) != 1 || rules[0].Period != 20 {
		t.Errorf("SMA cross rule period: got %+v, want period=20", rules)
	}
}

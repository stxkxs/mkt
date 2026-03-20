package alert

import "time"

// Condition defines the type of price alert.
type Condition string

const (
	CondAbove         Condition = "above"
	CondBelow         Condition = "below"
	CondPctUp         Condition = "pct_up"
	CondPctDown       Condition = "pct_down"
	CondRSIAbove      Condition = "rsi_above"
	CondRSIBelow      Condition = "rsi_below"
	CondSMACrossAbove Condition = "sma_cross_above"
	CondSMACrossBelow Condition = "sma_cross_below"
	CondMACDCross     Condition = "macd_cross"
)

// AllConditions returns all available alert conditions.
func AllConditions() []Condition {
	return []Condition{
		CondAbove, CondBelow, CondPctUp, CondPctDown,
		CondRSIAbove, CondRSIBelow,
		CondSMACrossAbove, CondSMACrossBelow,
		CondMACDCross,
	}
}

// IsIndicatorCondition returns true if the condition requires price history.
func IsIndicatorCondition(c Condition) bool {
	switch c {
	case CondRSIAbove, CondRSIBelow, CondSMACrossAbove, CondSMACrossBelow, CondMACDCross:
		return true
	}
	return false
}

// Rule is a single alert configuration.
type Rule struct {
	Symbol    string
	Condition Condition
	Value     float64
	Period    int // indicator period (default 14 for RSI, 20 for SMA)
	Enabled   bool
}

// TriggeredAlert is emitted when a rule fires.
type TriggeredAlert struct {
	Rule      Rule
	Price     float64
	Message   string
	Timestamp time.Time
}

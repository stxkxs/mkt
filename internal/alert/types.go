package alert

import "time"

// Condition defines the type of price alert.
type Condition string

const (
	CondAbove   Condition = "above"
	CondBelow   Condition = "below"
	CondPctUp   Condition = "pct_up"
	CondPctDown Condition = "pct_down"
)

// Rule is a single alert configuration.
type Rule struct {
	Symbol    string
	Condition Condition
	Value     float64
	Enabled   bool
}

// TriggeredAlert is emitted when a rule fires.
type TriggeredAlert struct {
	Rule      Rule
	Price     float64
	Message   string
	Timestamp time.Time
}

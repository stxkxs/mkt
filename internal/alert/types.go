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
	CondVolumeAbove   Condition = "volume_above"
	CondStddevAbove   Condition = "stddev_above"
)

// AllConditions returns all available alert conditions.
func AllConditions() []Condition {
	return []Condition{
		CondAbove, CondBelow, CondPctUp, CondPctDown,
		CondRSIAbove, CondRSIBelow,
		CondSMACrossAbove, CondSMACrossBelow,
		CondMACDCross,
		CondVolumeAbove, CondStddevAbove,
	}
}

// IsIndicatorCondition returns true if the condition requires price history.
func IsIndicatorCondition(c Condition) bool {
	switch c {
	case CondRSIAbove, CondRSIBelow, CondSMACrossAbove, CondSMACrossBelow, CondMACDCross, CondStddevAbove:
		return true
	}
	return false
}

// Match identifies how a compound rule combines its sub-conditions.
const (
	MatchAll      = "all"
	MatchAny      = "any"
	MatchSequence = "sequence"
)

// SubCondition is one leaf inside a compound rule.
type SubCondition struct {
	Type   Condition
	Value  float64
	Period int // indicator period (default 14 for RSI, 20 for SMA)
}

// Rule is a single alert configuration. When Conditions is non-empty
// the legacy Condition / Value / Period fields are ignored and the rule
// is evaluated as a compound according to Match.
type Rule struct {
	Symbol     string
	Condition  Condition // legacy single-condition fields
	Value      float64
	Period     int
	Enabled    bool
	Webhooks   []string       // per-rule webhook URLs; overrides any global default
	Conditions []SubCondition // optional; when set, evaluated as a compound
	Match      string         // "all" | "any" | "sequence"; default "all" when Conditions is set
}

// IsCompound reports whether the rule should be evaluated as a compound.
func (r Rule) IsCompound() bool { return len(r.Conditions) > 0 }

// TriggeredAlert is emitted when a rule fires.
type TriggeredAlert struct {
	Rule      Rule
	Price     float64
	Message   string
	Timestamp time.Time
}

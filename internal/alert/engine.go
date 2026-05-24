package alert

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
)

const (
	defaultCooldown = 5 * time.Minute
	notifyTimeout   = 5 * time.Second
)

// PriceSource provides historical prices for indicator evaluation.
type PriceSource interface {
	Prices(symbol string) []float64
}

// Engine evaluates alert rules against incoming quotes.
type Engine struct {
	mu        sync.RWMutex
	rules     []Rule
	cooldowns map[string]time.Time // key = rule identity, value = next allowed fire time
	cooldown  time.Duration
	onAlert   func(TriggeredAlert)
	prices    PriceSource
	notifiers []Notifier

	// Track reference prices for pct conditions
	refPrices map[string]float64 // symbol -> first seen price

	// Per-rule progress for compound rules
	compoundState map[string]*compoundProgress
}

// compoundProgress tracks evaluation state for a single compound rule.
type compoundProgress struct {
	fired   []bool // for "all" mode — which sub-conditions have fired
	nextIdx int    // for "sequence" mode — next-expected sub index
}

// NewEngine creates an alert engine.
func NewEngine(cooldown time.Duration, onAlert func(TriggeredAlert)) *Engine {
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	return &Engine{
		cooldowns:     make(map[string]time.Time),
		cooldown:      cooldown,
		onAlert:       onAlert,
		refPrices:     make(map[string]float64),
		compoundState: make(map[string]*compoundProgress),
	}
}

// SetPriceSource sets the price history source for indicator-based alerts.
func (e *Engine) SetPriceSource(ps PriceSource) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prices = ps
}

// AddNotifier registers a destination that receives every triggered alert.
// Notifiers are called in registration order with a per-call timeout; errors
// are logged and never propagated.
func (e *Engine) AddNotifier(n Notifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifiers = append(e.notifiers, n)
}

// SetRules replaces all rules. Any compound-rule progress is cleared
// because rule indices (and therefore keys) may have changed.
func (e *Engine) SetRules(rules []Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
	e.compoundState = make(map[string]*compoundProgress)
}

// Rules returns a copy of current rules.
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// AddRule adds a new alert rule.
func (e *Engine) AddRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, r)
}

// RemoveRule removes a rule by index.
func (e *Engine) RemoveRule(idx int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if idx >= 0 && idx < len(e.rules) {
		e.rules = append(e.rules[:idx], e.rules[idx+1:]...)
	}
}

// ToggleRule toggles a rule's enabled state.
func (e *Engine) ToggleRule(idx int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if idx >= 0 && idx < len(e.rules) {
		e.rules[idx].Enabled = !e.rules[idx].Enabled
	}
}

// Check evaluates all rules against a quote. Triggered alerts are collected
// under the lock and dispatched after release so slow notifiers cannot stall
// other engine operations.
func (e *Engine) Check(q provider.Quote) {
	e.mu.Lock()

	if _, ok := e.refPrices[q.Symbol]; !ok {
		e.refPrices[q.Symbol] = q.Price
	}

	now := time.Now()
	var triggered []TriggeredAlert
	for i, r := range e.rules {
		if !r.Enabled || r.Symbol != q.Symbol {
			continue
		}

		key := ruleKey(r, i)
		if next, ok := e.cooldowns[key]; ok && now.Before(next) {
			continue
		}

		var fires bool
		var msg string

		if r.IsCompound() {
			fires, msg = e.evaluateCompound(r, key, q)
		} else if IsIndicatorCondition(r.Condition) {
			if e.prices != nil {
				prices := e.prices.Prices(q.Symbol)
				fires, msg = evaluateIndicator(r, prices)
			}
		} else {
			fires, msg = evaluate(r, q, e.refPrices[q.Symbol])
		}

		if !fires {
			continue
		}

		e.cooldowns[key] = now.Add(e.cooldown)
		delete(e.compoundState, key) // reset compound progress on fire
		triggered = append(triggered, TriggeredAlert{
			Rule:      r,
			Price:     q.Price,
			Message:   msg,
			Timestamp: now,
		})
	}

	onAlert := e.onAlert
	notifiers := make([]Notifier, len(e.notifiers))
	copy(notifiers, e.notifiers)
	e.mu.Unlock()

	for _, a := range triggered {
		if onAlert != nil {
			onAlert(a)
		}
		for _, n := range notifiers {
			ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
			if err := n.Notify(ctx, a); err != nil {
				log.Printf("alert notifier %s: %v", n.Name(), err)
			}
			cancel()
		}
	}
}

func evaluate(r Rule, q provider.Quote, refPrice float64) (bool, string) {
	price := q.Price
	switch r.Condition {
	case CondAbove:
		if price >= r.Value {
			return true, fmt.Sprintf("%s price %.4f crossed above %.4f", r.Symbol, price, r.Value)
		}
	case CondBelow:
		if price <= r.Value {
			return true, fmt.Sprintf("%s price %.4f crossed below %.4f", r.Symbol, price, r.Value)
		}
	case CondPctUp:
		if refPrice > 0 {
			pct := ((price - refPrice) / refPrice) * 100
			if pct >= r.Value {
				return true, fmt.Sprintf("%s up %.1f%% (from %.4f to %.4f)", r.Symbol, pct, refPrice, price)
			}
		}
	case CondPctDown:
		if refPrice > 0 {
			pct := ((refPrice - price) / refPrice) * 100
			if pct >= r.Value {
				return true, fmt.Sprintf("%s down %.1f%% (from %.4f to %.4f)", r.Symbol, pct, refPrice, price)
			}
		}
	case CondVolumeAbove:
		if q.Volume > r.Value {
			return true, fmt.Sprintf("%s volume %.0f exceeds %.0f", r.Symbol, q.Volume, r.Value)
		}
	}
	return false, ""
}

func evaluateIndicator(r Rule, prices []float64) (bool, string) {
	if len(prices) < 2 {
		return false, ""
	}

	switch r.Condition {
	case CondRSIAbove, CondRSIBelow:
		period := r.Period
		if period <= 0 {
			period = 14
		}
		if len(prices) < period+1 {
			return false, ""
		}
		rsiVals := indicator.RSI(prices, period)
		last := rsiVals[len(rsiVals)-1]
		if math.IsNaN(last) {
			return false, ""
		}
		if r.Condition == CondRSIAbove && last >= r.Value {
			return true, fmt.Sprintf("%s RSI(%d) = %.1f crossed above %.1f", r.Symbol, period, last, r.Value)
		}
		if r.Condition == CondRSIBelow && last <= r.Value {
			return true, fmt.Sprintf("%s RSI(%d) = %.1f crossed below %.1f", r.Symbol, period, last, r.Value)
		}

	case CondSMACrossAbove, CondSMACrossBelow:
		period := r.Period
		if period <= 0 {
			period = 20
		}
		if len(prices) < period+1 {
			return false, ""
		}
		smaVals := indicator.SMA(prices, period)
		n := len(prices)
		curr := prices[n-1]
		prev := prices[n-2]
		smaCurr := smaVals[n-1]
		smaPrev := smaVals[n-2]
		if math.IsNaN(smaCurr) || math.IsNaN(smaPrev) {
			return false, ""
		}
		if r.Condition == CondSMACrossAbove && prev <= smaPrev && curr > smaCurr {
			return true, fmt.Sprintf("%s price crossed above SMA(%d) at %.4f", r.Symbol, period, smaCurr)
		}
		if r.Condition == CondSMACrossBelow && prev >= smaPrev && curr < smaCurr {
			return true, fmt.Sprintf("%s price crossed below SMA(%d) at %.4f", r.Symbol, period, smaCurr)
		}

	case CondMACDCross:
		if len(prices) < 35 {
			return false, ""
		}
		macdResult := indicator.MACD(prices, 12, 26, 9)
		n := len(prices)
		currDiff := macdResult.MACD[n-1] - macdResult.Signal[n-1]
		prevDiff := macdResult.MACD[n-2] - macdResult.Signal[n-2]
		if math.IsNaN(currDiff) || math.IsNaN(prevDiff) {
			return false, ""
		}
		// Sign change = crossover
		if prevDiff <= 0 && currDiff > 0 {
			return true, fmt.Sprintf("%s MACD bullish crossover (MACD=%.4f, Signal=%.4f)", r.Symbol, macdResult.MACD[n-1], macdResult.Signal[n-1])
		}
		if prevDiff >= 0 && currDiff < 0 {
			return true, fmt.Sprintf("%s MACD bearish crossover (MACD=%.4f, Signal=%.4f)", r.Symbol, macdResult.MACD[n-1], macdResult.Signal[n-1])
		}

	case CondStddevAbove:
		period := r.Period
		if period <= 1 {
			period = 20
		}
		if len(prices) < period {
			return false, ""
		}
		window := prices[len(prices)-period:]
		var sum float64
		for _, v := range window {
			sum += v
		}
		mean := sum / float64(period)
		if mean == 0 {
			return false, ""
		}
		stddevs := indicator.Stddev(prices, period)
		sd := stddevs[len(stddevs)-1]
		if math.IsNaN(sd) {
			return false, ""
		}
		pct := 100 * sd / mean
		if pct >= r.Value {
			return true, fmt.Sprintf("%s stddev %.2f%% of mean exceeds %.2f%% (period %d)", r.Symbol, pct, r.Value, period)
		}
	}

	return false, ""
}

func ruleKey(r Rule, idx int) string {
	return fmt.Sprintf("%d:%s:%s:%.8f", idx, r.Symbol, r.Condition, r.Value)
}

// evaluateCompound evaluates a compound rule against the latest quote.
// Caller holds the engine lock. May mutate compoundState.
func (e *Engine) evaluateCompound(r Rule, key string, q provider.Quote) (bool, string) {
	if len(r.Conditions) == 0 {
		return false, ""
	}
	prog, ok := e.compoundState[key]
	if !ok {
		prog = &compoundProgress{fired: make([]bool, len(r.Conditions))}
		e.compoundState[key] = prog
	}

	match := r.Match
	if match == "" {
		match = MatchAll
	}
	var prices []float64
	if e.prices != nil {
		prices = e.prices.Prices(q.Symbol)
	}
	evalSub := func(s SubCondition) (bool, string) {
		tmp := Rule{Symbol: r.Symbol, Condition: s.Type, Value: s.Value, Period: s.Period}
		if IsIndicatorCondition(s.Type) {
			return evaluateIndicator(tmp, prices)
		}
		return evaluate(tmp, q, e.refPrices[q.Symbol])
	}

	switch match {
	case MatchAny:
		for _, sub := range r.Conditions {
			if fires, msg := evalSub(sub); fires {
				return true, "any: " + msg
			}
		}
		return false, ""

	case MatchSequence:
		if prog.nextIdx >= len(r.Conditions) {
			// Should have fired and reset; defensive.
			return true, "sequence complete"
		}
		sub := r.Conditions[prog.nextIdx]
		if fires, _ := evalSub(sub); fires {
			prog.nextIdx++
		}
		if prog.nextIdx >= len(r.Conditions) {
			return true, fmt.Sprintf("sequence complete (%d steps)", len(r.Conditions))
		}
		return false, ""

	default: // MatchAll
		for i, sub := range r.Conditions {
			if prog.fired[i] {
				continue
			}
			if fires, _ := evalSub(sub); fires {
				prog.fired[i] = true
			}
		}
		for _, f := range prog.fired {
			if !f {
				return false, ""
			}
		}
		return true, fmt.Sprintf("all conditions met (%d)", len(r.Conditions))
	}
}

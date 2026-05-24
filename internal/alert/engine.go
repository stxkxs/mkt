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
}

// NewEngine creates an alert engine.
func NewEngine(cooldown time.Duration, onAlert func(TriggeredAlert)) *Engine {
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	return &Engine{
		cooldowns: make(map[string]time.Time),
		cooldown:  cooldown,
		onAlert:   onAlert,
		refPrices: make(map[string]float64),
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

// SetRules replaces all rules.
func (e *Engine) SetRules(rules []Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
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

		if IsIndicatorCondition(r.Condition) {
			if e.prices != nil {
				prices := e.prices.Prices(q.Symbol)
				fires, msg = evaluateIndicator(r, prices)
			}
		} else {
			fires, msg = evaluate(r, q.Price, e.refPrices[q.Symbol])
		}

		if !fires {
			continue
		}

		e.cooldowns[key] = now.Add(e.cooldown)
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

func evaluate(r Rule, price, refPrice float64) (bool, string) {
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
	}

	return false, ""
}

func ruleKey(r Rule, idx int) string {
	return fmt.Sprintf("%d:%s:%s:%.8f", idx, r.Symbol, r.Condition, r.Value)
}

package alert

import (
	"fmt"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const defaultCooldown = 5 * time.Minute

// Engine evaluates alert rules against incoming quotes.
type Engine struct {
	mu        sync.RWMutex
	rules     []Rule
	cooldowns map[string]time.Time // key = rule identity, value = next allowed fire time
	cooldown  time.Duration
	onAlert   func(TriggeredAlert)

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

// Check evaluates all rules against a quote.
func (e *Engine) Check(q provider.Quote) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Track reference price
	if _, ok := e.refPrices[q.Symbol]; !ok {
		e.refPrices[q.Symbol] = q.Price
	}

	now := time.Now()
	for i, r := range e.rules {
		if !r.Enabled || r.Symbol != q.Symbol {
			continue
		}

		key := ruleKey(r, i)
		if next, ok := e.cooldowns[key]; ok && now.Before(next) {
			continue
		}

		triggered, msg := evaluate(r, q.Price, e.refPrices[q.Symbol])
		if !triggered {
			continue
		}

		alert := TriggeredAlert{
			Rule:      r,
			Price:     q.Price,
			Message:   msg,
			Timestamp: now,
		}

		e.cooldowns[key] = now.Add(e.cooldown)

		if e.onAlert != nil {
			e.onAlert(alert)
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

func ruleKey(r Rule, idx int) string {
	return fmt.Sprintf("%d:%s:%s:%.8f", idx, r.Symbol, r.Condition, r.Value)
}

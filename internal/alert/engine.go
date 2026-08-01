package alert

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
)

const (
	defaultCooldown = 5 * time.Minute
	notifyTimeout   = 5 * time.Second

	// Per-notifier send cap: a safety net independent of per-rule cooldown so
	// the cooldown-bypassing Inject path (TradingView webhook) or an
	// oscillating rule can't drain a paid Pushover quota or spam a phone.
	// ~20/min sustained with a burst of 10.
	notifierMinInterval = 3 * time.Second
	notifierBurst       = 10

	// Default indicator lookbacks, used when a rule leaves Period unset.
	defaultRSIPeriod    = 14
	defaultSMAPeriod    = 20
	defaultStddevPeriod = 20

	// MACD(12,26,9) produces its first signal value only after the 26-period
	// slow EMA has warmed up and the 9-period signal EMA has run over it.
	macdMinSamples = 35
)

// PriceSource provides historical prices for indicator evaluation.
type PriceSource interface {
	Prices(symbol string) []float64
}

// Engine evaluates alert rules against incoming quotes.
//
// Level conditions (above, below, pct_up, pct_down, volume_above,
// rsi_above, rsi_below, stddev_above) are edge-triggered: the engine
// remembers the previous evaluation of every rule and fires only on the
// transition into the condition. The first evaluation after startup
// establishes that baseline without firing, so a rule that is already
// breached when mkt launches stays quiet until it un-breaches and
// re-breaches. Cross conditions (sma_cross_above, sma_cross_below,
// macd_cross) compare two adjacent samples and are edge-triggered by
// construction.
type Engine struct {
	mu        sync.RWMutex
	rules     []Rule
	cooldowns map[string]time.Time // key = rule identity, value = next allowed fire time
	cooldown  time.Duration
	onAlert   func(TriggeredAlert)
	prices    PriceSource
	sinks     []*notifierSink

	// now supplies wall time for quotes that carry no timestamp of their
	// own. Defaults to time.Now; a replay injects recorded time.
	now func() time.Time

	// levelState holds the previous evaluation of every edge-triggered
	// rule, keyed by rule identity. A missing key means "not yet
	// evaluated" — the next evaluation establishes the baseline.
	levelState map[string]bool

	// Track reference prices for pct conditions
	refPrices   map[string]float64 // symbol -> first price seen this process
	sessionRefs map[string]float64 // symbol -> previous close derived from the quote

	// Per-rule progress for compound rules
	compoundState map[string]*compoundProgress

	// warnedHistory records which rules have already reported insufficient
	// price history, so onShortHistory fires once per dry spell rather than
	// once per quote.
	warnedHistory map[string]bool

	onRulesChanged func([]Rule)
	onShortHistory func(RuleStatus)

	// inflight counts notifications enqueued but not yet delivered;
	// notifyDrops counts those the engine threw away.
	inflight    atomic.Int64
	notifyDrops atomic.Uint64
}

// compoundProgress tracks evaluation state for a single compound rule.
type compoundProgress struct {
	fired   []bool // for "all" mode — which sub-conditions have fired
	nextIdx int    // for "sequence" mode — next-expected sub index
}

// RuleStatus reports whether a rule can currently be evaluated.
//
// Indicator conditions need a minimum number of cached price samples
// before they produce any value at all, and the cache is a ring sized by
// sparkline_len that starts empty on every restart. Until it fills, an
// rsi_above(14) rule needs 15 samples and an sma_cross(50) needs 51 — so
// the rule is inert, not false. RuleStatus surfaces that instead of
// hiding it.
type RuleStatus struct {
	Index  int    // position in the slice returned by Rules
	Rule   Rule   // the rule itself
	Ready  bool   // false while Have < Need
	Have   int    // price samples currently cached for Rule.Symbol
	Need   int    // samples the rule's conditions require; 0 when none do
	Reason string // why the rule is not ready; empty when Ready
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
		now:           time.Now,
		levelState:    make(map[string]bool),
		refPrices:     make(map[string]float64),
		sessionRefs:   make(map[string]float64),
		compoundState: make(map[string]*compoundProgress),
		warnedHistory: make(map[string]bool),
	}
}

// SetClock replaces the engine's source of wall time. It is used for
// quotes that carry no timestamp of their own, and it is what a replay
// injects so cooldowns are measured in recorded time rather than in the
// milliseconds a burst replay actually takes. A nil argument restores
// time.Now.
func (e *Engine) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = now
}

// SetPriceSource sets the price history source for indicator-based alerts.
func (e *Engine) SetPriceSource(ps PriceSource) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prices = ps
}

// SetOnRulesChanged registers a callback invoked with the full rule set
// after AddRule, RemoveRule or ToggleRule. This is the hook that lets
// alerts created in the dashboard survive a restart — the caller persists
// the rules it receives. It deliberately does not fire on SetRules, which
// is the load path and would only write back what was just read. The
// callback runs outside the engine lock, so it may call back into the
// engine.
func (e *Engine) SetOnRulesChanged(fn func([]Rule)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onRulesChanged = fn
}

// SetOnShortHistory registers a callback invoked once per rule when that
// rule cannot be evaluated because the cached price history is shorter
// than its indicator lookback. It fires again for the same rule only
// after the rule has become ready in between, so a restart that empties
// the ring produces one warning per rule, not one per quote.
func (e *Engine) SetOnShortHistory(fn func(RuleStatus)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onShortHistory = fn
}

// Inject fires a triggered alert through onAlert + every registered
// notifier as if the engine had detected it. Bypasses rule evaluation
// and cooldown — intended for inbound webhooks (TradingView etc.).
// Notifier delivery is queued, so Inject does not block on a slow
// destination; the per-notifier rate limiter still applies.
func (e *Engine) Inject(a TriggeredAlert) {
	e.mu.RLock()
	onAlert := e.onAlert
	sinks := make([]*notifierSink, len(e.sinks))
	copy(sinks, e.sinks)
	e.mu.RUnlock()

	if onAlert != nil {
		onAlert(a)
	}
	for _, s := range sinks {
		s.send(a)
	}
}

// AddNotifier registers a destination that receives every triggered alert.
// Each notifier gets its own queue and delivery goroutine, so a slow
// destination delays only itself; errors are logged and never propagated.
func (e *Engine) AddNotifier(n Notifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sinks = append(e.sinks, newNotifierSink(e, n))
}

// Flush waits for every queued notification to be delivered or dropped,
// up to timeout, and reports whether the queues drained. Delivery is
// asynchronous, so a shutdown path (or a test) that needs notifications
// to have landed calls this first.
func (e *Engine) Flush(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if e.inflight.Load() == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// NotifyDrops returns the number of notifications the engine discarded,
// either because a notifier's queue was full or because the per-notifier
// rate limit was exceeded. Monotonic.
func (e *Engine) NotifyDrops() uint64 { return e.notifyDrops.Load() }

// SetRules replaces all rules. Per-rule state (cooldown, edge baseline,
// compound progress) is keyed by rule content, so rules that survive the
// replacement keep theirs; state belonging to rules that are gone is
// dropped.
func (e *Engine) SetRules(rules []Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
	e.pruneStateLocked()
}

// Rules returns a copy of current rules.
func (e *Engine) Rules() []Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rulesCopyLocked()
}

// Statuses reports, in Rules order, whether each rule has enough cached
// price history to be evaluated. Rules with no indicator condition are
// always ready.
func (e *Engine) Statuses() []RuleStatus {
	e.mu.RLock()
	rules := e.rulesCopyLocked()
	ps := e.prices
	e.mu.RUnlock()

	out := make([]RuleStatus, len(rules))
	for i, r := range rules {
		var have int
		if ps != nil && ruleHistory(r) > 0 {
			have = len(ps.Prices(r.Symbol))
		}
		out[i] = newRuleStatus(i, r, have, ps != nil)
	}
	return out
}

// AddRule adds a new alert rule.
func (e *Engine) AddRule(r Rule) {
	e.mu.Lock()
	e.rules = append(e.rules, r)
	fn, rules := e.onRulesChanged, e.rulesCopyLocked()
	e.mu.Unlock()
	if fn != nil {
		fn(rules)
	}
}

// RemoveRule removes a rule by index.
func (e *Engine) RemoveRule(idx int) {
	e.mu.Lock()
	if idx < 0 || idx >= len(e.rules) {
		e.mu.Unlock()
		return
	}
	e.rules = append(e.rules[:idx], e.rules[idx+1:]...)
	e.pruneStateLocked()
	fn, rules := e.onRulesChanged, e.rulesCopyLocked()
	e.mu.Unlock()
	if fn != nil {
		fn(rules)
	}
}

// ToggleRule toggles a rule's enabled state.
func (e *Engine) ToggleRule(idx int) {
	e.mu.Lock()
	if idx < 0 || idx >= len(e.rules) {
		e.mu.Unlock()
		return
	}
	e.rules[idx].Enabled = !e.rules[idx].Enabled
	fn, rules := e.onRulesChanged, e.rulesCopyLocked()
	e.mu.Unlock()
	if fn != nil {
		fn(rules)
	}
}

// Check evaluates all rules against a quote.
//
// Evaluation happens under the lock; alerts are dispatched after release
// and notifier delivery is queued, so neither a slow callback nor a slow
// webhook can stall the goroutine feeding quotes in.
//
// Time comes from the quote itself when it carries a timestamp, and from
// the injected clock otherwise. Cooldowns and TriggeredAlert.Timestamp
// both use that event time, which is what makes a burst replay of a
// recording behave like the hours it represents instead of collapsing
// every rule to a single fire at wall-clock now.
func (e *Engine) Check(q provider.Quote) {
	e.mu.Lock()

	if b, ok := sessionBaseline(q); ok {
		e.sessionRefs[q.Symbol] = b
	}
	if _, ok := e.refPrices[q.Symbol]; !ok {
		e.refPrices[q.Symbol] = q.Price
	}
	base := e.baselineLocked(q.Symbol)

	now := e.now()
	if !q.Timestamp.IsZero() {
		now = q.Timestamp
	}

	// Price history is fetched at most once per Check — every rule in the
	// loop is scoped to this quote's symbol.
	var prices []float64
	if e.prices != nil {
		prices = e.prices.Prices(q.Symbol)
	}

	var triggered []TriggeredAlert
	var warnings []RuleStatus
	for i, r := range e.rules {
		if !r.Enabled || r.Symbol != q.Symbol {
			continue
		}

		key := ruleKey(r)

		// Readiness is checked before evaluation so an indicator rule that
		// cannot produce a value is reported rather than silently false.
		if st := newRuleStatus(i, r, len(prices), e.prices != nil); !st.Ready {
			if !e.warnedHistory[key] {
				e.warnedHistory[key] = true
				warnings = append(warnings, st)
			}
			continue
		}
		delete(e.warnedHistory, key)

		var fires bool
		var msg string

		switch {
		case r.IsCompound():
			fires, msg = e.evaluateCompound(r, key, q, base, prices)
		case IsIndicatorCondition(r.Condition):
			fires, msg = evaluateIndicator(r, prices)
		default:
			fires, msg = evaluate(r, q, base)
		}

		// Edge detection runs even while the rule is in cooldown so the
		// baseline never goes stale; the cooldown gate below decides
		// whether the transition is actually delivered.
		if isEdgeGated(r) {
			prev, seen := e.levelState[key]
			e.levelState[key] = fires
			if !seen || prev {
				continue
			}
		}

		if !fires {
			continue
		}
		if next, ok := e.cooldowns[key]; ok && now.Before(next) {
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
	onShort := e.onShortHistory
	sinks := make([]*notifierSink, len(e.sinks))
	copy(sinks, e.sinks)
	e.mu.Unlock()

	if onShort != nil {
		for _, st := range warnings {
			onShort(st)
		}
	}
	for _, a := range triggered {
		if onAlert != nil {
			onAlert(a)
		}
		for _, s := range sinks {
			s.send(a)
		}
	}
}

// rulesCopyLocked returns a defensive copy of the rule slice. Caller
// holds the lock.
func (e *Engine) rulesCopyLocked() []Rule {
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// pruneStateLocked drops per-rule state for rules that are no longer
// configured. Keys are derived from rule content, so a rule that survives
// a delete or a reorder keeps its cooldown, its edge baseline and its
// compound progress. Caller holds the lock.
func (e *Engine) pruneStateLocked() {
	live := make(map[string]struct{}, len(e.rules))
	for _, r := range e.rules {
		live[ruleKey(r)] = struct{}{}
	}
	for k := range e.cooldowns {
		if _, ok := live[k]; !ok {
			delete(e.cooldowns, k)
		}
	}
	for k := range e.levelState {
		if _, ok := live[k]; !ok {
			delete(e.levelState, k)
		}
	}
	for k := range e.compoundState {
		if _, ok := live[k]; !ok {
			delete(e.compoundState, k)
		}
	}
	for k := range e.warnedHistory {
		if _, ok := live[k]; !ok {
			delete(e.warnedHistory, k)
		}
	}
}

// baseline is the reference price pct_up / pct_down measure against.
type baseline struct {
	price   float64
	session bool // true when derived from the quote's own previous close
}

// label describes where the baseline came from, for the alert message.
func (b baseline) label() string {
	if b.session {
		return "previous close"
	}
	return "first seen"
}

// baselineLocked resolves the pct baseline for a symbol, preferring the
// previous close carried by the quote over the first price this process
// happened to see. Caller holds the lock.
func (e *Engine) baselineLocked(sym string) baseline {
	if v, ok := e.sessionRefs[sym]; ok && v > 0 {
		return baseline{price: v, session: true}
	}
	return baseline{price: e.refPrices[sym]}
}

// sessionBaseline recovers the previous session close from a quote.
// Providers report Change and ChangePct relative to that close, so it is
// derivable without a second fetch. Reports false when the quote carries
// no session context, in which case pct rules fall back to the first
// price seen this process.
func sessionBaseline(q provider.Quote) (float64, bool) {
	if q.Price <= 0 {
		return 0, false
	}
	if q.Change != 0 {
		if prev := q.Price - q.Change; prev > 0 {
			return prev, true
		}
	}
	if q.ChangePct != 0 {
		if d := 1 + q.ChangePct/100; d > 0 {
			if prev := q.Price / d; prev > 0 && !math.IsInf(prev, 0) {
				return prev, true
			}
		}
	}
	return 0, false
}

// isEdgeGated reports whether a rule's outcome is a level test that must
// be converted into an edge. Cross conditions already compare adjacent
// samples. Compound rules carry their own progress state, which is the
// memory an edge would otherwise provide — except for "any", which is a
// plain OR of level tests and needs the same treatment as a simple rule.
func isEdgeGated(r Rule) bool {
	if r.IsCompound() {
		return r.Match == MatchAny
	}
	return isLevelCondition(r.Condition)
}

// isLevelCondition reports whether a condition tests the current value
// against a threshold (as opposed to detecting a crossing between two
// adjacent samples).
func isLevelCondition(c Condition) bool {
	switch c {
	case CondAbove, CondBelow, CondPctUp, CondPctDown,
		CondVolumeAbove, CondRSIAbove, CondRSIBelow, CondStddevAbove:
		return true
	}
	return false
}

func evaluate(r Rule, q provider.Quote, base baseline) (bool, string) {
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
		if base.price > 0 {
			pct := ((price - base.price) / base.price) * 100
			if pct >= r.Value {
				return true, fmt.Sprintf("%s up %.1f%% since %s (from %.4f to %.4f)", r.Symbol, pct, base.label(), base.price, price)
			}
		}
	case CondPctDown:
		if base.price > 0 {
			pct := ((base.price - price) / base.price) * 100
			if pct >= r.Value {
				return true, fmt.Sprintf("%s down %.1f%% since %s (from %.4f to %.4f)", r.Symbol, pct, base.label(), base.price, price)
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
			period = defaultRSIPeriod
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
			period = defaultSMAPeriod
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
		if len(prices) < macdMinSamples {
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
			period = defaultStddevPeriod
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

// requiredHistory returns the number of price samples a condition needs
// before it can produce any value at all. Non-indicator conditions need
// none and return 0.
func requiredHistory(c Condition, period int) int {
	switch c {
	case CondRSIAbove, CondRSIBelow:
		if period <= 0 {
			period = defaultRSIPeriod
		}
		return period + 1
	case CondSMACrossAbove, CondSMACrossBelow:
		if period <= 0 {
			period = defaultSMAPeriod
		}
		return period + 1
	case CondMACDCross:
		return macdMinSamples
	case CondStddevAbove:
		if period <= 1 {
			period = defaultStddevPeriod
		}
		return period
	}
	return 0
}

// ruleHistory returns the deepest lookback any of a rule's conditions
// needs. A compound rule is as demanding as its hungriest sub-condition.
func ruleHistory(r Rule) int {
	if !r.IsCompound() {
		return requiredHistory(r.Condition, r.Period)
	}
	var need int
	for _, s := range r.Conditions {
		if n := requiredHistory(s.Type, s.Period); n > need {
			need = n
		}
	}
	return need
}

// newRuleStatus reports whether a rule with have cached samples can be
// evaluated. hasSource distinguishes "the ring is empty" from "no price
// source is wired up at all".
func newRuleStatus(idx int, r Rule, have int, hasSource bool) RuleStatus {
	st := RuleStatus{Index: idx, Rule: r, Need: ruleHistory(r), Ready: true}
	if st.Need == 0 {
		return st
	}
	if !hasSource {
		st.Ready = false
		st.Reason = "no price history source configured"
		return st
	}
	st.Have = have
	if st.Have < st.Need {
		st.Ready = false
		st.Reason = fmt.Sprintf("needs %d price samples, have %d", st.Need, st.Have)
	}
	return st
}

// ruleKey derives a stable identity from a rule's content. Cooldowns,
// edge baselines and compound progress hang off it, so deleting or
// reordering rules can never hand one rule's state to another — which is
// exactly what keying by slice index did.
func ruleKey(r Rule) string {
	var b strings.Builder
	b.WriteString(r.Symbol)
	b.WriteByte('|')
	b.WriteString(string(r.Condition))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(r.Value, 'g', -1, 64))
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(r.Period))
	b.WriteByte('|')
	b.WriteString(r.Match)
	for _, s := range r.Conditions {
		b.WriteByte(';')
		b.WriteString(string(s.Type))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(s.Value, 'g', -1, 64))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(s.Period))
	}
	return b.String()
}

// evaluateCompound evaluates a compound rule against the latest quote.
// Caller holds the engine lock. May mutate compoundState.
//
// Sub-conditions stay level tests: for "all" and "sequence" the progress
// latch is the memory an edge would provide, and the rule fires on the
// transition that completes it. "any" has no latch, so the engine
// edge-gates its result the same way it gates a simple rule.
func (e *Engine) evaluateCompound(r Rule, key string, q provider.Quote, base baseline, prices []float64) (bool, string) {
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
	evalSub := func(s SubCondition) (bool, string) {
		tmp := Rule{Symbol: r.Symbol, Condition: s.Type, Value: s.Value, Period: s.Period}
		if IsIndicatorCondition(s.Type) {
			return evaluateIndicator(tmp, prices)
		}
		return evaluate(tmp, q, base)
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

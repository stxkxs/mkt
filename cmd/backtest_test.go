package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/provider"
)

// ─────────────────────────────── test harness ───────────────────────────────

// tapeQuote is one line of the recording wire format. The recording package
// keeps its wire struct unexported, so the tape is written directly here —
// which also keeps this test honest about the on-disk schema rather than
// round-tripping through the writer that produced it.
type tapeQuote struct {
	V         int     `json:"v"`
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Volume    float64 `json:"volume,omitempty"`
	Asset     int     `json:"asset"`
	Provider  string  `json:"provider,omitempty"`
	Timestamp int64   `json:"ts"`
}

// writeTape renders quotes as an NDJSON recording and returns its path.
func writeTape(t *testing.T, quotes []provider.Quote) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tape.ndjson")
	var buf bytes.Buffer
	for _, q := range quotes {
		// The wire format has no encoding for "absent", so an undated quote
		// is written as a zero ts — exactly what a tape recorded before
		// quotes were stamped contains.
		var ts int64
		if !q.Timestamp.IsZero() {
			ts = q.Timestamp.UnixNano()
		}
		line, err := json.Marshal(tapeQuote{
			V:         1,
			Symbol:    q.Symbol,
			Price:     q.Price,
			Volume:    q.Volume,
			Asset:     int(q.Asset),
			Provider:  "test",
			Timestamp: ts,
		})
		if err != nil {
			t.Fatalf("marshal tape line: %v", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write tape: %v", err)
	}
	return path
}

// tapeStart anchors the synthetic recordings at a fixed instant so the
// reported timestamps are deterministic.
var tapeStart = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

// crossingTape is the auditor's scenario: eight quotes spanning seven
// recorded hours, oscillating either side of 100 so that a "above 100" rule
// crosses upward exactly four times. One quote per hour is far wider than
// the five-minute cooldown, so in recorded time every crossing must be
// delivered.
func crossingTape(t *testing.T) string {
	t.Helper()
	prices := []float64{90, 110, 90, 120, 95, 130, 80, 140}
	quotes := make([]provider.Quote, 0, len(prices))
	for i, p := range prices {
		quotes = append(quotes, provider.Quote{
			Symbol:    "BTC-USD",
			Price:     p,
			Asset:     provider.AssetCrypto,
			Timestamp: tapeStart.Add(time.Duration(i) * time.Hour),
		})
	}
	return writeTape(t, quotes)
}

// aboveRule is a plain level rule on the crossing tape's symbol.
func aboveRule(value float64) alert.Rule {
	return alert.Rule{Symbol: "BTC-USD", Condition: alert.CondAbove, Value: value, Enabled: true}
}

// ────────────────────────────── recorded time ──────────────────────────────

// TestBacktestCountsEveryCrossingInRecordedTime is the regression test for
// the headline backtest bug: with a wall-clock cooldown the entire replay
// happened inside one cooldown window, so a tape with four crossings
// reported exactly one fire. Driving the engine's clock from the tape makes
// the count the recording actually contains.
func TestBacktestCountsEveryCrossingInRecordedTime(t *testing.T) {
	tape := crossingTape(t)

	var out, errOut bytes.Buffer
	if err := runBacktest(context.Background(), []alert.Rule{aboveRule(100)}, tape, 5*time.Minute, &out, &errOut); err != nil {
		t.Fatalf("runBacktest: %v", err)
	}

	if got := fireCount(t, out.String()); got != 4 {
		t.Errorf("fires = %d, want 4 (one per recorded crossing)\n%s", got, out.String())
	}

	// Timestamps must come from the tape, not from the wall clock at replay.
	// The replay reconstructs instants in the local zone, so the expected
	// strings are rendered the same way rather than hard-coded as UTC.
	firstCross := tapeStart.Add(1 * time.Hour).Local().Format(time.RFC3339)
	lastCross := tapeStart.Add(7 * time.Hour).Local().Format(time.RFC3339)
	if !strings.Contains(out.String(), "first="+firstCross) {
		t.Errorf("first fire is not the recorded crossing time %s:\n%s", firstCross, out.String())
	}
	if !strings.Contains(out.String(), "last="+lastCross) {
		t.Errorf("last fire is not the recorded crossing time %s:\n%s", lastCross, out.String())
	}
	if !strings.Contains(errOut.String(), "(7h0m0s), cooldown 5m0s in recorded time") {
		t.Errorf("recorded span not reported:\n%s", errOut.String())
	}
}

// TestBacktestCooldownIsMeasuredInRecordedTime pins the other half of the
// same fix: a cooldown wider than the recorded span really does suppress the
// later crossings. If the cooldown were wall-clock this would pass for the
// wrong reason, so the test above (which must count four) is its pair.
func TestBacktestCooldownIsMeasuredInRecordedTime(t *testing.T) {
	tape := crossingTape(t)

	var out, errOut bytes.Buffer
	if err := runBacktest(context.Background(), []alert.Rule{aboveRule(100)}, tape, 24*time.Hour, &out, &errOut); err != nil {
		t.Fatalf("runBacktest: %v", err)
	}
	if got := fireCount(t, out.String()); got != 1 {
		t.Errorf("fires = %d, want 1 (a 24h cooldown covers the whole 7h tape)\n%s", got, out.String())
	}
}

// TestBacktestUndatedQuotesInheritTheLastRecordedTime covers a tape written
// before quotes carried timestamps. Those quotes must inherit the last real
// timestamp seen rather than jumping to wall-clock now, which would expire
// every cooldown at once and reopen the bug on exactly the recordings that
// cannot defend themselves.
func TestBacktestUndatedQuotesInheritTheLastRecordedTime(t *testing.T) {
	quotes := []provider.Quote{
		{Symbol: "BTC-USD", Price: 90, Timestamp: tapeStart},
		{Symbol: "BTC-USD", Price: 110, Timestamp: tapeStart.Add(time.Hour)},
		{Symbol: "BTC-USD", Price: 90},  // undated
		{Symbol: "BTC-USD", Price: 130}, // undated
	}
	tape := writeTape(t, quotes)

	var out, errOut bytes.Buffer
	if err := runBacktest(context.Background(), []alert.Rule{aboveRule(100)}, tape, time.Hour, &out, &errOut); err != nil {
		t.Fatalf("runBacktest: %v", err)
	}
	if !strings.Contains(errOut.String(), "2 quote(s) carried no timestamp") {
		t.Errorf("undated quotes not reported:\n%s", errOut.String())
	}
	// The two undated quotes sit at the last recorded time (10:00), so the
	// second crossing is inside the one-hour cooldown and only one fire is
	// reported — the honest answer for a tape with no time information.
	if got := fireCount(t, out.String()); got != 1 {
		t.Errorf("fires = %d, want 1\n%s", got, out.String())
	}
}

// TestRecordedTimeTreatsTheEpochAsAbsent pins the guard that makes the
// undated path reachable at all. The wire format stores the timestamp as
// unix nanoseconds with no encoding for "missing", so an omitted field
// decodes to the Unix epoch — which, taken literally, reports a fifty-year
// recorded span and anchors every cooldown in 1970.
func TestRecordedTimeTreatsTheEpochAsAbsent(t *testing.T) {
	tests := []struct {
		name string
		ts   time.Time
		want bool
	}{
		{name: "zero time", ts: time.Time{}, want: false},
		{name: "unix epoch", ts: time.Unix(0, 0), want: false},
		{name: "before the epoch", ts: time.Unix(-1, 0), want: false},
		{name: "one nanosecond after the epoch", ts: time.Unix(0, 1), want: true},
		{name: "a real recording", ts: tapeStart, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := recordedTime(provider.Quote{Timestamp: tt.ts})
			if ok != tt.want {
				t.Fatalf("recordedTime ok = %v, want %v", ok, tt.want)
			}
			if ok && !got.Equal(tt.ts) {
				t.Errorf("recordedTime = %v, want %v", got, tt.ts)
			}
		})
	}
}

// ──────────────────────────── stats attribution ────────────────────────────

// TestBacktestAttributesFiresPerRuleNotPerConditionTuple is the regression
// test for the second backtest bug. Fires used to be tallied by
// (Symbol, Condition, Value); every compound rule has an empty Condition and
// a zero Value, so several compounds on one symbol collapsed into a single
// bucket and the report credited all of them to whichever came first.
func TestBacktestAttributesFiresPerRuleNotPerConditionTuple(t *testing.T) {
	tape := crossingTape(t)

	// Two distinct compound rules on the same symbol. Under the old key they
	// are indistinguishable: both are ("BTC-USD", "", 0).
	rules := []alert.Rule{
		{
			Symbol:     "BTC-USD",
			Match:      alert.MatchAll,
			Enabled:    true,
			Conditions: []alert.SubCondition{{Type: alert.CondAbove, Value: 125}},
		},
		{
			Symbol:     "BTC-USD",
			Match:      alert.MatchAll,
			Enabled:    true,
			Conditions: []alert.SubCondition{{Type: alert.CondAbove, Value: 105}},
		},
	}

	var out, errOut bytes.Buffer
	if err := runBacktest(context.Background(), rules, tape, 5*time.Minute, &out, &errOut); err != nil {
		t.Fatalf("runBacktest: %v", err)
	}

	// The report is sorted by fire count, so match on each rule's own line.
	above125 := ruleLine(t, out.String(), "above 125")
	above105 := ruleLine(t, out.String(), "above 105")
	if !strings.Contains(above125, "fires=2") {
		t.Errorf("above 125 rule: want fires=2, got %q", above125)
	}
	if !strings.Contains(above105, "fires=4") {
		t.Errorf("above 105 rule: want fires=4, got %q", above105)
	}
}

// TestRuleIdentityDistinguishesCompoundRules is the unit-level statement of
// the same thing: the key the report is built on must separate two compounds
// that differ only inside Conditions.
func TestRuleIdentityDistinguishesCompoundRules(t *testing.T) {
	a := alert.Rule{Symbol: "BTC-USD", Match: alert.MatchAll,
		Conditions: []alert.SubCondition{{Type: alert.CondAbove, Value: 125}}}
	b := alert.Rule{Symbol: "BTC-USD", Match: alert.MatchAll,
		Conditions: []alert.SubCondition{{Type: alert.CondAbove, Value: 105}}}

	if ruleIdentity(a) == ruleIdentity(b) {
		t.Fatalf("two different compound rules share an identity: %q", ruleIdentity(a))
	}
	if ruleIdentity(a) != ruleIdentity(a) {
		t.Error("ruleIdentity is not stable")
	}
}

// TestDescribeBacktestRuleNamesCompounds guards the report line for a
// compound rule, which used to render as a bare symbol and an empty column
// because it has no top-level Condition.
func TestDescribeBacktestRuleNamesCompounds(t *testing.T) {
	got := describeBacktestRule(alert.Rule{
		Symbol: "BTC-USD",
		Match:  alert.MatchAny,
		Conditions: []alert.SubCondition{
			{Type: alert.CondAbove, Value: 105},
			{Type: alert.CondRSIBelow, Value: 30},
		},
	})
	want := "BTC-USD any(above 105, rsi_below 30)"
	if got != want {
		t.Errorf("describeBacktestRule = %q, want %q", got, want)
	}

	// A compound with no explicit match defaults to "all".
	got = describeBacktestRule(alert.Rule{
		Symbol:     "AAPL",
		Conditions: []alert.SubCondition{{Type: alert.CondAbove, Value: 400}},
	})
	if want := "AAPL all(above 400)"; got != want {
		t.Errorf("describeBacktestRule = %q, want %q", got, want)
	}
}

// TestBacktestReportsRulesThatNeverFired keeps the zero-fire rows in the
// report: "this rule never fired" is a result, and dropping the row would
// look like the rule was not loaded.
func TestBacktestReportsRulesThatNeverFired(t *testing.T) {
	tape := crossingTape(t)

	var out, errOut bytes.Buffer
	if err := runBacktest(context.Background(), []alert.Rule{aboveRule(1000)}, tape, 5*time.Minute, &out, &errOut); err != nil {
		t.Fatalf("runBacktest: %v", err)
	}
	if !strings.Contains(out.String(), "(no fires)") {
		t.Errorf("a rule that never fired is missing from the report:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "replayed 8 quotes against 1 rule(s)") {
		t.Errorf("replay summary missing:\n%s", errOut.String())
	}
}

// TestBacktestMissingRecordingIsAnError checks the failure path names the
// file rather than reporting an empty, healthy-looking replay.
func TestBacktestMissingRecordingIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.ndjson")
	var out, errOut bytes.Buffer
	err := runBacktest(context.Background(), []alert.Rule{aboveRule(100)}, missing, time.Minute, &out, &errOut)
	if err == nil {
		t.Fatal("expected an error for a missing recording")
	}
	if !strings.Contains(err.Error(), "nope.ndjson") {
		t.Errorf("error does not name the recording: %v", err)
	}
}

// ───────────────────────────── rules file loading ─────────────────────────────

// TestLoadBacktestRulesEnablesEveryRule pins the deliberate override: a
// backtest is asking "what would these rules have done", so a rule disabled
// in the file still participates.
func TestLoadBacktestRulesEnablesEveryRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.yaml")
	body := `alerts:
  - symbol: BTC-USD
    condition: above
    value: 100
    enabled: false
  - symbol: AAPL
    match: all
    conditions:
      - condition: above
        value: 400
      - condition: rsi_below
        value: 30
        period: 14
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	rules, err := loadBacktestRules(path)
	if err != nil {
		t.Fatalf("loadBacktestRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("loaded %d rules, want 2", len(rules))
	}
	for i, r := range rules {
		if !r.Enabled {
			t.Errorf("rules[%d] is disabled; a backtest must evaluate every rule", i)
		}
	}
	if !rules[1].IsCompound() {
		t.Error("rules[1] should be compound")
	}
	if rules[1].Conditions[1].Period != 14 {
		t.Errorf("sub-condition period = %d, want 14", rules[1].Conditions[1].Period)
	}
}

// ──────────────────────────────── helpers ────────────────────────────────

// fireCount extracts the fires= count from a single-rule report.
func fireCount(t *testing.T, report string) int {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if i := strings.Index(line, "fires="); i >= 0 {
			rest := line[i+len("fires="):]
			end := strings.IndexByte(rest, ' ')
			if end < 0 {
				end = len(rest)
			}
			n, err := strconv.Atoi(rest[:end])
			if err != nil {
				t.Fatalf("parse fires from %q: %v", line, err)
			}
			return n
		}
	}
	return 0
}

// ruleLine returns the report line describing the rule containing needle.
func ruleLine(t *testing.T, report, needle string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no report line mentions %q:\n%s", needle, report)
	return ""
}

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/recording"
	"gopkg.in/yaml.v3"
)

// backtestCacheSize is the ring depth the replay pushes quotes into. It
// bounds the history indicator conditions can see, so it must be at least
// the longest lookback a rule can ask for.
const backtestCacheSize = 256

type backtestRulesFile struct {
	Alerts []ruleEntry `yaml:"alerts"`
}

type ruleEntry struct {
	Symbol     string             `yaml:"symbol"`
	Condition  string             `yaml:"condition,omitempty"`
	Value      float64            `yaml:"value,omitempty"`
	Period     int                `yaml:"period,omitempty"`
	Enabled    bool               `yaml:"enabled"`
	Conditions []subConditionYAML `yaml:"conditions,omitempty"`
	Match      string             `yaml:"match,omitempty"`
}

type subConditionYAML struct {
	Condition string  `yaml:"condition"`
	Value     float64 `yaml:"value"`
	Period    int     `yaml:"period,omitempty"`
}

// ruleStat is one rule's outcome over a replay.
type ruleStat struct {
	count     int
	firstFire time.Time
	lastFire  time.Time
}

func init() {
	backtestCmd := &cobra.Command{
		Use:   "backtest [rules.yaml] [replay.ndjson]",
		Short: "Replay a recorded quote stream against alert rules and report fire counts",
		Long: `Loads alert rules from a YAML file (same schema as config.alerts) and
replays the recorded quote stream (produced by MKT_RECORD) through the
alert engine. Reports the number of times each rule fires, plus first
and last trigger times. No notifiers fire — this is read-only analysis.

The replay runs in recorded time, not wall time: the engine's clock is
driven from each quote's own timestamp, so a rule's cooldown expires
after the recorded interval it represents rather than after the
milliseconds a burst replay actually takes. Replaying seven hours of
quotes therefore reports the fires those seven hours would have
produced.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rulesPath, replayPath := args[0], args[1]

			rules, err := loadBacktestRules(rulesPath)
			if err != nil {
				return fmt.Errorf("load rules: %w", err)
			}
			cooldown, _ := cmd.Flags().GetDuration("cooldown")
			return runBacktest(cmd.Context(), rules, replayPath, cooldown, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	backtestCmd.Flags().Duration("cooldown", 5*time.Minute, "per-rule cooldown, measured in recorded time")
	rootCmd.AddCommand(backtestCmd)
}

// runBacktest replays the recording at replayPath through the alert engine
// and writes the per-rule report.
//
// Two things make the numbers real. The engine's clock is advanced to each
// quote's recorded timestamp before that quote is checked, so cooldowns and
// TriggeredAlert timestamps live in recorded time — a burst replay of seven
// recorded hours behaves like seven hours, not like the 3ms it takes. And
// fires are attributed by rule index rather than by (symbol, condition,
// value), which every compound rule shares — they all have an empty
// Condition, so a file with several compounds used to pile every fire onto
// whichever one happened to be first in the slice.
func runBacktest(ctx context.Context, rules []alert.Rule, replayPath string, cooldown time.Duration, out, errOut io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cache := market.NewCache(backtestCacheSize)
	stats := make([]ruleStat, len(rules))

	// The engine reports the rule that fired, not its position, so map each
	// rule's identity back to its index. Identity is content-based and
	// matches how the engine itself keys per-rule state, so two byte-identical
	// rules are one rule to both of us — they already share a cooldown.
	index := make(map[string]int, len(rules))
	for i, r := range rules {
		if _, dup := index[ruleIdentity(r)]; !dup {
			index[ruleIdentity(r)] = i
		}
	}

	engine := alert.NewEngine(cooldown, func(a alert.TriggeredAlert) {
		i, ok := index[ruleIdentity(a.Rule)]
		if !ok {
			return
		}
		if stats[i].count == 0 {
			stats[i].firstFire = a.Timestamp
		}
		stats[i].lastFire = a.Timestamp
		stats[i].count++
	})
	engine.SetRules(rules)
	engine.SetPriceSource(cache)

	rep := recording.NewReplay(replayPath, recording.ModeBurst)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	quotes := make(chan provider.Quote, 256)
	done := make(chan struct{})
	var subErr error
	go func() {
		defer close(done)
		subErr = rep.Subscribe(ctx, nil, quotes)
		close(quotes)
	}()

	var (
		total       int
		undated     int
		first, last time.Time
		clock       time.Time
	)
	for q := range quotes {
		// Drive the engine's clock from the tape. Quotes carry their own
		// timestamp and Check prefers it, but a recording made before
		// timestamps were stamped (or a synthetic tape) has none — those
		// then inherit the last real timestamp seen rather than jumping to
		// wall-clock now, which would expire every cooldown at once.
		if ts, ok := recordedTime(q); ok {
			clock = ts
			if first.IsZero() || ts.Before(first) {
				first = ts
			}
			if ts.After(last) {
				last = ts
			}
		} else {
			undated++
			// Check prefers the quote's own timestamp over the injected
			// clock, so an unusable one has to be cleared rather than left
			// for the engine to trust.
			q.Timestamp = time.Time{}
		}
		if !clock.IsZero() {
			at := clock
			engine.SetClock(func() time.Time { return at })
		}

		cache.Push(q)
		engine.Check(q)
		total++
	}
	<-done
	if subErr != nil {
		return fmt.Errorf("replay %s: %w", replayPath, subErr)
	}

	fmt.Fprintf(errOut, "backtest: replayed %d quotes against %d rule(s)\n", total, len(rules))
	if !first.IsZero() {
		fmt.Fprintf(errOut, "backtest: recorded span %s → %s (%s), cooldown %s in recorded time\n",
			first.Format(time.RFC3339), last.Format(time.RFC3339), last.Sub(first), cooldown)
	}
	if undated > 0 {
		fmt.Fprintf(errOut, "backtest: %d quote(s) carried no timestamp\n", undated)
	}

	fmt.Fprintln(out)
	order := make([]int, len(rules))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return stats[order[a]].count > stats[order[b]].count
	})

	for _, i := range order {
		s := stats[i]
		desc := describeBacktestRule(rules[i])
		if s.count == 0 {
			fmt.Fprintf(out, "  %-40s  (no fires)\n", desc)
			continue
		}
		fmt.Fprintf(out, "  %-40s  fires=%d  first=%s  last=%s\n",
			desc, s.count,
			s.firstFire.Format(time.RFC3339),
			s.lastFire.Format(time.RFC3339))
	}
	return nil
}

// recordedTime returns the instant a quote was recorded at, and whether it
// carries a usable one.
//
// The recording wire format stores the timestamp as unix nanoseconds with no
// encoding for "absent", so a line written before quotes were stamped — or a
// hand-written tape that omits the field — decodes to the Unix epoch rather
// than to the zero time. Taken at face value that anchors the whole replay in
// 1970: the reported span becomes half a century, and every cooldown expires
// against a clock fifty years behind the rest of the tape. Anything at or
// before the epoch is therefore treated as no timestamp at all, which is what
// it is.
func recordedTime(q provider.Quote) (time.Time, bool) {
	if q.Timestamp.IsZero() || !q.Timestamp.After(time.Unix(0, 0)) {
		return time.Time{}, false
	}
	return q.Timestamp, true
}

// ruleIdentity is a rule's content-addressed key, mirroring how the alert
// engine keys its own per-rule state. It is what maps a TriggeredAlert back
// to the rule's position in the file.
func ruleIdentity(r alert.Rule) string {
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

// describeBacktestRule renders a rule as one readable line. Compound rules
// have no top-level condition at all, so printing Condition alone left them
// as a bare symbol and an empty column.
func describeBacktestRule(r alert.Rule) string {
	if r.IsCompound() {
		match := r.Match
		if match == "" {
			match = alert.MatchAll
		}
		parts := make([]string, 0, len(r.Conditions))
		for _, c := range r.Conditions {
			parts = append(parts, fmt.Sprintf("%s %s", c.Type, strconv.FormatFloat(c.Value, 'g', -1, 64)))
		}
		return fmt.Sprintf("%s %s(%s)", r.Symbol, match, strings.Join(parts, ", "))
	}
	if r.Period > 0 && r.Value == 0 {
		return fmt.Sprintf("%s %s %d", r.Symbol, r.Condition, r.Period)
	}
	return fmt.Sprintf("%s %s %s", r.Symbol, r.Condition, strconv.FormatFloat(r.Value, 'g', -1, 64))
}

func loadBacktestRules(path string) ([]alert.Rule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f backtestRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	out := make([]alert.Rule, 0, len(f.Alerts))
	for _, r := range f.Alerts {
		var subs []alert.SubCondition
		for _, s := range r.Conditions {
			subs = append(subs, alert.SubCondition{
				Type: alert.Condition(s.Condition), Value: s.Value, Period: s.Period,
			})
		}
		out = append(out, alert.Rule{
			Symbol:     r.Symbol,
			Condition:  alert.Condition(r.Condition),
			Value:      r.Value,
			Period:     r.Period,
			Enabled:    true, // backtests assume enabled regardless of the YAML default
			Conditions: subs,
			Match:      r.Match,
		})
	}
	return out, nil
}

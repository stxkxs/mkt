package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/recording"
	"gopkg.in/yaml.v3"
)

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

func init() {
	backtestCmd := &cobra.Command{
		Use:   "backtest [rules.yaml] [replay.ndjson]",
		Short: "Replay a recorded quote stream against alert rules and report fire counts",
		Long: `Loads alert rules from a YAML file (same schema as config.alerts) and
replays the recorded quote stream (produced by MKT_RECORD) through the
alert engine. Reports the number of times each rule fires, plus first
and last trigger times. No notifiers fire — this is read-only analysis.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rulesPath, replayPath := args[0], args[1]

			rules, err := loadBacktestRules(rulesPath)
			if err != nil {
				return fmt.Errorf("load rules: %w", err)
			}

			cache := market.NewCache(256)

			type stat struct {
				count     int
				firstFire time.Time
				lastFire  time.Time
			}
			stats := make([]stat, len(rules))

			engine := alert.NewEngine(5*time.Minute, func(a alert.TriggeredAlert) {
				for i, r := range rules {
					if r.Symbol == a.Rule.Symbol && r.Condition == a.Rule.Condition && r.Value == a.Rule.Value {
						if stats[i].count == 0 {
							stats[i].firstFire = a.Timestamp
						}
						stats[i].lastFire = a.Timestamp
						stats[i].count++
						break
					}
				}
			})
			engine.SetRules(rules)
			engine.SetPriceSource(cache)

			rep := recording.NewReplay(replayPath, recording.ModeBurst)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := make(chan provider.Quote, 256)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = rep.Subscribe(ctx, nil, out)
				close(out)
			}()

			var total int
			for q := range out {
				cache.Push(q)
				engine.Check(q)
				total++
			}
			<-done

			fmt.Fprintf(os.Stderr, "backtest: replayed %d quotes against %d rule(s)\n", total, len(rules))
			fmt.Println()
			type row struct {
				idx int
				stat
			}
			var rows []row
			for i := range stats {
				rows = append(rows, row{idx: i, stat: stats[i]})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })

			for _, r := range rows {
				rule := rules[r.idx]
				if r.count == 0 {
					fmt.Printf("  %-12s %-18s %-10v  (no fires)\n", rule.Symbol, rule.Condition, rule.Value)
					continue
				}
				fmt.Printf("  %-12s %-18s %-10v  fires=%d  first=%s  last=%s\n",
					rule.Symbol, rule.Condition, rule.Value, r.count,
					r.firstFire.Format(time.RFC3339), r.lastFire.Format(time.RFC3339))
			}
			return nil
		},
	}
	rootCmd.AddCommand(backtestCmd)
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

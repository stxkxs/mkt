package cmd

import (
	"fmt"
	"time"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// validateConfig checks cfg for values that would otherwise silently fall
// back to defaults or be ignored at runtime (Load never rejects them).
// Returns one human-readable message per problem.
func validateConfig(cfg *config.Config) []string {
	var issues []string

	if d, err := time.ParseDuration(cfg.PollInterval); err != nil {
		issues = append(issues, fmt.Sprintf("poll_interval: %q is not a valid duration (e.g. 15s, 1m)", cfg.PollInterval))
	} else if d <= 0 {
		issues = append(issues, fmt.Sprintf("poll_interval: %q must be positive", cfg.PollInterval))
	}

	if cfg.SparklineLen <= 0 {
		issues = append(issues, fmt.Sprintf("sparkline_len: %d must be positive", cfg.SparklineLen))
	}

	if cfg.Theme != "" && !validTheme(cfg.Theme) {
		issues = append(issues, fmt.Sprintf("theme: %q is not a known theme (valid: %v)", cfg.Theme, theme.ThemeNames))
	}

	validCond := make(map[string]bool)
	for _, c := range alert.AllConditions() {
		validCond[string(c)] = true
	}
	for i, r := range cfg.Alerts {
		loc := fmt.Sprintf("alerts[%d] (%s)", i, r.Symbol)
		if r.Symbol == "" {
			issues = append(issues, fmt.Sprintf("alerts[%d]: symbol is required", i))
		}
		if len(r.Conditions) > 0 {
			switch r.Match {
			case "", alert.MatchAll, alert.MatchAny, alert.MatchSequence:
			default:
				issues = append(issues, fmt.Sprintf("%s: match %q is not one of all, any, sequence", loc, r.Match))
			}
			for j, sc := range r.Conditions {
				if !validCond[sc.Condition] {
					issues = append(issues, fmt.Sprintf("%s: conditions[%d] condition %q is not a known condition", loc, j, sc.Condition))
				}
			}
		} else if !validCond[r.Condition] {
			issues = append(issues, fmt.Sprintf("%s: condition %q is not a known condition", loc, r.Condition))
		}
	}

	for i, p := range cfg.Portfolios {
		loc := fmt.Sprintf("portfolios[%d] (%s)", i, p.Name)
		switch p.TaxMethod {
		case "", "fifo", "lifo", "hifo":
		default:
			issues = append(issues, fmt.Sprintf("%s: tax_method %q is not one of fifo, lifo, hifo (empty = weighted average)", loc, p.TaxMethod))
		}
		for j, h := range p.Holdings {
			if h.Symbol == "" {
				issues = append(issues, fmt.Sprintf("%s: holdings[%d]: symbol is required", loc, j))
			}
			if h.Quantity < 0 {
				issues = append(issues, fmt.Sprintf("%s: holdings[%d] (%s): quantity must not be negative", loc, j, h.Symbol))
			}
		}
		for j, tx := range p.Transactions {
			if tx.Type != "buy" && tx.Type != "sell" {
				issues = append(issues, fmt.Sprintf("%s: transactions[%d]: type %q must be buy or sell", loc, j, tx.Type))
			}
			if tx.Time != "" && config.ParseTime(tx.Time).IsZero() {
				issues = append(issues, fmt.Sprintf("%s: transactions[%d]: time %q is not a recognized format (use RFC3339 or 2006-01-02)", loc, j, tx.Time))
			}
		}
	}

	for i, w := range cfg.Watchlists {
		if w.Name == "" {
			issues = append(issues, fmt.Sprintf("watchlists[%d]: name is required", i))
		}
		if len(w.Symbols) == 0 {
			issues = append(issues, fmt.Sprintf("watchlists[%d] (%s): no symbols", i, w.Name))
		}
	}

	return issues
}

// validTheme reports whether name is a known theme preset ("dark" is a
// backwards-compat alias for tokyonight).
func validTheme(name string) bool {
	if name == "dark" {
		return true
	}
	for _, n := range theme.ThemeNames {
		if n == name {
			return true
		}
	}
	return false
}

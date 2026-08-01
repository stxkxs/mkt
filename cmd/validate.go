package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/fred"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/theme"
	"gopkg.in/yaml.v3"
)

// symbolCheckWorkers bounds the concurrent history fetches --check-symbols
// makes. A default config references well over a hundred symbols; eight in
// flight keeps the wall time reasonable without hammering the providers.
const symbolCheckWorkers = 8

// symbolCheckTimeout caps the whole --check-symbols pass. It is a
// convenience check, not a gate — it must not hang a scripted validate.
const symbolCheckTimeout = 60 * time.Second

// validateCommand builds `mkt config validate`.
//
// This is the command that was supposed to catch a config.yaml broken by a
// single bad indent and instead printed "Config OK: 163 watchlist symbols,
// 12 portfolios" — the *defaults*, because a file that does not parse still
// yields a usable Config. It now reports the parse failure with its line
// number and exits non-zero, so the reassurance can never be false again.
func validateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the config file for invalid values",
		Long: `Check ~/.config/mkt/config.yaml for anything the dashboard would
otherwise silently ignore or replace with defaults: a file that does not
parse at all, malformed durations, unknown themes, unknown alert
conditions, bad tax methods, malformed transactions, and symbols that
route to no provider. Exits non-zero if any issues are found.

With --check-symbols it additionally asks each provider for one bar per
configured symbol, which catches a typo like APPL that is shaped like a
valid ticker but will never produce a price.`,
		RunE: runValidate,
	}
	cmd.Flags().Bool("check-symbols", false, "also fetch one bar per configured symbol to catch typos that route fine but have no data (needs network)")
	return cmd
}

func runValidate(cmd *cobra.Command, args []string) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	res, err := config.LoadWithResult()
	if err != nil {
		return err
	}

	// A file that exists but does not parse is the headline failure. Report
	// it and stop: everything below would be describing the defaults, which
	// is exactly the lie this command used to tell.
	if res.Degraded {
		fmt.Fprintf(errOut, "✗ %s does not parse%s\n", tildePath(res.Path), lineSuffix(res.Line))
		if detail := parseDetail(res.Err); detail != "" {
			fmt.Fprintf(errOut, "    %s\n", detail)
		}
		fmt.Fprintf(errOut, "\n  mkt would start on built-in defaults, NOT your settings, and refuse every write.\n")
		if res.Line > 0 {
			fmt.Fprintf(errOut, "  Fix line %d, or recover an earlier copy: mkt config repair --list\n", res.Line)
		} else {
			fmt.Fprintf(errOut, "  Fix the file, or recover an earlier copy: mkt config repair --list\n")
		}
		return errSilent
	}

	cfg := res.Config
	issues := validateConfig(cfg)

	// Normalization happens during Load, so the canonical-spelling report has
	// to come from the bytes on disk — by the time we hold a Config, "btc"
	// has already become "BTC-USD".
	notes := canonicalNotes(res.Path)

	var warnings []string
	if check, _ := cmd.Flags().GetBool("check-symbols"); check {
		warnings = append(warnings, checkSymbolData(cmd.Context(), cfg)...)
	}

	for _, n := range notes {
		fmt.Fprintln(out, "ℹ "+n)
	}
	for _, w := range warnings {
		fmt.Fprintln(out, "⚠ "+w)
	}
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintln(errOut, "✗ "+issue)
		}
		return fmt.Errorf("%d issue(s) found in %s", len(issues), tildePath(res.Path))
	}

	symbols := len(cfg.Watchlist)
	for _, w := range cfg.Watchlists {
		symbols += len(w.Symbols)
	}
	fmt.Fprintf(out, "Config OK: %d watchlist symbols, %d portfolios, %d alerts (%s)\n",
		symbols, len(cfg.Portfolios), len(cfg.Alerts), tildePath(res.Path))
	return nil
}

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
		} else if !routable(r.Symbol) {
			issues = append(issues, fmt.Sprintf("%s: symbol routes to no provider, so this rule can never fire", loc))
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
			} else if !routable(h.Symbol) {
				issues = append(issues, fmt.Sprintf("%s: holdings[%d] (%s): symbol routes to no provider, so this position can never be priced", loc, j, h.Symbol))
			}
			if h.Quantity < 0 {
				issues = append(issues, fmt.Sprintf("%s: holdings[%d] (%s): quantity must not be negative", loc, j, h.Symbol))
			}
		}
		for j, tx := range p.Transactions {
			if !validTxType(tx.Type) {
				issues = append(issues, fmt.Sprintf("%s: transactions[%d]: type %q must be one of %s, %s, %s",
					loc, j, tx.Type, portfolio.TxBuy, portfolio.TxSell, portfolio.TxDividend))
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
		for _, s := range w.Symbols {
			if !routable(s) {
				issues = append(issues, fmt.Sprintf("watchlists[%d] (%s): symbol %q routes to no provider and will never show a price", i, w.Name, s))
			}
		}
	}
	for _, s := range cfg.Watchlist {
		if !routable(s) {
			issues = append(issues, fmt.Sprintf("watchlist: symbol %q routes to no provider and will never show a price", s))
		}
	}

	return issues
}

// validTxType reports whether a config transaction type is one mkt knows how
// to fold into a position. `mkt portfolio import` writes "dividend" for both
// supported CSV formats, so rejecting it here made two first-party commands
// contradict each other.
func validTxType(t string) bool {
	switch portfolio.TxType(t) {
	case portfolio.TxBuy, portfolio.TxSell, portfolio.TxDividend:
		return true
	}
	return false
}

// routable reports whether a symbol reaches some provider. A symbol that
// classifies as nothing (a company name, a slash-separated pair, free text)
// is never subscribed and never quoted — it just sits in the watchlist
// forever showing no price, which is indistinguishable from a dead feed.
func routable(sym string) bool {
	return symbol.IsCrypto(sym) || symbol.IsStock(sym) || symbol.IsFRED(sym)
}

// rawConfig is the subset of config.yaml the canonical-spelling report
// needs, decoded straight from the file so the symbols are exactly as the
// user typed them. config.Load normalizes on the way in, which is why this
// cannot come from a *config.Config.
type rawConfig struct {
	Watchlist  []string `yaml:"watchlist"`
	Watchlists []struct {
		Name    string   `yaml:"name"`
		Symbols []string `yaml:"symbols"`
	} `yaml:"watchlists"`
	Portfolios []struct {
		Name     string `yaml:"name"`
		Holdings []struct {
			Symbol string `yaml:"symbol"`
		} `yaml:"holdings"`
		Transactions []struct {
			Symbol string `yaml:"symbol"`
		} `yaml:"transactions"`
	} `yaml:"portfolios"`
	Alerts []struct {
		Symbol string `yaml:"symbol"`
	} `yaml:"alerts"`
}

// canonicalNotes reports symbols written in a non-canonical spelling and
// what mkt rewrites them to. Nothing is broken — normalization handles it —
// but a user comparing their file against the dashboard deserves to know
// why "matic" shows up as POL-USD.
func canonicalNotes(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rc rawConfig
	if err := yaml.Unmarshal(raw, &rc); err != nil {
		return nil
	}

	// One note per distinct rewrite, not one per occurrence: a symbol listed
	// in three groups is still one thing to know.
	seen := make(map[string]string)
	note := func(s string) {
		c := symbol.Canonical(s)
		if c == "" || c == s {
			return
		}
		seen[s] = c
	}
	for _, s := range rc.Watchlist {
		note(s)
	}
	for _, w := range rc.Watchlists {
		for _, s := range w.Symbols {
			note(s)
		}
	}
	for _, p := range rc.Portfolios {
		for _, h := range p.Holdings {
			note(h.Symbol)
		}
		for _, t := range p.Transactions {
			note(t.Symbol)
		}
	}
	for _, a := range rc.Alerts {
		note(a.Symbol)
	}

	typed := make([]string, 0, len(seen))
	for s := range seen {
		typed = append(typed, s)
	}
	sort.Strings(typed)
	out := make([]string, 0, len(typed))
	for _, s := range typed {
		out = append(out, fmt.Sprintf("%q is not canonical; mkt reads it as %s", s, seen[s]))
	}
	return out
}

// checkSymbolData asks each provider for a single bar per configured symbol
// and reports the ones that came back empty. This is the only way to catch a
// typo like APPL: it is shaped exactly like a ticker, routes cleanly to
// Yahoo, and simply never has data — which on the dashboard looks the same
// as a slow feed.
//
// Failures are warnings, never issues: a flaky network must not make
// `mkt config validate` fail in CI.
func checkSymbolData(ctx context.Context, cfg *config.Config) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, symbolCheckTimeout)
	defer cancel()

	symbols := configSymbols(cfg)
	if len(symbols) == 0 {
		return nil
	}

	hist := market.NewMultiHistoryProvider(fred.New(), coinbase.New(), yahoo.New(cfg.PollDuration()))

	var (
		mu     sync.Mutex
		failed []string
		wg     sync.WaitGroup
	)
	work := make(chan string)
	for range symbolCheckWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range work {
				bars, err := hist.History(ctx, provider.HistoryParams{Symbol: sym, Interval: provider.Interval1d, Limit: 1})
				if err == nil && len(bars) > 0 {
					continue
				}
				mu.Lock()
				failed = append(failed, sym)
				mu.Unlock()
			}
		}()
	}
	for _, sym := range symbols {
		select {
		case work <- sym:
		case <-ctx.Done():
		}
	}
	close(work)
	wg.Wait()

	if len(failed) == 0 {
		return nil
	}
	sort.Strings(failed)
	out := make([]string, 0, len(failed))
	for _, sym := range failed {
		out = append(out, fmt.Sprintf("%s returned no data — a typo, a delisted ticker, or the provider was unreachable", sym))
	}
	return out
}

// configSymbols is the deduplicated union of every symbol the config
// references: watchlists, portfolio holdings, and alert rules.
func configSymbols(cfg *config.Config) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, s := range cfg.Watchlist {
		add(s)
	}
	for _, w := range cfg.Watchlists {
		for _, s := range w.Symbols {
			add(s)
		}
	}
	for _, p := range cfg.Portfolios {
		for _, h := range p.Holdings {
			add(h.Symbol)
		}
	}
	for _, r := range cfg.Alerts {
		add(r.Symbol)
	}
	sort.Strings(out)
	return out
}

// configFilePath is the path Load reads and every write targets. config
// exports the directory but not the filename, and several commands need the
// full path to name it in a message.
func configFilePath() string {
	return filepath.Join(config.ConfigDir(), "config.yaml")
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

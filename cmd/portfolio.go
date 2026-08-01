package cmd

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/importer"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/fred"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	portfolioview "github.com/stxkxs/mkt/internal/tui/portfolio"
)

// equityHistoryFile is the NDJSON equity curve the dashboard appends a mark
// to every five minutes. `mkt portfolio stats` reads the same file the TUI
// does, so both report the same history.
const equityHistoryFile = "equity-history.ndjson"

// minBetaDays is the fewest paired daily observations worth computing a beta
// from. Below this the number is noise dressed up as a risk measure.
const minBetaDays = 5

func init() {
	portfolioCmd := &cobra.Command{
		Use:   "portfolio",
		Short: "Manage portfolio data",
	}

	importCmd := &cobra.Command{
		Use:   "import [file]",
		Short: "Import transactions from a broker CSV export",
		Long: `Reads a CSV file and appends the parsed transactions to a named portfolio.
Auto-detects the format (generic, schwab) from the header line; use --format to override.
The portfolio is created if it does not exist.

Importing the same file twice is a no-op: a transaction already present in
the destination portfolio (same date, type, symbol, quantity and price) is
skipped rather than duplicated. Genuine same-day repeats are preserved —
matching is by count, so two identical rows in the CSV still import as two.
Pass --force to append everything regardless.`,
		Args: cobra.ExactArgs(1),
		RunE: runImport,
	}
	importCmd.Flags().String("portfolio", "", "destination portfolio name (created if absent)")
	importCmd.Flags().String("format", "auto", "CSV format: auto, generic, or schwab")
	importCmd.Flags().Bool("dry-run", false, "parse and print summary without saving")

	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Risk and return statistics from the recorded equity curve",
		Long: `Reports Sharpe, Sortino, max drawdown, CAGR and beta for each portfolio
that has an equity history.

The numbers come from ` + equityHistoryFile + ` in the config directory, which
the dashboard appends a mark to every five minutes — so a portfolio only
appears here once mkt has been running with it for a while. Annualized
figures extrapolated from a very short window are labelled as such rather
than presented as fact.`,
		Args: cobra.NoArgs,
		RunE: runPortfolioStats,
	}
	statsCmd.Flags().String("portfolio", "", "only report this portfolio (default: every portfolio with history)")
	statsCmd.Flags().Float64("rf", 0, "risk-free rate per mark interval, for the Sharpe/Sortino excess return")
	statsCmd.Flags().String("benchmark", portfolioview.DefaultBenchmark, "symbol to compute beta against; empty string skips the fetch")

	portfolioCmd.AddCommand(importCmd, statsCmd)
	addWriteSafetyFlags(portfolioCmd)
	rootCmd.AddCommand(portfolioCmd)
}

// ──────────────────────────────── import ────────────────────────────────

func runImport(cmd *cobra.Command, args []string) error {
	path := args[0]
	pname, _ := cmd.Flags().GetString("portfolio")
	formatName, _ := cmd.Flags().GetString("format")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	out := cmd.OutOrStdout()

	if pname == "" {
		return fmt.Errorf("--portfolio is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var fmtImpl importer.Format
	var reader io.Reader = f
	if formatName == "" || formatName == "auto" {
		detected, rest, err := importer.Detect(f)
		if err != nil {
			return err
		}
		fmtImpl = detected
		reader = rest
	} else {
		fmtImpl = importer.ByName(formatName)
		if fmtImpl == nil {
			return fmt.Errorf("unknown format %q (try generic or schwab)", formatName)
		}
	}

	txs, err := fmtImpl.Parse(reader)
	if err != nil {
		return fmt.Errorf("%s: %w", fmtImpl.Name(), err)
	}

	res, err := config.LoadWithResult()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := res.Config

	converted := make([]config.Transaction, 0, len(txs))
	for _, t := range txs {
		converted = append(converted, config.Transaction{
			Type:     string(t.Type),
			Symbol:   t.Symbol,
			Quantity: t.Quantity,
			Price:    t.Price,
			Time:     t.Time.Format("2006-01-02"),
			Fee:      t.Fee,
			Note:     t.Note,
		})
	}

	// Find or create portfolio.
	pIdx := -1
	for i := range cfg.Portfolios {
		if cfg.Portfolios[i].Name == pname {
			pIdx = i
			break
		}
	}
	if pIdx == -1 {
		cfg.Portfolios = append(cfg.Portfolios, config.Portfolio{Name: pname})
		pIdx = len(cfg.Portfolios) - 1
	}

	fresh, skipped := newTransactions(cfg.Portfolios[pIdx].Transactions, converted, force)
	cfg.Portfolios[pIdx].Transactions = append(cfg.Portfolios[pIdx].Transactions, fresh...)

	summary := summarize(txs)
	fmt.Fprintf(out, "Parsed %d transactions from %s (format: %s)\n", len(txs), path, fmtImpl.Name())
	fmt.Fprintf(out, "  %d buy, %d sell, %d dividend\n", summary[portfolio.TxBuy], summary[portfolio.TxSell], summary[portfolio.TxDividend])
	if skipped > 0 {
		fmt.Fprintf(out, "  %d already in portfolio %q, skipped (re-run with --force to append anyway)\n", skipped, pname)
	}
	// Both early returns finish without calling saveConfig, so neither would
	// otherwise say that the portfolio being reported on is a built-in
	// default rather than the user's — which on a dry run is the difference
	// between "these 3 rows are new" and "these 3 rows look new because mkt
	// cannot read the file that already holds them".
	if dryRun {
		fmt.Fprintf(out, "--dry-run set; config not modified (%d would be appended)\n", len(fresh))
		warnDegradedConfig(cmd.ErrOrStderr(), res)
		if res.Degraded {
			return errSilent
		}
		return nil
	}
	if len(fresh) == 0 {
		fmt.Fprintf(out, "Nothing new to import; portfolio %q left unchanged (%d transactions)\n",
			pname, len(cfg.Portfolios[pIdx].Transactions))
		warnDegradedConfig(cmd.ErrOrStderr(), res)
		if res.Degraded {
			return errSilent
		}
		return nil
	}
	if err := saveConfig(cmd, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "  ✓ appended %d to portfolio %q (now %d transactions)\n",
		len(fresh), pname, len(cfg.Portfolios[pIdx].Transactions))
	return nil
}

// newTransactions splits incoming against the transactions already recorded
// for the portfolio, returning the ones to append and how many were skipped
// as already present.
//
// Matching is a multiset, not a set: two identical rows in one CSV are two
// real transactions and both must import, while re-running the same CSV must
// add nothing. Counting occurrences gives both. force disables the check
// entirely for the case where the same trade genuinely happened again.
func newTransactions(existing, incoming []config.Transaction, force bool) (fresh []config.Transaction, skipped int) {
	if force {
		return incoming, 0
	}
	have := make(map[string]int, len(existing))
	for _, t := range existing {
		have[txKey(t)]++
	}
	fresh = make([]config.Transaction, 0, len(incoming))
	for _, t := range incoming {
		k := txKey(t)
		if have[k] > 0 {
			have[k]--
			skipped++
			continue
		}
		fresh = append(fresh, t)
	}
	return fresh, skipped
}

// txKey is a transaction's identity for de-duplication: the fields a broker
// export reproduces byte-for-byte on every download. Fees and notes are
// deliberately excluded — brokers revise them, and a revised fee does not
// make it a different trade.
func txKey(t config.Transaction) string {
	return t.Time + "|" + t.Type + "|" + t.Symbol + "|" +
		strconv.FormatFloat(t.Quantity, 'f', -1, 64) + "|" +
		strconv.FormatFloat(t.Price, 'f', -1, 64)
}

func summarize(txs []portfolio.Transaction) map[portfolio.TxType]int {
	out := map[portfolio.TxType]int{}
	for _, t := range txs {
		out[t.Type]++
	}
	return out
}

// ───────────────────────────────── stats ─────────────────────────────────

func runPortfolioStats(cmd *cobra.Command, args []string) error {
	only, _ := cmd.Flags().GetString("portfolio")
	rf, _ := cmd.Flags().GetFloat64("rf")
	benchmark, _ := cmd.Flags().GetString("benchmark")
	out := cmd.OutOrStdout()

	res, err := config.LoadWithResult()
	if err != nil {
		return err
	}
	warnDegradedConfig(cmd.ErrOrStderr(), res)

	path := filepath.Join(config.ConfigDir(), equityHistoryFile)
	// max 0: stats want the whole curve, not the tail the dashboard seeds
	// its sparkline from.
	byName, err := portfolio.NewEquityFile(path, 0).LoadByName()
	if err != nil {
		return err
	}
	if len(byName) == 0 {
		fmt.Fprintf(out, "No equity history yet (%s).\n", tildePath(path))
		fmt.Fprintf(out, "mkt records a mark every 5 minutes while the dashboard is running;\n")
		fmt.Fprintf(out, "leave `mkt` open for a while and these stats fill in.\n")
		return nil
	}

	names := sortedNames(byName)
	if only != "" {
		if _, ok := byName[only]; !ok {
			return fmt.Errorf("no equity history for portfolio %q (have: %v)", only, names)
		}
		names = []string{only}
	}

	hist := historyProvider(res.Config)
	for i, name := range names {
		if i > 0 {
			fmt.Fprintln(out)
		}
		printPortfolioStats(cmd.Context(), out, hist, name, byName[name], rf, benchmark)
	}
	return nil
}

// printPortfolioStats renders one portfolio's risk/return picture. Every
// figure that is not defined for the input is printed as "n/a" rather than
// as a zero a reader would take at face value.
func printPortfolioStats(ctx context.Context, w io.Writer, hist historyFetcher, name string, marks []portfolio.EquityMark, rf float64, benchmark string) {
	s := portfolio.StatsFromMarks(marks, rf)

	fmt.Fprintf(w, "Portfolio %q\n", name)
	if s.Marks < 2 {
		fmt.Fprintf(w, "  %d equity mark recorded — not enough history for any statistic.\n", s.Marks)
		fmt.Fprintf(w, "  mkt records one every 5 minutes while the dashboard is running.\n")
		return
	}

	period := "period"
	if s.Period > 0 {
		period = shortDuration(s.Period)
	}

	fmt.Fprintf(w, "  marks           %d over %s (%s → %s, ~%s apart)\n",
		s.Marks, shortDuration(s.Elapsed),
		s.First.Local().Format("2006-01-02 15:04"),
		s.Last.Local().Format("2006-01-02 15:04"),
		period)
	fmt.Fprintf(w, "  value           %s → %s (%s)\n",
		money(s.StartValue), money(s.EndValue), pct(s.TotalReturn))
	fmt.Fprintf(w, "  max drawdown    %s\n", pctPlain(s.MaxDrawdown))
	fmt.Fprintf(w, "  volatility      %s per %s (%s annualized)\n",
		pctPlain(s.Volatility), period, pctPlain(s.AnnualizedVolatility))
	fmt.Fprintf(w, "  sharpe          %s per %s (%s annualized)\n",
		ratio(s.Sharpe), period, ratio(s.AnnualizedSharpe))
	fmt.Fprintf(w, "  sortino         %s per %s\n", ratio(s.Sortino), period)
	fmt.Fprintf(w, "  CAGR            %s\n", pct(s.AnnualizedReturn))

	if benchmark != "" {
		beta, n, err := betaAgainst(ctx, hist, benchmark, marks)
		switch {
		case err != nil:
			fmt.Fprintf(w, "  beta vs %-7s n/a (%v)\n", benchmark, err)
		default:
			fmt.Fprintf(w, "  beta vs %-7s %s (%d paired daily observations)\n", benchmark, ratio(beta), n)
		}
	}

	// Annualizing an afternoon of five-minute marks produces confident
	// nonsense. Say so instead of letting the reader assume a year of data.
	if s.Elapsed < 7*24*time.Hour {
		fmt.Fprintf(w, "\n  ⚠  only %s of history — the annualized figures (CAGR, annualized\n", shortDuration(s.Elapsed))
		fmt.Fprintf(w, "     volatility and Sharpe) are extrapolations, not measurements.\n")
	}
}

// betaAgainst computes the portfolio's beta to a benchmark symbol.
//
// The equity curve is sampled every five minutes and the benchmark is only
// available as daily bars, so both series are collapsed to one value per
// calendar day and paired on the days they share. That is the finest
// alignment the two sources actually support; anything finer would be
// interpolation presented as data.
func betaAgainst(ctx context.Context, hist historyFetcher, benchmark string, marks []portfolio.EquityMark) (float64, int, error) {
	if hist == nil {
		return 0, 0, fmt.Errorf("no history provider")
	}
	equity := dailyCloses(marks)
	if len(equity) < minBetaDays {
		return 0, 0, fmt.Errorf("%d day(s) of equity history, need %d", len(equity), minBetaDays)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// A little headroom over the equity window: weekends and holidays mean
	// the benchmark has fewer bars than the span has days.
	bars, err := hist.History(ctx, provider.HistoryParams{
		Symbol:   benchmark,
		Interval: provider.Interval1d,
		Limit:    len(equity) + 30,
	})
	if err != nil {
		return 0, 0, err
	}
	bench := make(map[string]float64, len(bars))
	for _, b := range bars {
		bench[b.Time.UTC().Format("2006-01-02")] = b.Close
	}

	days := make([]string, 0, len(equity))
	for d := range equity {
		if _, ok := bench[d]; ok {
			days = append(days, d)
		}
	}
	if len(days) < minBetaDays {
		return 0, 0, fmt.Errorf("%d overlapping day(s) with %s, need %d", len(days), benchmark, minBetaDays)
	}
	sort.Strings(days)

	assetVals := make([]float64, 0, len(days))
	benchVals := make([]float64, 0, len(days))
	for _, d := range days {
		assetVals = append(assetVals, equity[d])
		benchVals = append(benchVals, bench[d])
	}
	beta := portfolio.Beta(portfolio.Returns(assetVals), portfolio.Returns(benchVals))
	if math.IsNaN(beta) {
		return 0, len(days), fmt.Errorf("benchmark did not move over the window")
	}
	return beta, len(days), nil
}

// dailyCloses collapses equity marks to the last value recorded on each UTC
// calendar day, keyed by date — the granularity a daily benchmark bar can
// actually be paired with.
func dailyCloses(marks []portfolio.EquityMark) map[string]float64 {
	sorted := make([]portfolio.EquityMark, len(marks))
	copy(sorted, marks)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })
	out := make(map[string]float64, len(sorted))
	for _, m := range sorted {
		out[m.Time.UTC().Format("2006-01-02")] = m.Value
	}
	return out
}

// historyFetcher is the one method these commands need from a history
// provider. market.MultiHistoryProvider is a router rather than a provider
// — it has no Name or Supports of its own — so depending on the narrower
// interface is what lets the CLI share the dashboard's routing.
type historyFetcher interface {
	History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error)
}

// historyProvider builds the same provider chain the dashboard routes
// history through: FRED first (its prefix is unique), then Coinbase for
// crypto, then Yahoo for everything else.
func historyProvider(cfg *config.Config) *market.MultiHistoryProvider {
	interval := 15 * time.Second
	if cfg != nil {
		interval = cfg.PollDuration()
	}
	return market.NewMultiHistoryProvider(fred.New(), coinbase.New(), yahoo.New(interval))
}

// ───────────────────────── config → domain portfolios ─────────────────────

// portfoliosFromConfig converts config portfolios into the domain type.
//
// This is the single canonical conversion: it carries the transaction log
// and the tax method across, and folds any transactions onto the snapshot
// holdings with Materialize (with no transactions the snapshot passes
// through unchanged). Re-deriving it per call site is how the MCP server
// ended up silently dropping every transaction, every tax method, and the
// materialization — reporting a P&L from stale snapshot holdings that an
// agent then treated as authoritative.
func portfoliosFromConfig(cps []config.Portfolio) []portfolio.Portfolio {
	out := make([]portfolio.Portfolio, 0, len(cps))
	for _, cp := range cps {
		var holdings []portfolio.Holding
		for _, h := range cp.Holdings {
			holdings = append(holdings, portfolio.Holding{
				Symbol:    h.Symbol,
				Name:      h.Name,
				Quantity:  h.Quantity,
				CostBasis: h.CostBasis,
			})
		}
		var txs []portfolio.Transaction
		for _, t := range cp.Transactions {
			txs = append(txs, portfolio.Transaction{
				Type:     portfolio.TxType(t.Type),
				Symbol:   t.Symbol,
				Quantity: t.Quantity,
				Price:    t.Price,
				Time:     config.ParseTime(t.Time),
				Fee:      t.Fee,
				Note:     t.Note,
			})
		}
		out = append(out, portfolio.Portfolio{
			Name:         cp.Name,
			Holdings:     portfolio.Materialize(holdings, txs),
			Transactions: txs,
			TaxMethod:    portfolio.TaxMethod(cp.TaxMethod),
		})
	}
	return out
}

// ──────────────────────────── number formatting ───────────────────────────

// pct renders a fraction as a percentage, or "n/a" when the statistic is
// undefined for the input (StatsFromMarks returns NaN rather than zero
// precisely so this distinction survives).
func pct(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "n/a"
	}
	return fmt.Sprintf("%+.2f%%", f*100)
}

// pctPlain renders a magnitude (drawdown, volatility) as a percentage
// without a sign, where a leading "+" would only read as noise.
func pctPlain(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", f*100)
}

// ratio renders a dimensionless statistic (Sharpe, Sortino, beta).
func ratio(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "n/a"
	}
	return fmt.Sprintf("%.3f", f)
}

// money renders a portfolio value.
func money(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "n/a"
	}
	return fmt.Sprintf("$%.2f", f)
}

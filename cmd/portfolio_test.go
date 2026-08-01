package cmd

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/portfolio"
)

// genericCSV is the format `mkt portfolio import` auto-detects, including a
// dividend row — the type both supported formats emit and that `config
// validate` used to reject.
const genericCSV = `date,type,symbol,quantity,price
2025-01-02,buy,AAPL,10,150.00
2025-01-03,buy,MSFT,5,400.00
2025-02-13,dividend,AAPL,10,0.24
`

func writeCSV(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trades.csv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

// txCount reports how many transactions the named portfolio holds on disk.
func txCount(t *testing.T, name string) int {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, p := range cfg.Portfolios {
		if p.Name == name {
			return len(p.Transactions)
		}
	}
	t.Fatalf("no portfolio %q in config", name)
	return 0
}

// TestImportIsIdempotent is the regression test: re-running the same CSV
// used to silently double every transaction.
func TestImportIsIdempotent(t *testing.T) {
	seedConfig(t, goodConfig)
	csv := writeCSV(t, genericCSV)

	if _, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage"); err != nil {
		t.Fatalf("first import: %v\n%s", err, errOut)
	}
	if got := txCount(t, "Brokerage"); got != 3 {
		t.Fatalf("after first import got %d transactions, want 3", got)
	}

	out, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage")
	if err != nil {
		t.Fatalf("second import: %v\n%s", err, errOut)
	}
	if got := txCount(t, "Brokerage"); got != 3 {
		t.Errorf("re-importing the same file produced %d transactions, want 3", got)
	}
	if !strings.Contains(out, "3 already in portfolio") {
		t.Errorf("skip count not reported:\n%s", out)
	}
	if !strings.Contains(out, "Nothing new to import") {
		t.Errorf("no-op import not reported as such:\n%s", out)
	}
}

// TestImportForceAppendsAnyway covers the escape hatch for a trade that
// genuinely happened twice.
func TestImportForceAppendsAnyway(t *testing.T) {
	seedConfig(t, goodConfig)
	csv := writeCSV(t, genericCSV)

	if _, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage"); err != nil {
		t.Fatalf("first import: %v\n%s", err, errOut)
	}
	if _, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage", "--force"); err != nil {
		t.Fatalf("forced import: %v\n%s", err, errOut)
	}
	if got := txCount(t, "Brokerage"); got != 6 {
		t.Errorf("--force produced %d transactions, want 6", got)
	}
}

// TestImportPartialOverlapAppendsOnlyTheNewRows is the realistic case: a
// broker export that overlaps the previous one.
func TestImportPartialOverlapAppendsOnlyTheNewRows(t *testing.T) {
	seedConfig(t, goodConfig)

	first := writeCSV(t, genericCSV)
	if _, errOut, err := runCLI(t, nil, "portfolio", "import", first, "--portfolio", "Brokerage"); err != nil {
		t.Fatalf("first import: %v\n%s", err, errOut)
	}

	second := writeCSV(t, genericCSV+"2025-03-01,sell,MSFT,5,420.00\n")
	out, errOut, err := runCLI(t, nil, "portfolio", "import", second, "--portfolio", "Brokerage")
	if err != nil {
		t.Fatalf("second import: %v\n%s", err, errOut)
	}
	if got := txCount(t, "Brokerage"); got != 4 {
		t.Errorf("got %d transactions, want 4", got)
	}
	if !strings.Contains(out, "appended 1") {
		t.Errorf("append count not reported:\n%s", out)
	}
}

// TestImportDryRunStillWritesNothing guards the flag that survived the
// rewrite.
func TestImportDryRunStillWritesNothing(t *testing.T) {
	path := seedConfig(t, goodConfig)
	before := readFile(t, path)
	csv := writeCSV(t, genericCSV)

	out, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, errOut)
	}
	if readFile(t, path) != before {
		t.Error("--dry-run modified the config")
	}
	if !strings.Contains(out, "config not modified") {
		t.Errorf("dry run not reported:\n%s", out)
	}
}

// TestImportRefusesOverADegradedConfig makes sure the import path goes
// through the same write safety as `config set` — it writes the same file.
func TestImportRefusesOverADegradedConfig(t *testing.T) {
	path := seedConfig(t, brokenConfig)
	before := readFile(t, path)
	csv := writeCSV(t, genericCSV)

	out, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage")
	if err == nil {
		t.Fatal("import over a degraded config exited zero")
	}
	if readFile(t, path) != before {
		t.Error("config was modified despite the refusal")
	}
	if !strings.Contains(out+errOut, "Your file has NOT been modified") {
		t.Errorf("no refusal message:\n%s", out+errOut)
	}
}

// TestImportedTransactionsPassValidate is the cross-command contract: what
// import writes, validate must accept.
func TestImportedTransactionsPassValidate(t *testing.T) {
	seedConfig(t, goodConfig)
	csv := writeCSV(t, genericCSV)

	if _, errOut, err := runCLI(t, nil, "portfolio", "import", csv, "--portfolio", "Brokerage"); err != nil {
		t.Fatalf("import: %v\n%s", err, errOut)
	}
	out, errOut, err := runCLI(t, nil, "config", "validate")
	if err != nil {
		t.Fatalf("validate rejected what import wrote: %v\n%s%s", err, out, errOut)
	}
	if !strings.Contains(out, "Config OK") {
		t.Errorf("validate did not report OK:\n%s", out)
	}
}

func TestNewTransactionsMultisetSemantics(t *testing.T) {
	tx := func(day string, qty float64) config.Transaction {
		return config.Transaction{Type: "buy", Symbol: "AAPL", Quantity: qty, Price: 100, Time: day}
	}
	dup := tx("2025-01-02", 10)

	// Two identical rows in one CSV are two real trades on a fresh import.
	fresh, skipped := newTransactions(nil, []config.Transaction{dup, dup}, false)
	if len(fresh) != 2 || skipped != 0 {
		t.Errorf("fresh import of a repeated row: fresh=%d skipped=%d, want 2/0", len(fresh), skipped)
	}

	// Re-importing that same file adds nothing.
	fresh, skipped = newTransactions([]config.Transaction{dup, dup}, []config.Transaction{dup, dup}, false)
	if len(fresh) != 0 || skipped != 2 {
		t.Errorf("re-import: fresh=%d skipped=%d, want 0/2", len(fresh), skipped)
	}

	// One already present, one new.
	fresh, skipped = newTransactions([]config.Transaction{dup}, []config.Transaction{dup, tx("2025-01-03", 5)}, false)
	if len(fresh) != 1 || skipped != 1 {
		t.Errorf("partial overlap: fresh=%d skipped=%d, want 1/1", len(fresh), skipped)
	}

	// force bypasses the check entirely.
	fresh, skipped = newTransactions([]config.Transaction{dup}, []config.Transaction{dup}, true)
	if len(fresh) != 1 || skipped != 0 {
		t.Errorf("force: fresh=%d skipped=%d, want 1/0", len(fresh), skipped)
	}
}

// TestTxKeyIgnoresFeeAndNote: a broker revising a fee does not make it a
// different trade, and must not resurrect a duplicate.
func TestTxKeyIgnoresFeeAndNote(t *testing.T) {
	a := config.Transaction{Type: "buy", Symbol: "AAPL", Quantity: 10, Price: 150, Time: "2025-01-02"}
	b := a
	b.Fee = 1.25
	b.Note = "revised"
	if txKey(a) != txKey(b) {
		t.Error("fee/note changed the transaction identity")
	}
	c := a
	c.Quantity = 11
	if txKey(a) == txKey(c) {
		t.Error("a different quantity produced the same identity")
	}
}

// ───────────────────────── config → domain portfolios ─────────────────────

// TestPortfoliosFromConfigCarriesEverything is the regression test for the
// MCP server's private copy of this conversion, which dropped Transactions,
// Materialize and TaxMethod.
func TestPortfoliosFromConfigCarriesEverything(t *testing.T) {
	got := portfoliosFromConfig([]config.Portfolio{{
		Name:      "Core",
		TaxMethod: "fifo",
		Holdings:  []config.Holding{{Symbol: "AAPL", Quantity: 10, CostBasis: 150}},
		Transactions: []config.Transaction{
			{Type: "buy", Symbol: "MSFT", Quantity: 5, Price: 400, Time: "2025-01-03"},
		},
	}})
	if len(got) != 1 {
		t.Fatalf("got %d portfolios, want 1", len(got))
	}
	p := got[0]
	if p.TaxMethod != portfolio.TaxMethod("fifo") {
		t.Errorf("tax method dropped: %q", p.TaxMethod)
	}
	if len(p.Transactions) != 1 {
		t.Fatalf("transactions dropped: %+v", p.Transactions)
	}
	if p.Transactions[0].Time.IsZero() {
		t.Error("transaction time was not parsed")
	}
	// Materialize folds the MSFT buy onto the AAPL snapshot.
	syms := map[string]bool{}
	for _, h := range p.Holdings {
		syms[h.Symbol] = true
	}
	if !syms["AAPL"] || !syms["MSFT"] {
		t.Errorf("holdings were not materialized from transactions: %+v", p.Holdings)
	}
}

// ───────────────────────────────── stats ─────────────────────────────────

// writeEquity seeds the equity history file the stats command reads.
func writeEquity(t *testing.T, marks []portfolio.EquityMark) {
	t.Helper()
	f := portfolio.NewEquityFile(filepath.Join(config.ConfigDir(), equityHistoryFile), 0)
	for _, m := range marks {
		if err := f.Append(m); err != nil {
			t.Fatalf("append equity: %v", err)
		}
	}
}

func TestPortfolioStatsWithNoHistorySaysSo(t *testing.T) {
	seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, nil, "portfolio", "stats")
	if err != nil {
		t.Fatalf("portfolio stats: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "No equity history yet") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// TestPortfolioStatsReportsTheRatios exercises the metrics that had zero
// callers before this command existed.
func TestPortfolioStatsReportsTheRatios(t *testing.T) {
	seedConfig(t, goodConfig)

	// A month of daily marks with a drawdown in the middle, so Sharpe,
	// Sortino and max drawdown are all defined.
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var marks []portfolio.EquityMark
	value := 10000.0
	for i := range 30 {
		switch {
		case i < 10:
			value *= 1.01
		case i < 15:
			value *= 0.97
		default:
			value *= 1.008
		}
		marks = append(marks, portfolio.EquityMark{
			Time:          start.AddDate(0, 0, i),
			PortfolioName: "Retirement",
			Value:         value,
		})
	}
	writeEquity(t, marks)

	// --benchmark "" keeps the test off the network.
	out, errOut, err := runCLI(t, nil, "portfolio", "stats", "--benchmark", "")
	if err != nil {
		t.Fatalf("portfolio stats: %v\n%s", err, errOut)
	}
	for _, want := range []string{"Retirement", "sharpe", "sortino", "max drawdown", "CAGR", "marks"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "n/a per") {
		t.Errorf("ratios undefined over 30 marks:\n%s", out)
	}
	if strings.Contains(out, "extrapolations") {
		t.Errorf("30 days of history should not be flagged as too short:\n%s", out)
	}
}

// TestPortfolioStatsFlagsThinHistory: annualizing an afternoon must be
// labelled rather than presented as measured.
func TestPortfolioStatsFlagsThinHistory(t *testing.T) {
	seedConfig(t, goodConfig)

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	var marks []portfolio.EquityMark
	for i := range 6 {
		marks = append(marks, portfolio.EquityMark{
			Time:          start.Add(time.Duration(i) * 5 * time.Minute),
			PortfolioName: "Retirement",
			Value:         10000 + float64(i)*25,
		})
	}
	writeEquity(t, marks)

	out, errOut, err := runCLI(t, nil, "portfolio", "stats", "--benchmark", "")
	if err != nil {
		t.Fatalf("portfolio stats: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "extrapolations") {
		t.Errorf("thin history was not flagged:\n%s", out)
	}
}

func TestPortfolioStatsUnknownPortfolioIsAnError(t *testing.T) {
	seedConfig(t, goodConfig)
	writeEquity(t, []portfolio.EquityMark{
		{Time: time.Now().Add(-time.Hour), PortfolioName: "Retirement", Value: 100},
		{Time: time.Now(), PortfolioName: "Retirement", Value: 110},
	})

	if _, _, err := runCLI(t, nil, "portfolio", "stats", "--portfolio", "Nope", "--benchmark", ""); err == nil {
		t.Error("unknown portfolio did not error")
	}
}

// TestBetaAgainstNeedsOverlappingDays checks the honest refusal rather than
// a beta computed from two points.
func TestBetaAgainstNeedsOverlappingDays(t *testing.T) {
	marks := []portfolio.EquityMark{
		{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Value: 100},
		{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Value: 101},
	}
	if _, _, err := betaAgainst(t.Context(), nil, "SPY", marks); err == nil {
		t.Error("beta computed from two marks and no provider")
	}
}

func TestDailyClosesKeepsTheLastMarkOfEachDay(t *testing.T) {
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := dailyCloses([]portfolio.EquityMark{
		{Time: day.Add(9 * time.Hour), Value: 100},
		{Time: day.Add(16 * time.Hour), Value: 120},
		{Time: day.AddDate(0, 0, 1), Value: 130},
	})
	if len(got) != 2 {
		t.Fatalf("got %d days, want 2", len(got))
	}
	if got["2026-01-01"] != 120 {
		t.Errorf("2026-01-01 = %v, want the last mark of the day (120)", got["2026-01-01"])
	}
}

func TestNumberFormattersRenderUndefinedAsNA(t *testing.T) {
	nan := math.NaN()
	if pct(nan) != "n/a" || ratio(nan) != "n/a" || money(nan) != "n/a" || pctPlain(nan) != "n/a" {
		t.Error("a NaN statistic was rendered as a number")
	}
	if got := pct(0.1234); got != "+12.34%" {
		t.Errorf("pct(0.1234) = %q", got)
	}
	if got := pctPlain(0.1234); got != "12.34%" {
		t.Errorf("pctPlain(0.1234) = %q", got)
	}
}

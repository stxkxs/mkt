package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stxkxs/mkt/internal/config"
	watchlistview "github.com/stxkxs/mkt/internal/tui/watchlist"
)

// The symbols the data plane subscribes to must include every portfolio
// holding, not just the watchlist. `mkt portfolio import` does not touch the
// watchlist, so a holding-only symbol that is not subscribed is never priced
// and shows up as an invented break-even row.
func TestSubscribeSymbolsIncludesHoldings(t *testing.T) {
	groups := []watchlistview.Group{{Name: "Core", Symbols: []string{"AAPL", "BTC-USD"}}}
	portfolios := []config.Portfolio{{
		Name:     "Long term",
		Holdings: []config.Holding{{Symbol: "KO", Quantity: 100, CostBasis: 55}},
	}}

	got := subscribeSymbols(groups, portfolios)
	want := []string{"AAPL", "BTC-USD", "KO"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subscribeSymbols() = %v, want %v", got, want)
	}
}

// A ledger can name a symbol that no longer appears in the holdings
// snapshot; it still has to be priced for the realized/unrealized split to
// add up.
func TestSubscribeSymbolsIncludesTransactionSymbols(t *testing.T) {
	portfolios := []config.Portfolio{{
		Name:         "Traded",
		Transactions: []config.Transaction{{Type: "buy", Symbol: "msft", Quantity: 1, Price: 300}},
	}}

	got := subscribeSymbols(nil, portfolios)
	if !reflect.DeepEqual(got, []string{"MSFT"}) {
		t.Errorf("subscribeSymbols() = %v, want [MSFT]", got)
	}
}

// Everything is canonicalized at the ingest boundary so a hand-typed `btc`
// or `ETHUSDT` matches the spelling the providers emit, and the spellings
// that collapse onto one symbol subscribe once.
func TestSubscribeSymbolsCanonicalizesAndDedupes(t *testing.T) {
	groups := []watchlistview.Group{
		{Name: "A", Symbols: []string{"btc", "BTCUSDT", "", "  ", "aapl"}},
		{Name: "B", Symbols: []string{"BTC-USD", "ethusdt"}},
	}
	portfolios := []config.Portfolio{{
		Holdings: []config.Holding{{Symbol: "AAPL"}, {Symbol: "eth-usd"}},
	}}

	got := subscribeSymbols(groups, portfolios)
	want := []string{"BTC-USD", "AAPL", "ETH-USD"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subscribeSymbols() = %v, want %v", got, want)
	}
}

func TestDedupeUnionCanonicalizes(t *testing.T) {
	groups := []watchlistview.Group{
		{Name: "A", Symbols: []string{"aapl", "AAPL"}},
		{Name: "B", Symbols: []string{"nvda"}},
	}
	got := dedupeUnion(groups)
	if !reflect.DeepEqual(got, []string{"AAPL", "NVDA"}) {
		t.Errorf("dedupeUnion() = %v, want [AAPL NVDA]", got)
	}
}

// The earnings endpoint only answers for plain equity tickers. Crypto, FRED
// series and the index / futures / FX pseudo-tickers symbol.IsStock now
// routes to Yahoo would each cost a request and return nothing.
func TestStockTickersFiltersNonEquities(t *testing.T) {
	in := []string{"aapl", "BTC-USD", "FRED:DGS10", "^GSPC", "GC=F", "EURUSD=X", "brk-b"}
	got := stockTickers(in)
	want := []string{"AAPL", "BRK-B"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stockTickers() = %v, want %v", got, want)
	}
}

// MKT_RECORD points at a file the sink opens with O_TRUNC. An existing
// capture must survive the relaunch as a timestamped sibling.
func TestPreserveRecordingBacksUpExistingCapture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.ndjson")
	body := []byte(`{"symbol":"BTC-USD"}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := preserveRecording(path)
	if err != nil {
		t.Fatalf("preserveRecording: %v", err)
	}
	if backup == "" {
		t.Fatal("expected a backup path for a non-empty recording")
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("backup = %q, want %q", got, body)
	}
}

// Nothing to preserve means no backup — a first run must not litter the
// directory, and neither must a relaunch after an empty capture.
func TestPreserveRecordingSkipsMissingAndEmpty(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "absent.ndjson")
	if got, err := preserveRecording(missing); err != nil || got != "" {
		t.Errorf("missing file: got (%q, %v), want (\"\", nil)", got, err)
	}

	empty := filepath.Join(dir, "empty.ndjson")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := preserveRecording(empty); err != nil || got != "" {
		t.Errorf("empty file: got (%q, %v), want (\"\", nil)", got, err)
	}
}

// Two launches in the same second must not clobber each other's backup.
func TestPreserveRecordingDisambiguatesSameSecond(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.ndjson")

	var backups []string
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := preserveRecording(path)
		if err != nil {
			t.Fatalf("preserveRecording: %v", err)
		}
		backups = append(backups, b)
	}
	if backups[0] == backups[1] {
		t.Errorf("both launches wrote the same backup path %q", backups[0])
	}
}

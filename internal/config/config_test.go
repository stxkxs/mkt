package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{"empty returns zero", "", time.Time{}},
		{"garbage returns zero", "not-a-date", time.Time{}},
		{"RFC3339", "2026-05-24T13:45:00Z", time.Date(2026, 5, 24, 13, 45, 0, 0, time.UTC)},
		{"RFC3339-no-tz", "2026-05-24T13:45:00", time.Date(2026, 5, 24, 13, 45, 0, 0, time.UTC)},
		{"space separator", "2026-05-24 13:45:00", time.Date(2026, 5, 24, 13, 45, 0, 0, time.UTC)},
		{"date only", "2026-05-24", time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTime(tc.in)
			if !got.Equal(tc.want) {
				t.Errorf("ParseTime(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestPollDuration(t *testing.T) {
	t.Parallel()
	def, _ := time.ParseDuration(DefaultPollInterval)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"garbage", def},
		{"", def},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := (&Config{PollInterval: tc.in}).PollDuration()
			if got != tc.want {
				t.Errorf("PollDuration(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestAddSymbol(t *testing.T) {
	t.Parallel()
	c := &Config{Watchlist: []string{"AAPL", "BTC-USD"}}
	if !c.AddSymbol("TSLA") {
		t.Error("AddSymbol(TSLA): want true, got false")
	}
	if len(c.Watchlist) != 3 || c.Watchlist[2] != "TSLA" {
		t.Errorf("Watchlist after add: %v", c.Watchlist)
	}
	if c.AddSymbol("AAPL") {
		t.Error("AddSymbol(AAPL) duplicate: want false, got true")
	}
	if len(c.Watchlist) != 3 {
		t.Errorf("Watchlist length after duplicate add: %d", len(c.Watchlist))
	}
}

func TestRemoveSymbol(t *testing.T) {
	t.Parallel()
	c := &Config{Watchlist: []string{"AAPL", "BTC-USD", "TSLA"}}
	if !c.RemoveSymbol("BTC-USD") {
		t.Error("RemoveSymbol(BTC-USD): want true, got false")
	}
	if !reflect.DeepEqual(c.Watchlist, []string{"AAPL", "TSLA"}) {
		t.Errorf("Watchlist after remove: %v", c.Watchlist)
	}
	if c.RemoveSymbol("NOPE") {
		t.Error("RemoveSymbol(NOPE) absent: want false, got true")
	}
}

// AddSymbol canonicalizes on the way in, which is what makes
// `mkt config add btc` put a symbol in the watchlist that the Coinbase feed
// will actually price.
func TestAddSymbolCanonicalizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		start []string
		add   string
		added bool
		want  []string
	}{
		{"lowercase stock", []string{}, "aapl", true, []string{"AAPL"}},
		{"bare crypto base", []string{}, "btc", true, []string{"BTC-USD"}},
		{"binance spelling", []string{}, "BTCUSDT", true, []string{"BTC-USD"}},
		{"surrounding space", []string{}, "  eth  ", true, []string{"ETH-USD"}},
		{"fred series", []string{}, "fred:dgs10", true, []string{"FRED:DGS10"}},
		{"duplicate in another spelling", []string{"BTC-USD"}, "btc", false, []string{"BTC-USD"}},
		{"duplicate of a stock", []string{"AAPL"}, "aapl", false, []string{"AAPL"}},
		{"blank is rejected", []string{"AAPL"}, "   ", false, []string{"AAPL"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Watchlist: append([]string(nil), tc.start...)}
			if got := c.AddSymbol(tc.add); got != tc.added {
				t.Errorf("AddSymbol(%q) = %v, want %v", tc.add, got, tc.added)
			}
			if !reflect.DeepEqual(c.Watchlist, tc.want) {
				t.Errorf("watchlist after AddSymbol(%q) = %v, want %v", tc.add, c.Watchlist, tc.want)
			}
		})
	}
}

func TestRemoveSymbolCanonicalizes(t *testing.T) {
	t.Parallel()
	c := &Config{Watchlist: []string{"AAPL", "BTC-USD", "FRED:DGS10"}}
	if !c.RemoveSymbol("btc") {
		t.Error("RemoveSymbol(btc) should drop BTC-USD")
	}
	if !c.RemoveSymbol("fred:dgs10") {
		t.Error("RemoveSymbol(fred:dgs10) should drop FRED:DGS10")
	}
	if !reflect.DeepEqual(c.Watchlist, []string{"AAPL"}) {
		t.Errorf("watchlist = %v, want [AAPL]", c.Watchlist)
	}
	if c.RemoveSymbol("") {
		t.Error("RemoveSymbol(\"\") should be a no-op")
	}
}

// Everything the config addresses by symbol is canonicalized on load, so a
// hand-edited file lines up with the quotes flowing through the hub.
func TestLoadCanonicalizesEverySymbol(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)
	cfgDir := filepath.Join(dir, ".config", "mkt")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `watchlist:
  - btc
  - aapl
  - BTC-USD
watchlists:
  - name: Mine
    symbols: [eth, msft]
alerts:
  - symbol: btcusdt
    condition: above
    value: 100000
    enabled: true
portfolios:
  - name: Mine
    holdings:
      - symbol: sol
        quantity: 10
        cost_basis: 100
    transactions:
      - type: buy
        symbol: sol
        quantity: 10
        price: 100
notes:
  btc: digital gold
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// "btc" and "BTC-USD" collapse onto one entry, in first-seen order.
	if !reflect.DeepEqual(cfg.Watchlist, []string{"BTC-USD", "AAPL"}) {
		t.Errorf("watchlist = %v, want [BTC-USD AAPL]", cfg.Watchlist)
	}
	if len(cfg.Watchlists) != 1 || !reflect.DeepEqual(cfg.Watchlists[0].Symbols, []string{"ETH-USD", "MSFT"}) {
		t.Errorf("watchlist group = %+v", cfg.Watchlists)
	}
	if len(cfg.Alerts) != 1 || cfg.Alerts[0].Symbol != "BTC-USD" {
		t.Errorf("alert symbol = %+v", cfg.Alerts)
	}
	if len(cfg.Portfolios) != 1 {
		t.Fatalf("portfolios = %+v", cfg.Portfolios)
	}
	if got := cfg.Portfolios[0].Holdings[0].Symbol; got != "SOL-USD" {
		t.Errorf("holding symbol = %q, want SOL-USD", got)
	}
	if got := cfg.Portfolios[0].Transactions[0].Symbol; got != "SOL-USD" {
		t.Errorf("transaction symbol = %q, want SOL-USD", got)
	}
	if cfg.Notes["BTC-USD"] != "digital gold" {
		t.Errorf("notes = %+v, want the note keyed BTC-USD", cfg.Notes)
	}
}

func TestCanonicalSymbols(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil stays nil", nil, nil},
		{"canonicalizes", []string{"btc", "aapl"}, []string{"BTC-USD", "AAPL"}},
		{"dedupes across spellings", []string{"btc", "BTC-USD", "BTCUSDT"}, []string{"BTC-USD"}},
		{"drops blanks", []string{"AAPL", "", "  "}, []string{"AAPL"}},
		{"preserves order", []string{"c", "b", "a"}, []string{"C", "B", "A"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalSymbols(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("canonicalSymbols(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestLoadCreatesDefaultsWhenMissing isolates HOME to a tempdir so the
// real ~/.config/mkt/config.yaml is not touched. The first Load on a
// fresh dir writes defaults; the second Load reads what was written.
func TestLoadCreatesDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if len(cfg.Watchlist) == 0 {
		t.Error("default watchlist should be non-empty")
	}
	if cfg.Theme != DefaultTheme {
		t.Errorf("theme: got %q, want %q", cfg.Theme, DefaultTheme)
	}

	// Directory must exist with 0o700 (private — holdings + alerts).
	want := filepath.Join(dir, ".config", "mkt")
	st, err := os.Stat(want)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o700 {
		t.Errorf("config dir perm: got %o, want 0700", mode)
	}
}

// TestLoadWritesConfigFileOnFirstRun guards the first-run write: with
// SetConfigFile, a missing file surfaces as os.ErrNotExist (not viper's
// ConfigFileNotFoundError), so Load must handle both and actually create
// config.yaml — otherwise the seeded defaults never reach disk.
func TestLoadWritesConfigFileOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)

	if _, err := Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}
	p := filepath.Join(dir, ".config", "mkt", "config.yaml")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("first-run Load must write %s: %v", p, err)
	}
	for _, key := range []string{"watchlists:", "alerts:", "edgar_tickers:", "notes:", "portfolios:"} {
		if !contains(raw, key) {
			t.Errorf("written config.yaml missing seeded section %q", key)
		}
	}
}

// TestExistingConfigNotSeeded verifies the upgrade path: an existing
// config that predates the rich seed (no watchlists/alerts/edgar/notes)
// must NOT have those sections injected, and the file must not be
// rewritten on load. Only fresh installs get seeded.
func TestExistingConfigNotSeeded(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)
	cfgDir := filepath.Join(dir, ".config", "mkt")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "config.yaml")
	// A minimal pre-seed config: the user's own watchlist + theme only.
	body := "watchlist:\n  - AAPL\n  - BTC-USD\ntheme: nord\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Rich seed must NOT be injected into an existing config.
	if len(cfg.Watchlists) != 0 {
		t.Errorf("existing config got watchlist groups injected: %+v", cfg.Watchlists)
	}
	if len(cfg.Alerts) != 0 {
		t.Errorf("existing config got alerts injected: %+v", cfg.Alerts)
	}
	if len(cfg.EDGARTickers) != 0 {
		t.Errorf("existing config got edgar_tickers injected: %v", cfg.EDGARTickers)
	}
	if len(cfg.Notes) != 0 {
		t.Errorf("existing config got notes injected: %v", cfg.Notes)
	}
	// The user's own values survive; operational fallbacks still apply.
	if cfg.Theme != "nord" {
		t.Errorf("theme: got %q, want nord", cfg.Theme)
	}
	if len(cfg.Watchlist) != 2 {
		t.Errorf("watchlist: got %v, want the user's 2 symbols", cfg.Watchlist)
	}
	// Load must never rewrite an existing file.
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("Load rewrote an existing config file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestConfigFileWritten0600(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, ".config", "mkt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml perm = %o, want 0600 (holds secrets)", perm)
	}
}

func TestProvidersDefaultOnAndDisable(t *testing.T) {
	var p Providers // zero value = all nil
	if !p.BinanceOn() || !p.DeFiLlamaOn() || !p.NewsOn() || !p.MacroOn() {
		t.Error("nil provider pointers should default on")
	}
	no := false
	p.Binance = &no
	if p.BinanceOn() {
		t.Error("providers.binance=false should disable")
	}
	if !p.NewsOn() {
		t.Error("disabling one provider must not affect others")
	}
}

// TestSaveRoundTripWatchlistsNotesServe verifies the sections Save now
// persists (previously dropped) survive a rewrite by `mkt config set`.
func TestSaveRoundTripWatchlistsNotesServe(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".config", "mkt"), 0o700); err != nil {
		t.Fatal(err)
	}

	original := &Config{
		Watchlist:    []string{"AAPL"},
		Watchlists:   []Watchlist{{Name: "Tech", Symbols: []string{"AAPL", "MSFT"}}},
		Notes:        map[string]string{"AAPL": "flagship note"},
		Serve:        ServeConfig{Addr: "0.0.0.0:2222", AuthorizedKeys: []string{"ssh-ed25519 AAAAC3 you@host"}},
		PollInterval: "15s",
		SparklineLen: 60,
		Theme:        "tokyonight",
	}
	if err := Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if len(got.Watchlists) != 1 || got.Watchlists[0].Name != "Tech" || len(got.Watchlists[0].Symbols) != 2 {
		t.Errorf("watchlists round-trip: got %+v", got.Watchlists)
	}
	if got.Notes["AAPL"] != "flagship note" {
		t.Errorf("notes round-trip: got %+v", got.Notes)
	}
	if got.Serve.Addr != "0.0.0.0:2222" || len(got.Serve.AuthorizedKeys) != 1 {
		t.Errorf("serve round-trip: got %+v", got.Serve)
	}
}

// TestSaveRoundTrip writes a config, reloads it, and verifies the
// fields that Save persists round-trip cleanly.
func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)

	// Pre-create the config dir so Save's WriteConfig has somewhere to land.
	if err := os.MkdirAll(filepath.Join(dir, ".config", "mkt"), 0o700); err != nil {
		t.Fatal(err)
	}

	original := &Config{
		Watchlist:    []string{"NVDA", "AMD"},
		PollInterval: "20s",
		SparklineLen: 90,
		Theme:        "gruvbox",
		WebhookURL:   "https://example.invalid/hook",
		NtfyTopic:    "mkt-test",
		EDGARTickers: []string{"NVDA"},
		Portfolios: []Portfolio{
			{Name: "Core", Holdings: []Holding{{Symbol: "NVDA", Quantity: 5, CostBasis: 100}}},
		},
		Alerts: []AlertRule{
			{Symbol: "NVDA", Condition: "above", Value: 200, Enabled: true},
		},
	}
	if err := Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !reflect.DeepEqual(got.Watchlist, original.Watchlist) {
		t.Errorf("watchlist: got %v, want %v", got.Watchlist, original.Watchlist)
	}
	if got.PollInterval != original.PollInterval {
		t.Errorf("poll_interval: got %q, want %q", got.PollInterval, original.PollInterval)
	}
	if got.SparklineLen != original.SparklineLen {
		t.Errorf("sparkline_len: got %d, want %d", got.SparklineLen, original.SparklineLen)
	}
	if got.Theme != original.Theme {
		t.Errorf("theme: got %q, want %q", got.Theme, original.Theme)
	}
	if got.WebhookURL != original.WebhookURL {
		t.Errorf("webhook_url: got %q, want %q", got.WebhookURL, original.WebhookURL)
	}
	if got.NtfyTopic != original.NtfyTopic {
		t.Errorf("ntfy_topic: got %q, want %q", got.NtfyTopic, original.NtfyTopic)
	}
	if !reflect.DeepEqual(got.EDGARTickers, original.EDGARTickers) {
		t.Errorf("edgar_tickers: got %v, want %v", got.EDGARTickers, original.EDGARTickers)
	}
	if len(got.Portfolios) != 1 || got.Portfolios[0].Name != "Core" {
		t.Errorf("portfolios round-trip: got %+v", got.Portfolios)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].Symbol != "NVDA" {
		t.Errorf("alerts round-trip: got %+v", got.Alerts)
	}
}

// Save should omit empty optional secrets so the persisted YAML stays
// minimal and doesn't accidentally create empty keys in the user's
// file.
func TestSaveOmitsEmptyOptionalFields(t *testing.T) {
	dir := t.TempDir()
	isolateAt(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, ".config", "mkt"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Watchlist:    []string{"AAPL"},
		PollInterval: "15s",
		SparklineLen: 60,
		Theme:        "tokyonight",
		// No webhook URL, ntfy, pushover, EDGAR tickers.
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".config", "mkt", "config.yaml"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, key := range []string{"webhook_url", "ntfy_topic", "ntfy_server", "pushover_user", "pushover_token", "edgar_tickers"} {
		if contains(raw, key) {
			t.Errorf("yaml should not contain %q when value is empty; got:\n%s", key, raw)
		}
	}
}

func contains(haystack []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}

// TestConfigDirOverrideWinsOverHome pins the property the whole test suite
// relies on for isolation, and that a user relies on to relocate their
// config: MKT_CONFIG_DIR is authoritative and does not consult the home
// directory at all. Setting HOME is not portable — os.UserHomeDir reads
// %USERPROFILE% on Windows — so the override is what makes isolation work
// on every platform.
func TestConfigDirOverrideWinsOverHome(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("HOME", "/nonexistent-home")
	t.Setenv("USERPROFILE", "/nonexistent-home")
	t.Setenv(DirEnv, elsewhere)

	if got := ConfigDir(); got != elsewhere {
		t.Errorf("ConfigDir() = %q, want the override %q", got, elsewhere)
	}
}

// An empty or whitespace-only override falls back to the home directory
// rather than resolving to "" and writing into the working directory.
func TestConfigDirIgnoresBlankOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	for _, blank := range []string{"", "   "} {
		t.Setenv(DirEnv, blank)
		want := filepath.Join(home, ".config", "mkt")
		if got := ConfigDir(); got != want {
			t.Errorf("ConfigDir() with override %q = %q, want %q", blank, got, want)
		}
	}
}

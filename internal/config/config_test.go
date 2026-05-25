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

// TestLoadCreatesDefaultsWhenMissing isolates HOME to a tempdir so the
// real ~/.config/mkt/config.yaml is not touched. The first Load on a
// fresh dir writes defaults; the second Load reads what was written.
func TestLoadCreatesDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

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

// TestSaveRoundTrip writes a config, reloads it, and verifies the
// fields that Save persists round-trip cleanly.
func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

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
	t.Setenv("HOME", dir)
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

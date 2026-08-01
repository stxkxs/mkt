package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/stxkxs/mkt/internal/symbol"
	"gopkg.in/yaml.v3"
)

// Holding represents a portfolio position from config.
type Holding struct {
	Symbol    string  `mapstructure:"symbol" yaml:"symbol"`
	Name      string  `mapstructure:"name" yaml:"name"`
	Quantity  float64 `mapstructure:"quantity" yaml:"quantity"`
	CostBasis float64 `mapstructure:"cost_basis" yaml:"cost_basis"`
}

// Transaction is a buy or sell event for an optional transaction log.
// Time is parsed lazily by callers (see ParseTime); zero means "unset".
type Transaction struct {
	Type     string  `mapstructure:"type" yaml:"type"` // "buy" or "sell"
	Symbol   string  `mapstructure:"symbol" yaml:"symbol"`
	Quantity float64 `mapstructure:"quantity" yaml:"quantity"`
	Price    float64 `mapstructure:"price" yaml:"price"`
	Time     string  `mapstructure:"time,omitempty" yaml:"time,omitempty"`
	Fee      float64 `mapstructure:"fee,omitempty" yaml:"fee,omitempty"`
	Note     string  `mapstructure:"note,omitempty" yaml:"note,omitempty"`
}

// Portfolio is a named collection of holdings and optional transactions.
type Portfolio struct {
	Name         string        `mapstructure:"name" yaml:"name"`
	Holdings     []Holding     `mapstructure:"holdings" yaml:"holdings,omitempty"`
	Transactions []Transaction `mapstructure:"transactions,omitempty" yaml:"transactions,omitempty"`
	TaxMethod    string        `mapstructure:"tax_method,omitempty" yaml:"tax_method,omitempty"` // fifo | lifo | hifo (empty = weighted average)
}

// ParseTime accepts a few common YAML date layouts. Returns the zero
// time on empty input or any parse failure; callers should treat the
// zero time as "unset" rather than as an error.
func ParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// AlertSubCondition is one leaf inside a compound alert rule.
type AlertSubCondition struct {
	Condition string  `mapstructure:"condition" yaml:"condition"`
	Value     float64 `mapstructure:"value" yaml:"value"`
	Period    int     `mapstructure:"period,omitempty" yaml:"period,omitempty"`
}

// AlertRule represents a saved alert from config. When Conditions is
// non-empty the legacy Condition / Value / Period fields are ignored
// and the rule is evaluated as a compound according to Match.
type AlertRule struct {
	Symbol     string              `mapstructure:"symbol" yaml:"symbol"`
	Condition  string              `mapstructure:"condition,omitempty" yaml:"condition,omitempty"`
	Value      float64             `mapstructure:"value,omitempty" yaml:"value,omitempty"`
	Period     int                 `mapstructure:"period,omitempty" yaml:"period,omitempty"`
	Enabled    bool                `mapstructure:"enabled" yaml:"enabled"`
	Webhooks   []string            `mapstructure:"webhooks,omitempty" yaml:"webhooks,omitempty"`
	Conditions []AlertSubCondition `mapstructure:"conditions,omitempty" yaml:"conditions,omitempty"`
	Match      string              `mapstructure:"match,omitempty" yaml:"match,omitempty"` // all | any | sequence
}

// Watchlist is a named group of symbols. Multiple groups can be defined
// under `watchlists:` and switched at runtime with `[` / `]` on the
// watchlist tab.
type Watchlist struct {
	Name    string   `mapstructure:"name" yaml:"name"`
	Symbols []string `mapstructure:"symbols" yaml:"symbols"`
}

// ServeConfig configures `mkt serve`, the Wish SSH dashboard. Access is
// gated by a public-key allowlist: connections are refused unless the
// client key appears in AuthorizedKeys or AuthorizedKeysFile. The command
// refuses to start with an empty allowlist rather than serve openly.
type ServeConfig struct {
	Addr               string   `mapstructure:"addr,omitempty" yaml:"addr,omitempty"`                                 // SSH bind, e.g. 127.0.0.1:2222 or 0.0.0.0:2222
	HostKey            string   `mapstructure:"host_key,omitempty" yaml:"host_key,omitempty"`                         // path to the SSH host key (auto-generated if missing)
	AuthorizedKeys     []string `mapstructure:"authorized_keys,omitempty" yaml:"authorized_keys,omitempty"`           // inline allowlist of public keys
	AuthorizedKeysFile string   `mapstructure:"authorized_keys_file,omitempty" yaml:"authorized_keys_file,omitempty"` // optional path to an authorized_keys file
}

// Providers gates the optional background data feeds so a locked-down or
// geo-restricted deployment can cut egress it doesn't want. A nil pointer
// means "default on" — the core Coinbase/Yahoo price feeds are always on
// and are not gated here. Set to false in config to disable.
type Providers struct {
	Binance   *bool `mapstructure:"binance,omitempty" yaml:"binance,omitempty"`     // Binance futures funding/OI poll
	DeFiLlama *bool `mapstructure:"defillama,omitempty" yaml:"defillama,omitempty"` // DeFiLlama TVL poll
	News      *bool `mapstructure:"news,omitempty" yaml:"news,omitempty"`           // RSS feeds + SEC EDGAR
	Macro     *bool `mapstructure:"macro,omitempty" yaml:"macro,omitempty"`         // Yahoo macro-index poll
}

func on(p *bool) bool { return p == nil || *p }

// BinanceOn / DeFiLlamaOn / NewsOn / MacroOn report whether each optional
// provider is enabled (default on when unset).
func (p Providers) BinanceOn() bool   { return on(p.Binance) }
func (p Providers) DeFiLlamaOn() bool { return on(p.DeFiLlama) }
func (p Providers) NewsOn() bool      { return on(p.News) }
func (p Providers) MacroOn() bool     { return on(p.Macro) }

// NewsFeed is a named RSS source. When Config.NewsFeeds is non-empty it
// replaces the built-in DefaultFeeds, letting a team drop or swap feeds.
type NewsFeed struct {
	Name string `mapstructure:"name" yaml:"name"`
	URL  string `mapstructure:"url" yaml:"url"`
}

// Config is the application configuration.
type Config struct {
	Watchlist     []string          `mapstructure:"watchlist" yaml:"watchlist"`
	Watchlists    []Watchlist       `mapstructure:"watchlists,omitempty" yaml:"watchlists,omitempty"`
	Portfolios    []Portfolio       `mapstructure:"portfolios" yaml:"portfolios"`
	Alerts        []AlertRule       `mapstructure:"alerts" yaml:"alerts"`
	PollInterval  string            `mapstructure:"poll_interval" yaml:"poll_interval"`
	SparklineLen  int               `mapstructure:"sparkline_len" yaml:"sparkline_len"`
	Theme         string            `mapstructure:"theme" yaml:"theme"`
	WebhookURL    string            `mapstructure:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`
	NtfyTopic     string            `mapstructure:"ntfy_topic,omitempty" yaml:"ntfy_topic,omitempty"`
	NtfyServer    string            `mapstructure:"ntfy_server,omitempty" yaml:"ntfy_server,omitempty"`
	PushoverUser  string            `mapstructure:"pushover_user,omitempty" yaml:"pushover_user,omitempty"`
	PushoverToken string            `mapstructure:"pushover_token,omitempty" yaml:"pushover_token,omitempty"`
	EDGARTickers  []string          `mapstructure:"edgar_tickers,omitempty" yaml:"edgar_tickers,omitempty"`
	Notes         map[string]string `mapstructure:"notes,omitempty" yaml:"notes,omitempty"` // per-symbol freeform notes (markdown plaintext)
	Serve         ServeConfig       `mapstructure:"serve,omitempty" yaml:"serve,omitempty"`
	DesktopNotify *bool             `mapstructure:"desktop_notify,omitempty" yaml:"desktop_notify,omitempty"` // nil = on (dashboard) / off (serve)
	Notifications *bool             `mapstructure:"notifications,omitempty" yaml:"notifications,omitempty"`   // master switch, nil = on
	Providers     Providers         `mapstructure:"providers,omitempty" yaml:"providers,omitempty"`
	NewsFeeds     []NewsFeed        `mapstructure:"news_feeds,omitempty" yaml:"news_feeds,omitempty"`
}

// DirEnv overrides the config directory. It exists so a caller can point mkt
// at a specific directory without depending on how the host resolves a home
// directory — which is not portable: os.UserHomeDir reads $HOME on Unix but
// %USERPROFILE% on Windows, so setting $HOME alone relocates nothing there.
// Tests rely on this to stay isolated on every platform; before it existed,
// the config tests ran against the developer's real ~/.config/mkt on Windows
// and wrote backups into it.
const DirEnv = "MKT_CONFIG_DIR"

// ConfigDir returns the application's config / data directory path.
//
// MKT_CONFIG_DIR wins when set and non-empty; otherwise it is
// <home>/.config/mkt. The XDG base-directory spec is deliberately not
// consulted: honoring XDG_CONFIG_HOME now would silently relocate the config
// of every existing user who has it set.
func ConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv(DirEnv)); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mkt")
}

// configPath returns the config file path.
func configPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// LoadResult reports how the config file was resolved, so a caller can tell
// a healthy load apart from one that fell back to defaults because the file
// on disk is broken. Degraded is the case that matters: the file exists but
// does not parse, Config holds defaults rather than the user's settings, and
// every write must be refused until the file is repaired (see SaveSafely).
type LoadResult struct {
	Config   *Config
	Degraded bool  // file existed but could not be parsed; Config holds defaults
	Err      error // the parse error, nil unless Degraded
	Path     string
	Line     int // best-effort 1-based line from the YAML error; 0 if unknown
}

// Load reads the config file, creating defaults if it doesn't exist. A file
// that exists but cannot be parsed is not an error here — the dashboard
// still starts on defaults. Callers that need to know about that (anything
// that will later write, or wants to warn the user) should use
// LoadWithResult instead.
func Load() (*Config, error) {
	res, err := LoadWithResult()
	if err != nil {
		return nil, err
	}
	return res.Config, nil
}

// LoadWithResult reads the config file and reports how it went. There are
// exactly three outcomes:
//
//   - the file is absent: defaults are seeded and written, Degraded is false;
//   - the file parses: the user's settings are returned, Degraded is false;
//   - the file exists but fails to parse: Config holds defaults, Degraded is
//     true, and Err / Path / Line describe the failure.
//
// The error return is reserved for problems that make the config unusable
// (an unwritable config dir, a decode failure against the struct) — a bad
// file on disk is reported through LoadResult, not through err.
func LoadWithResult() (*LoadResult, error) {
	dir := ConfigDir()
	// 0o700: holdings and alert rules are user-private; don't expose to other local users.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	// The config dir may pre-exist at loose perms (another tool, a restored
	// tar/rsync); MkdirAll won't tighten it, so chmod explicitly. config.yaml
	// holds holdings + bearer/pushover/webhook secrets — keep it private.
	_ = os.Chmod(dir, 0o700)

	path := configPath()
	res := &LoadResult{Path: path}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetConfigPermissions(0o600) // secrets at rest — not world-readable

	// Operational fallbacks — always active so any run has sane values even
	// when a hand-edited config omits them. These mirror the long-standing
	// pre-seed defaults and are safe to inject into an existing config.
	v.SetDefault("watchlist", DefaultWatchlist)
	v.SetDefault("poll_interval", DefaultPollInterval)
	v.SetDefault("sparkline_len", DefaultSparklineLen)
	v.SetDefault("theme", DefaultTheme)
	v.SetDefault("portfolios", DefaultPortfolios)

	if err := v.ReadInConfig(); err != nil {
		// With SetConfigFile, a missing file surfaces as os.ErrNotExist (a
		// *fs.PathError), NOT viper's ConfigFileNotFoundError — handle both.
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			// Fresh install ONLY: seed the rich content (grouped watchlists,
			// example alerts, EDGAR tickers, notes) and persist a full
			// config.yaml the user can see and edit. We deliberately do NOT
			// inject these sections into a config that already exists — an
			// upgrader keeps their file exactly as-is.
			v.SetDefault("watchlists", DefaultWatchlists)
			v.SetDefault("alerts", DefaultAlerts)
			v.SetDefault("edgar_tickers", DefaultEDGARTickers)
			v.SetDefault("notes", DefaultNotes)
			if werr := writeSeed(path, v.AllSettings()); werr != nil {
				fmt.Fprintf(os.Stderr, "config: could not write default config to %s: %v\n", path, werr)
			}
		} else {
			// The file is there but unreadable or unparseable. Fall through
			// with defaults so the dashboard still starts, but record it so
			// the caller can warn and so every write gets refused.
			res.Degraded = true
			res.Err = err
			res.Line = yamlErrorLine(err)
		}
	}
	// Correct perms on an existing file that may have been written loose by
	// an older version (viper historically wrote 0644).
	tightenPerms()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.normalize()

	res.Config = &cfg
	return res, nil
}

// yamlLine matches the "line N" that both viper and gopkg.in/yaml.v3 put in
// their parse errors. There is no structured position on the error type, so
// scraping the text is the only way to point the user at the offending line.
var yamlLine = regexp.MustCompile(`line (\d+)`)

// yamlErrorLine extracts a best-effort 1-based line number from a YAML parse
// error. Returns 0 when the error carries no position.
func yamlErrorLine(err error) int {
	if err == nil {
		return 0
	}
	m := yamlLine.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil || n < 0 {
		return 0
	}
	return n
}

// normalize rewrites every symbol the config carries into the canonical
// spelling the providers emit, so a hand-typed `btc`, `BTCUSDT` or `aapl`
// lines up with the quotes flowing through the hub instead of sitting in
// the watchlist forever showing no price. Watchlist entries are also
// de-duplicated, since two spellings can collapse onto one symbol.
//
// viper lowercases all map keys, so this doubles as the fix-up for note
// keys, which are symbols and are uppercase everywhere else.
func (c *Config) normalize() {
	c.Watchlist = canonicalSymbols(c.Watchlist)
	for i := range c.Watchlists {
		c.Watchlists[i].Symbols = canonicalSymbols(c.Watchlists[i].Symbols)
	}
	for i := range c.Alerts {
		c.Alerts[i].Symbol = symbol.Canonical(c.Alerts[i].Symbol)
	}
	for i := range c.Portfolios {
		p := &c.Portfolios[i]
		for j := range p.Holdings {
			p.Holdings[j].Symbol = symbol.Canonical(p.Holdings[j].Symbol)
		}
		for j := range p.Transactions {
			p.Transactions[j].Symbol = symbol.Canonical(p.Transactions[j].Symbol)
		}
	}
	if len(c.Notes) > 0 {
		notes := make(map[string]string, len(c.Notes))
		for k, v := range c.Notes {
			notes[symbol.Canonical(k)] = v
		}
		c.Notes = notes
	}
}

// canonicalSymbols canonicalizes a symbol list, dropping blanks and
// duplicates while preserving the user's ordering.
func canonicalSymbols(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		c := symbol.Canonical(s)
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

// tightenPerms best-effort restricts the config dir to 0700 and config.yaml
// to 0600 (holdings + secret tokens live there). Called after every write
// and on load so a pre-existing loose dir/file is corrected — mirrors how
// the SSH host key is handled.
func tightenPerms() {
	_ = os.Chmod(ConfigDir(), 0o700)
	_ = os.Chmod(configPath(), 0o600)
}

// Save writes the config to disk. It is SaveSafely with AssumeYes set: the
// write is atomic, a timestamped backup is taken first, and a config file
// that does not parse is protected (ErrWouldDestroy) — but the user is never
// prompted, so existing callers keep their non-interactive behavior.
func Save(cfg *Config) error {
	_, err := SaveSafely(cfg, SaveOptions{AssumeYes: true})
	return err
}

// encodeConfig renders cfg as the YAML that belongs on disk and returns both
// the bytes and the settings map they were built from. The settings are
// assembled through viper so key naming and omissions match what Load reads
// back, but the marshalling happens here because the bytes have to go to the
// atomic writer rather than to viper's in-place rewrite.
func encodeConfig(cfg *Config) ([]byte, map[string]any, error) {
	v := viper.New()

	v.Set("watchlist", cfg.Watchlist)
	if len(cfg.Watchlists) > 0 {
		v.Set("watchlists", cfg.Watchlists)
	}
	v.Set("portfolios", cfg.Portfolios)
	v.Set("alerts", cfg.Alerts)
	v.Set("poll_interval", cfg.PollInterval)
	v.Set("sparkline_len", cfg.SparklineLen)
	v.Set("theme", cfg.Theme)
	if cfg.WebhookURL != "" {
		v.Set("webhook_url", cfg.WebhookURL)
	}
	if cfg.NtfyTopic != "" {
		v.Set("ntfy_topic", cfg.NtfyTopic)
	}
	if cfg.NtfyServer != "" {
		v.Set("ntfy_server", cfg.NtfyServer)
	}
	if cfg.PushoverUser != "" {
		v.Set("pushover_user", cfg.PushoverUser)
	}
	if cfg.PushoverToken != "" {
		v.Set("pushover_token", cfg.PushoverToken)
	}
	if len(cfg.EDGARTickers) > 0 {
		v.Set("edgar_tickers", cfg.EDGARTickers)
	}
	if len(cfg.Notes) > 0 {
		v.Set("notes", cfg.Notes)
	}
	// Persist the SSH serve block so a `mkt config set` doesn't drop a
	// hand-edited serve config (Save rebuilds the file from scratch).
	if cfg.Serve.Addr != "" || cfg.Serve.HostKey != "" || len(cfg.Serve.AuthorizedKeys) > 0 || cfg.Serve.AuthorizedKeysFile != "" {
		v.Set("serve", cfg.Serve)
	}
	// Hardening toggles — persist only when explicitly set so an untouched
	// config stays minimal.
	if cfg.DesktopNotify != nil {
		v.Set("desktop_notify", *cfg.DesktopNotify)
	}
	if cfg.Notifications != nil {
		v.Set("notifications", *cfg.Notifications)
	}
	if cfg.Providers != (Providers{}) {
		v.Set("providers", cfg.Providers)
	}
	if len(cfg.NewsFeeds) > 0 {
		v.Set("news_feeds", cfg.NewsFeeds)
	}

	settings := v.AllSettings()
	raw, err := yaml.Marshal(settings)
	if err != nil {
		return nil, nil, fmt.Errorf("encode config: %w", err)
	}
	return raw, settings, nil
}

// writeSeed persists the first-run defaults. Split out so the fresh-install
// path in LoadWithResult goes through the same atomic writer as every other
// write — a crash while seeding must not leave a half-written config.
func writeSeed(path string, settings map[string]any) error {
	raw, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode default config: %w", err)
	}
	if err := writeAtomic(path, raw, 0o600); err != nil {
		return err
	}
	return nil
}

// PollDuration parses the poll interval as a duration.
func (c *Config) PollDuration() time.Duration {
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		d, _ = time.ParseDuration(DefaultPollInterval)
	}
	return d
}

// AddSymbol adds a symbol to the watchlist if not already present. The
// symbol is canonicalized first, so `mkt config add btc` stores BTC-USD and
// a second `mkt config add BTCUSDT` is correctly recognized as a duplicate.
// A symbol that canonicalizes to nothing (blank input) is rejected.
func (c *Config) AddSymbol(sym string) bool {
	want := symbol.Canonical(sym)
	if want == "" {
		return false
	}
	for _, s := range c.Watchlist {
		if symbol.Canonical(s) == want {
			return false
		}
	}
	c.Watchlist = append(c.Watchlist, want)
	return true
}

// RemoveSymbol removes a symbol from the watchlist. Matching is done on the
// canonical spelling, so `mkt config remove btc` drops a BTC-USD entry.
func (c *Config) RemoveSymbol(sym string) bool {
	want := symbol.Canonical(sym)
	if want == "" {
		return false
	}
	for i, s := range c.Watchlist {
		if symbol.Canonical(s) == want {
			c.Watchlist = append(c.Watchlist[:i], c.Watchlist[i+1:]...)
			return true
		}
	}
	return false
}

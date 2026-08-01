package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/broadcast"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

// scriptedProvider replays a fixed list of quotes and then parks until the
// context is cancelled, standing in for a live venue.
type scriptedProvider struct {
	quotes []provider.Quote
}

func (p *scriptedProvider) Name() string         { return "scripted" }
func (p *scriptedProvider) Supports(string) bool { return true }
func (p *scriptedProvider) Subscribe(ctx context.Context, _ []string, out chan<- provider.Quote) error {
	for _, q := range p.quotes {
		select {
		case out <- q:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// blockedProvider claims nothing, so the hub reports its symbols as
// unroutable.
type blockedProvider struct{}

func (blockedProvider) Name() string         { return "blocked" }
func (blockedProvider) Supports(string) bool { return false }
func (blockedProvider) Subscribe(ctx context.Context, _ []string, _ chan<- provider.Quote) error {
	<-ctx.Done()
	return ctx.Err()
}

func boolPtr(b bool) *bool { return &b }

// newTestBackend assembles a backend around qp with every optional network
// poller switched off, so startDataPlane exercises the wiring and nothing
// else.
func newTestBackend(t *testing.T, qp provider.QuoteProvider, symbols []string, engine *alert.Engine) *backend {
	t.Helper()
	dir := t.TempDir()
	cache := market.NewCache(16)
	cfg := &config.Config{
		SparklineLen: 16,
		PollInterval: "1h",
		Providers: config.Providers{
			Binance:   boolPtr(false),
			DeFiLlama: boolPtr(false),
			News:      boolPtr(false),
			Macro:     boolPtr(false),
		},
	}
	return &backend{
		cfg:          cfg,
		symbols:      symbols,
		cache:        cache,
		hub:          market.NewHub(cache, qp),
		histProvider: market.NewMultiHistoryProvider(),
		alertEngine:  engine,
		yahooProv:    yahoo.New(time.Hour),
		coinbaseProv: coinbase.New(),
		bc:           broadcast.New(),
		equityFile:   portfolio.NewEquityFile(filepath.Join(dir, "equity.ndjson"), 10),
		writable:     false,
	}
}

// Alert evaluation must ride the hub's observer path, which never drops.
// On the dispatch path a quote can be discarded under TUI back-pressure,
// which silently loses a level crossing.
func TestStartDataPlaneEvaluatesAlertsOnReliablePath(t *testing.T) {
	fired := make(chan alert.TriggeredAlert, 4)
	engine := alert.NewEngine(0, func(a alert.TriggeredAlert) { fired <- a })
	engine.SetRules([]alert.Rule{{
		Symbol:    "BTC-USD",
		Condition: alert.CondAbove,
		Value:     150,
		Enabled:   true,
	}})

	qp := &scriptedProvider{quotes: []provider.Quote{
		{Symbol: "BTC-USD", Price: 100, Timestamp: time.Now()},
		{Symbol: "BTC-USD", Price: 200, Timestamp: time.Now()},
	}}
	b := newTestBackend(t, qp, []string{"BTC-USD"}, engine)
	engine.SetPriceSource(b.cache)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A dispatch consumer that never returns is exactly the back-pressure
	// that used to swallow alerts.
	b.hub.AddObserver(func(provider.Quote) {})
	b.startDataPlane(ctx)

	select {
	case a := <-fired:
		if a.Rule.Symbol != "BTC-USD" {
			t.Errorf("fired for %q, want BTC-USD", a.Rule.Symbol)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("alert never fired: Check is not on the hub's reliable observer path")
	}
}

// A symbol no provider claims used to vanish without a trace, which is
// indistinguishable from one that simply has not ticked yet.
func TestStartDataPlaneRecordsUnroutableSymbols(t *testing.T) {
	engine := alert.NewEngine(0, nil)
	b := newTestBackend(t, blockedProvider{}, []string{"APPL"}, engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startDataPlane(ctx)

	if len(b.unroutable) != 1 || b.unroutable[0] != "APPL" {
		t.Fatalf("unroutable = %v, want [APPL]", b.unroutable)
	}
}

// applyWiring hands the unroutable list and the degraded-config state to a
// model that accepts them, and leaves one that does not alone.
type wiringRecorder struct {
	ctx        context.Context
	bannerPath string
	bannerLine int
	bannerErr  error
	unroutable []string
}

func (w *wiringRecorder) SetContext(ctx context.Context) { w.ctx = ctx }
func (w *wiringRecorder) LoadConfigBanner(path string, line int, err error) {
	w.bannerPath, w.bannerLine, w.bannerErr = path, line, err
}
func (w *wiringRecorder) LoadUnroutableSymbols(symbols []string) { w.unroutable = symbols }

func TestApplyWiringDeliversDataPlaneState(t *testing.T) {
	b := &backend{
		degraded:   true,
		configErr:  os.ErrInvalid,
		configPath: "/tmp/config.yaml",
		configLine: 12,
		unroutable: []string{"APPL"},
	}
	ctx := context.Background()
	var rec wiringRecorder
	b.applyWiring(ctx, &rec)

	if rec.ctx != ctx {
		t.Error("SetContext was not called with the data-plane context")
	}
	if rec.bannerPath != "/tmp/config.yaml" || rec.bannerLine != 12 || rec.bannerErr == nil {
		t.Errorf("banner = (%q, %d, %v), want (/tmp/config.yaml, 12, non-nil)", rec.bannerPath, rec.bannerLine, rec.bannerErr)
	}
	if len(rec.unroutable) != 1 || rec.unroutable[0] != "APPL" {
		t.Errorf("unroutable = %v, want [APPL]", rec.unroutable)
	}
}

func TestApplyWiringSkipsAModelThatAcceptsNothing(t *testing.T) {
	b := &backend{degraded: true, unroutable: []string{"APPL"}}
	b.applyWiring(context.Background(), struct{}{}) // must not panic
}

// writeConfig seeds $HOME/.config/mkt/config.yaml with raw YAML.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := isolateHome(t, home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A config that does not parse must not stop the dashboard: mkt starts on
// defaults, records the parse error for the banner, and refuses every write
// so the user's real settings are not replaced by the fallback.
func TestSetupBackendStartsDegradedAndRefusesWrites(t *testing.T) {
	path := writeConfig(t, "watchlist: [AAPL\nportfolios: {{{\n")

	b, cleanup, err := setupBackend(backendOpts{noNotify: true, persistAlerts: true})
	if err != nil {
		t.Fatalf("setupBackend: %v", err)
	}
	defer cleanup()

	if !b.degraded {
		t.Fatal("degraded = false, want true for an unparseable config")
	}
	if b.writable {
		t.Error("writable = true; a degraded config must disable writes")
	}
	if b.configErr == nil {
		t.Error("configErr = nil, want the parse error")
	}
	if b.configPath != path {
		t.Errorf("configPath = %q, want %q", b.configPath, path)
	}
	if len(b.symbols) == 0 {
		t.Error("no symbols: the dashboard should still start on defaults")
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b.persistRules([]alert.Rule{{Symbol: "AAPL", Condition: alert.CondAbove, Value: 1, Enabled: true}})
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("persistRules rewrote a degraded config")
	}
}

// --force is the escape hatch: the user has decided the broken file is
// expendable, so writes are re-enabled (SaveSafely still backs it up).
func TestSetupBackendForceReenablesWrites(t *testing.T) {
	writeConfig(t, "watchlist: [AAPL\n")

	b, cleanup, err := setupBackend(backendOpts{noNotify: true, force: true})
	if err != nil {
		t.Fatalf("setupBackend: %v", err)
	}
	defer cleanup()

	if !b.degraded {
		t.Fatal("degraded = false, want true")
	}
	if !b.writable {
		t.Error("writable = false; --force must re-enable writes")
	}
}

// A healthy config stays writable, and rules edited in the TUI round-trip
// back to disk — without this every alert created in the dashboard is lost
// on exit.
func TestPersistRulesRoundTrips(t *testing.T) {
	writeConfig(t, "watchlist:\n  - AAPL\nsparkline_len: 30\n")

	b, cleanup, err := setupBackend(backendOpts{noNotify: true, persistAlerts: true})
	if err != nil {
		t.Fatalf("setupBackend: %v", err)
	}
	defer cleanup()

	if b.degraded || !b.writable {
		t.Fatalf("degraded=%v writable=%v, want false/true for a healthy config", b.degraded, b.writable)
	}

	b.persistRules([]alert.Rule{{
		Symbol:    "NVDA",
		Condition: alert.CondBelow,
		Value:     100,
		Enabled:   true,
		Conditions: []alert.SubCondition{
			{Type: alert.CondRSIBelow, Value: 30, Period: 14},
		},
		Match: "all",
	}})

	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Alerts) != 1 {
		t.Fatalf("got %d persisted alerts, want 1", len(reloaded.Alerts))
	}
	got := reloaded.Alerts[0]
	if got.Symbol != "NVDA" || got.Condition != string(alert.CondBelow) || got.Value != 100 {
		t.Errorf("persisted rule = %+v, want NVDA below 100", got)
	}
	if len(got.Conditions) != 1 || got.Conditions[0].Period != 14 {
		t.Errorf("sub-conditions did not round-trip: %+v", got.Conditions)
	}
}

// Rule edits arrive in bursts (holding a key down a list of alerts); the
// debounce must collapse them into a single write of the final state.
func TestStartAlertPersistenceDebouncesAndWrites(t *testing.T) {
	path := writeConfig(t, "watchlist:\n  - AAPL\n")

	b, cleanup, err := setupBackend(backendOpts{noNotify: true, persistAlerts: true})
	if err != nil {
		t.Fatalf("setupBackend: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startAlertPersistence(ctx)

	for i := 0; i < 5; i++ {
		b.alertEngine.AddRule(alert.Rule{
			Symbol:    "AAPL",
			Condition: alert.CondAbove,
			Value:     float64(100 + i),
			Enabled:   true,
		})
	}

	// The file is read rather than config.Load()ed: a second load while the
	// writer goroutine is encoding would re-normalize the package-level
	// defaults the loaded Config still aliases.
	deadline := time.Now().Add(4 * alertPersistDebounce)
	for {
		raw, rerr := os.ReadFile(path)
		if rerr == nil && strings.Count(string(raw), string(alert.CondAbove)) == 5 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("rules were never persisted (last read: %v)", rerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Under `mkt serve` the engine is shared by every SSH session, so a guest
// must not be able to rewrite the host's config.
func TestServeModeDoesNotPersistAlerts(t *testing.T) {
	writeConfig(t, "watchlist:\n  - AAPL\n")

	b, cleanup, err := setupBackend(backendOpts{noNotify: true, serveMode: true})
	if err != nil {
		t.Fatalf("setupBackend: %v", err)
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startAlertPersistence(ctx)
	b.alertEngine.AddRule(alert.Rule{Symbol: "AAPL", Condition: alert.CondAbove, Value: 1, Enabled: true})

	time.Sleep(2 * alertPersistDebounce)
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Alerts) != 0 {
		t.Errorf("serve mode persisted %d alert(s); an SSH guest must not write the host's config", len(reloaded.Alerts))
	}
}

// Round-tripping through the config representation must not change a rule.
func TestEngineRulesConfigRulesRoundTrip(t *testing.T) {
	in := []config.AlertRule{{
		Symbol:    "BTC-USD",
		Condition: string(alert.CondPctUp),
		Value:     5,
		Period:    14,
		Enabled:   true,
		Webhooks:  []string{"https://example.test/hook"},
		Conditions: []config.AlertSubCondition{
			{Condition: string(alert.CondRSIAbove), Value: 70, Period: 14},
		},
		Match: "sequence",
	}}

	got := configRules(engineRules(in))
	if len(got) != 1 {
		t.Fatalf("got %d rules, want 1", len(got))
	}
	if got[0].Symbol != in[0].Symbol || got[0].Condition != in[0].Condition ||
		got[0].Value != in[0].Value || got[0].Period != in[0].Period ||
		got[0].Match != in[0].Match || got[0].Enabled != in[0].Enabled {
		t.Errorf("round trip = %+v, want %+v", got[0], in[0])
	}
	if len(got[0].Webhooks) != 1 || got[0].Webhooks[0] != in[0].Webhooks[0] {
		t.Errorf("webhooks = %v, want %v", got[0].Webhooks, in[0].Webhooks)
	}
	if len(got[0].Conditions) != 1 || got[0].Conditions[0] != in[0].Conditions[0] {
		t.Errorf("conditions = %+v, want %+v", got[0].Conditions, in[0].Conditions)
	}
}

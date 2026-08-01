package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/api"
	"github.com/stxkxs/mkt/internal/broadcast"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/news"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	"github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/defillama"
	"github.com/stxkxs/mkt/internal/provider/fred"
	"github.com/stxkxs/mkt/internal/provider/recording"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui"
	"github.com/stxkxs/mkt/internal/tui/theme"
	watchlistview "github.com/stxkxs/mkt/internal/tui/watchlist"
)

// backend is the shared data plane behind both `mkt` (one local program)
// and `mkt serve` (one program per SSH session). It owns the cache, hub,
// providers, alert engine, and portfolios, and fans every quote/update
// out through a broadcaster so any number of TUIs can attach to it.
//
// buildApp() constructs a fresh per-session model seeded with the same
// persisted history; startDataPlane() starts the shared goroutines once.
type backend struct {
	cfg          *config.Config
	groups       []watchlistview.Group
	symbols      []string
	cache        *market.Cache
	hub          *market.Hub
	histProvider *market.MultiHistoryProvider
	portfolios   []portfolio.Portfolio
	alertEngine  *alert.Engine
	yahooProv    *yahoo.Provider
	coinbaseProv *coinbase.Provider
	bc           *broadcast.Broadcaster
	equityFile   *portfolio.EquityFile
	opts         backendOpts

	// config health. A file that exists but does not parse does not stop
	// the dashboard — it starts on defaults — but every write is refused
	// until it is repaired, and the TUI shows a persistent banner.
	degraded   bool
	configErr  error
	configPath string
	configLine int
	writable   bool // config writes permitted (not degraded, or --force)

	// unroutable holds the symbols no provider claimed, filled in by
	// startDataPlane and seeded into every session's model.
	unroutable []string

	// state loaded once at setup and seeded into every session's model.
	baseEvents   []calendar.Event
	calEvents    []calendar.Event
	pastTriggers []alert.TriggeredAlert
	pastEquity   map[string][]portfolio.EquityMark
}

// backendOpts carries per-command hardening toggles (from CLI flags) into
// the shared setup: serve mode changes safe defaults, and the notify /
// webhook / token switches gate sensitive surfaces.
type backendOpts struct {
	serveMode       bool // running under `mkt serve` (SSH) — tightens defaults
	noNotify        bool // --no-notify: silence desktop + webhook + ntfy + pushover
	noDesktopNotify bool // --no-desktop-notify
	enableWebhook   bool // --enable-webhook: mount /webhook/tradingview (requires token)
	requireToken    bool // --require-token: force --listen-token even on loopback
	force           bool // --force: keep writing config even when the file on disk is degraded
	persistAlerts   bool // write alerts created/toggled/deleted in the TUI back to config
}

// registerDataPlaneFlags adds the flags the data plane reads. They are
// declared on the individual commands rather than as persistent flags on the
// root so the config subcommands (which own their own --force / --yes
// semantics) are unaffected.
func init() {
	forceUsage := "start and keep writing config even when the config file does not parse (a timestamped backup is taken before any write)"
	rootCmd.Flags().Bool("force", false, forceUsage)
	serveCmd.Flags().Bool("force", false, forceUsage)
	serveCmd.Flags().Bool("persist-alerts", false,
		"let SSH sessions write alerts they create/toggle/delete back to the host's config (off by default: a guest must not rewrite the host's config)")
}

// optsFromFlags reads the shared hardening flags off the command. serveMode
// is set by the caller (true only for `mkt serve`).
//
// Alert persistence follows the trust model of the surface: `mkt` and `mkt
// daemon` are the local user's own process and persist by default, while
// `mkt serve` shares one engine across N SSH sessions, so a guest's edit
// would silently rewrite the host's config — off unless --persist-alerts.
func optsFromFlags(cmd *cobra.Command, serveMode bool) backendOpts {
	o := backendOpts{serveMode: serveMode}
	o.noNotify, _ = cmd.Flags().GetBool("no-notify")
	o.noDesktopNotify, _ = cmd.Flags().GetBool("no-desktop-notify")
	o.enableWebhook, _ = cmd.Flags().GetBool("enable-webhook")
	o.requireToken, _ = cmd.Flags().GetBool("require-token")
	o.force, _ = cmd.Flags().GetBool("force")
	if serveMode {
		o.persistAlerts, _ = cmd.Flags().GetBool("persist-alerts")
	} else {
		o.persistAlerts = true
	}
	return o
}

// registerNotifiers wires the desktop + third-party notifiers onto engine,
// honoring the notification hardening toggles. The caller adds the history
// notifier (it owns the history file). Shared by the dashboard/serve backend
// and the daemon so every surface gates identically:
//   - --no-notify / notifications:false silences all of them (rules + history
//     still run);
//   - desktop is gated by --no-desktop-notify / desktop_notify and defaults
//     OFF under `mkt serve`.
func registerNotifiers(engine *alert.Engine, cfg *config.Config, opts backendOpts) {
	notifyOn := !opts.noNotify && (cfg.Notifications == nil || *cfg.Notifications)
	desktopOn := notifyOn && !opts.noDesktopNotify
	if cfg.DesktopNotify != nil {
		desktopOn = desktopOn && *cfg.DesktopNotify
	} else {
		desktopOn = desktopOn && !opts.serveMode
	}
	if desktopOn {
		engine.AddNotifier(alert.NewDesktopNotifier())
	}
	if !notifyOn {
		return
	}
	anyWebhook := cfg.WebhookURL != ""
	for _, r := range cfg.Alerts {
		if len(r.Webhooks) > 0 {
			anyWebhook = true
		}
	}
	if anyWebhook {
		engine.AddNotifier(alert.NewWebhookNotifier(cfg.WebhookURL))
	}
	if cfg.NtfyTopic != "" {
		engine.AddNotifier(alert.NewNtfyNotifier(cfg.NtfyServer, cfg.NtfyTopic))
	}
	if cfg.PushoverUser != "" && cfg.PushoverToken != "" {
		engine.AddNotifier(alert.NewPushoverNotifier(cfg.PushoverUser, cfg.PushoverToken))
	}
}

// startReadAPI starts the read-only HTTP surface when --listen is set. The
// inbound webhook is mounted only with --enable-webhook and always requires a
// token; --require-token forces a token even on loopback. Shared by the
// dashboard/serve backend and the daemon.
// drops, when non-nil, is exported on /metrics as mkt_quote_drops_total so a
// wedged TUI consumer is visible to monitoring instead of only to whoever is
// staring at the terminal.
func startReadAPI(cmd *cobra.Command, cache *market.Cache, engine *alert.Engine, opts backendOpts, drops func() uint64) (func(), error) {
	addr, _ := cmd.Flags().GetString("listen")
	if addr == "" {
		return func() {}, nil
	}
	token, _ := cmd.Flags().GetString("listen-token")
	if opts.requireToken && token == "" {
		return nil, fmt.Errorf("--require-token set but --listen-token is empty")
	}
	if opts.enableWebhook && token == "" {
		return nil, fmt.Errorf("--enable-webhook requires --listen-token: the inbound webhook injects alerts into the notifier fan-out and must never be unauthenticated, even on loopback")
	}
	if !opts.requireToken {
		if err := checkListenSafety(addr, token); err != nil {
			return nil, err
		}
	}
	srv := api.New(addr, cache, engine).WithToken(token).WithWebhook(opts.enableWebhook).WithDrops(drops)
	_ = srv.Start()
	fmt.Fprintf(os.Stderr, "api: listening on %s (webhook=%v)\n", addr, opts.enableWebhook)
	return func() { _ = srv.Shutdown(context.Background()) }, nil
}

// setupBackend wires providers, hub, alert engine, and portfolios and
// loads persisted history — everything except creating a tea.Program.
// The returned cleanup closes any recording sink; callers must defer it.
func setupBackend(opts backendOpts) (*backend, func(), error) {
	res, err := config.LoadWithResult()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	cfg := res.Config
	// A config that does not parse is not fatal: mkt starts on defaults so
	// the user still gets a dashboard, but it says so loudly and refuses
	// every write until the file is repaired, because writing now would
	// replace their real settings with the defaults we fell back to.
	if res.Degraded {
		reportDegradedConfig(res, opts.force)
	}

	// Apply theme from config before creating any TUI components.
	theme.Apply(cfg.Theme)

	// Build watchlist groups, preserving backward compat with the legacy
	// top-level `watchlist:` field.
	var groups []watchlistview.Group
	if len(cfg.Watchlists) > 0 {
		for _, w := range cfg.Watchlists {
			groups = append(groups, watchlistview.Group{Name: w.Name, Symbols: w.Symbols})
		}
	}
	if len(cfg.Watchlist) > 0 {
		legacy := watchlistview.Group{Name: "Default", Symbols: cfg.Watchlist}
		if len(groups) == 0 {
			groups = []watchlistview.Group{legacy}
		} else {
			groups = append([]watchlistview.Group{legacy}, groups...)
		}
	}
	if len(groups) == 0 {
		groups = []watchlistview.Group{{Name: "Default"}}
	}
	// Every symbol the data plane must price: the watchlist union plus every
	// portfolio holding and transaction. Holdings are not implicitly watched
	// (`mkt portfolio import` never touches the watchlist), so leaving them
	// out is how a position ends up unsubscribed and unpriced.
	symbols := subscribeSymbols(groups, cfg.Portfolios)

	cache := market.NewCache(cfg.SparklineLen)
	coinbaseProv := coinbase.New()
	yahooProv := yahoo.New(cfg.PollDuration())

	var coinbaseQP provider.QuoteProvider = coinbaseProv
	var yahooQP provider.QuoteProvider = yahooProv
	var closers []func()
	if recordPath := os.Getenv("MKT_RECORD"); recordPath != "" {
		// The sink opens with O_TRUNC, so preserve whatever is already there
		// before it is destroyed — a recording is exactly the kind of data a
		// relaunch must not silently throw away.
		backupPath, err := preserveRecording(recordPath)
		if err != nil {
			return nil, nil, fmt.Errorf("recording: %w", err)
		}
		if backupPath != "" {
			fmt.Fprintf(os.Stderr, "recording: %s already exists; previous capture kept at %s\n", recordPath, backupPath)
		}
		sink, err := recording.NewSink(recordPath)
		if err != nil {
			return nil, nil, fmt.Errorf("recording: %w", err)
		}
		closers = append(closers, func() { _ = sink.Close() })
		coinbaseQP = recording.New(coinbaseProv, sink)
		yahooQP = recording.New(yahooProv, sink)
	}

	hub := market.NewHub(cache, coinbaseQP, yahooQP)

	// Convert config portfolios. Materialize folds any optional
	// transactions on top of the snapshot holdings; with no transactions
	// the snapshot passes through unchanged.
	var portfolios []portfolio.Portfolio
	for _, cp := range cfg.Portfolios {
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
		portfolios = append(portfolios, portfolio.Portfolio{
			Name:         cp.Name,
			Holdings:     portfolio.Materialize(holdings, txs),
			Transactions: txs,
			TaxMethod:    portfolio.TaxMethod(cp.TaxMethod),
		})
	}

	// Broadcaster fans every data-plane message out to all attached
	// programs; the alert engine's notifier callback rides the same path.
	bc := broadcast.New()
	alertEngine := alert.NewEngine(5*time.Minute, func(a alert.TriggeredAlert) {
		bc.Send(tui.AlertTriggeredMsg{Alert: a})
	})

	// Load alert rules from config.
	alertEngine.SetRules(engineRules(cfg.Alerts))

	// Notifier registration (gated by the hardening toggles) — shared with
	// the daemon path so every headless/attended surface gates identically.
	registerNotifiers(alertEngine, cfg, opts)

	// Persisted alert history: load past triggers and register the
	// notifier so future ones are appended automatically.
	historyFile := alert.NewHistoryFile(filepath.Join(config.ConfigDir(), "alert-history.ndjson"), 500)
	pastTriggers, err := historyFile.LoadAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "alert history: %v\n", err)
	}
	alertEngine.AddNotifier(alert.NewHistoryNotifier(historyFile))
	alertEngine.SetPriceSource(cache)

	// Route history requests: fred first (its FRED: prefix is unique),
	// then Coinbase for crypto, then Yahoo for everything else.
	fredProv := fred.New()
	histProvider := market.NewMultiHistoryProvider(fredProv, coinbaseProv, yahooProv)

	// Portfolio equity history.
	equityFile := portfolio.NewEquityFile(filepath.Join(config.ConfigDir(), "equity-history.ndjson"), 1000)
	pastEquity, eqErr := equityFile.LoadByName()
	if eqErr != nil {
		fmt.Fprintf(os.Stderr, "equity history: %v\n", eqErr)
	}

	// Upcoming events for the macro tab: hardcoded econ schedule now,
	// per-ticker earnings merged in later by startDataPlane.
	events := calendar.EconomicEvents()
	calEvents := calendar.Upcoming(events, time.Now().UTC(), 30*24*time.Hour)

	b := &backend{
		cfg:          cfg,
		groups:       groups,
		symbols:      symbols,
		cache:        cache,
		hub:          hub,
		histProvider: histProvider,
		portfolios:   portfolios,
		alertEngine:  alertEngine,
		yahooProv:    yahooProv,
		coinbaseProv: coinbaseProv,
		bc:           bc,
		opts:         opts,
		equityFile:   equityFile,
		degraded:     res.Degraded,
		configErr:    res.Err,
		configPath:   res.Path,
		configLine:   res.Line,
		writable:     !res.Degraded || opts.force,
		baseEvents:   events,
		calEvents:    calEvents,
		pastTriggers: pastTriggers,
		pastEquity:   pastEquity,
	}
	cleanup := func() {
		// Notifier fan-out is asynchronous, so anything queued at exit is
		// lost unless it is drained first.
		if !alertEngine.Flush(alertFlushTimeout) {
			fmt.Fprintf(os.Stderr, "alerts: gave up waiting for queued notifications after %s\n", alertFlushTimeout)
		}
		for _, c := range closers {
			c()
		}
	}
	return b, cleanup, nil
}

// alertFlushTimeout bounds how long shutdown waits for queued notifications
// to be delivered. Long enough for an HTTP webhook round trip, short enough
// that quitting still feels instant when a destination is unreachable.
const alertFlushTimeout = 3 * time.Second

// engineRules converts the config representation of alert rules into the
// engine's. Shared by every surface that loads rules so a rule behaves the
// same under `mkt`, `mkt serve` and `mkt daemon`.
func engineRules(in []config.AlertRule) []alert.Rule {
	var rules []alert.Rule
	for _, r := range in {
		var subs []alert.SubCondition
		for _, s := range r.Conditions {
			subs = append(subs, alert.SubCondition{
				Type:   alert.Condition(s.Condition),
				Value:  s.Value,
				Period: s.Period,
			})
		}
		rules = append(rules, alert.Rule{
			Symbol:     r.Symbol,
			Condition:  alert.Condition(r.Condition),
			Value:      r.Value,
			Period:     r.Period,
			Enabled:    r.Enabled,
			Webhooks:   r.Webhooks,
			Conditions: subs,
			Match:      r.Match,
		})
	}
	return rules
}

// configRules is the inverse of engineRules: it converts the engine's rules
// back into the config representation so alerts created, toggled or deleted
// in the TUI can be persisted.
func configRules(in []alert.Rule) []config.AlertRule {
	var rules []config.AlertRule
	for _, r := range in {
		var subs []config.AlertSubCondition
		for _, s := range r.Conditions {
			subs = append(subs, config.AlertSubCondition{
				Condition: string(s.Type),
				Value:     s.Value,
				Period:    s.Period,
			})
		}
		rules = append(rules, config.AlertRule{
			Symbol:     r.Symbol,
			Condition:  string(r.Condition),
			Value:      r.Value,
			Period:     r.Period,
			Enabled:    r.Enabled,
			Webhooks:   r.Webhooks,
			Conditions: subs,
			Match:      r.Match,
		})
	}
	return rules
}

// reportDegradedConfig explains, on stderr before the TUI takes the screen,
// that the config file could not be read — what is running instead, what is
// missing, and what happens to writes.
func reportDegradedConfig(res *config.LoadResult, force bool) {
	where := res.Path
	if res.Line > 0 {
		where = fmt.Sprintf("%s line %d", res.Path, res.Line)
	}
	fmt.Fprintf(os.Stderr, "mkt: config at %s does not parse: %v\n", where, res.Err)
	fmt.Fprintf(os.Stderr, "  starting on built-in defaults — your watchlists, portfolios and alerts are NOT loaded\n")
	if force {
		fmt.Fprintf(os.Stderr, "  --force: writes are enabled and will replace the file with defaults (a timestamped backup is taken first)\n")
		return
	}
	fmt.Fprintf(os.Stderr, "  config writes are disabled until the file is fixed; run `mkt config validate`, or pass --force to overwrite it\n")
}

// buildApp constructs a fresh TUI model attached to the shared backend,
// seeded with the persisted alert / equity / calendar / notes state plus the
// data-plane state (degraded config, unroutable symbols, parent context).
// Safe to call once per SSH session — the model is per-session, the data
// plane behind it is shared.
func (b *backend) buildApp(ctx context.Context) *tui.App {
	app := tui.NewApp(b.groups, b.cache, b.histProvider, b.portfolios, b.alertEngine, b.yahooProv, b.coinbaseProv)
	if len(b.pastTriggers) > 0 {
		app.LoadPastAlerts(b.pastTriggers)
	}
	if len(b.pastEquity) > 0 {
		app.LoadEquityHistory(b.pastEquity)
	}
	app.LoadCalendarEvents(b.calEvents)
	if len(b.cfg.Notes) > 0 {
		app.LoadNotes(b.cfg.Notes)
	}
	b.applyWiring(ctx, app)
	return app
}

// startDataPlane launches the shared goroutines: provider status, the hub
// fan-out, the earnings fetch, and the macro / futures / DeFi / equity /
// news pollers. Every update is delivered via the broadcaster, so it
// reaches whatever programs are attached. Call exactly once.
func (b *backend) startDataPlane(ctx context.Context) {
	// Provider connection status → all sessions. Both providers report now:
	// Coinbase's WebSocket state and Yahoo's rolling health, so a total
	// Yahoo outage is visible instead of showing as prices that merely stop
	// changing.
	go pumpStatus(ctx, b.bc, "coinbase", b.coinbaseProv.StatusChan(), nil)
	go pumpStatus(ctx, b.bc, "yahoo", b.yahooProv.StatusChan(), b.yahooProv.LastError)

	// Per-ticker earnings merged into the econ calendar (fetched once,
	// concurrently, so startup isn't blocked on a flaky endpoint). Bounded
	// by ctx as well as its own timeout so it cannot outlive shutdown.
	go func() {
		earningsCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		earnings, err := (yahoo.EarningsAdapter{P: b.yahooProv}).Fetch(earningsCtx, stockTickers(b.symbols))
		if err != nil || len(earnings) == 0 {
			return
		}
		merged := append(b.baseEvents, earnings...)
		b.bc.Send(tui.CalendarUpdateMsg{Events: calendar.Upcoming(merged, time.Now().UTC(), 30*24*time.Hour)})
	}()

	// Alert evaluation rides the hub's observer path, which never drops:
	// on the dispatch path a transient spike could be discarded under TUI
	// back-pressure, which silently loses a level crossing and rewinds a
	// `match: sequence` rule's progress. The dispatch callback is left with
	// only the (best-effort by design) UI fan-out.
	b.hub.AddObserver(b.alertEngine.Check)

	// Live quote fan-out.
	b.unroutable = b.hub.Start(ctx, b.symbols, func(q provider.Quote) {
		b.bc.Send(tui.QuoteUpdateMsg{Quote: q})
	})
	if len(b.unroutable) > 0 {
		fmt.Fprintf(os.Stderr, "mkt: %d symbol(s) no provider can serve, they will never price: %s\n",
			len(b.unroutable), strings.Join(b.unroutable, ", "))
		fmt.Fprintf(os.Stderr, "  check for a typo (APPL vs AAPL), or use FRED:<series> for economic data\n")
	}

	// Backfill the ring buffers from history so sparklines and indicator
	// alerts are meaningful immediately. Runs in the background: first paint
	// must not wait on it, and any live tick that lands first wins.
	go b.seedCache(ctx)

	// Persist alerts created / toggled / deleted in the TUI.
	b.startAlertPersistence(ctx)

	// Optional feeds are each gated by config.Providers so a locked-down or
	// geo-restricted deployment can cut the egress it doesn't want.
	prov := b.cfg.Providers

	// Macro dashboard polling (Yahoo macro indices).
	if prov.MacroOn() {
		go poll(ctx, b.cfg.PollDuration(), func() {
			quotes := b.yahooProv.FetchMacroQuotes(ctx)
			if len(quotes) > 0 {
				b.bc.Send(tui.MacroUpdateMsg{Quotes: quotes})
			}
		})
	}

	// Crypto futures — Binance funding + OI for major perps.
	if prov.BinanceOn() {
		go poll(ctx, 2*time.Minute, func() {
			snaps := binance.FetchFuturesSnapshot(ctx, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
			if len(snaps) > 0 {
				b.bc.Send(tui.FuturesUpdateMsg{Snapshots: snaps})
			}
		})
	}

	// DeFi TVL — DeFiLlama public API.
	if prov.DeFiLlamaOn() {
		go poll(ctx, 5*time.Minute, func() {
			chains, err := defillama.FetchChains(ctx)
			if err != nil || len(chains) == 0 {
				return
			}
			b.bc.Send(tui.DeFiUpdateMsg{Chains: chains})
		})
	}

	// Portfolio equity-curve marking every 5 minutes.
	go poll(ctx, 5*time.Minute, func() {
		now := time.Now().UTC()
		quoteSnap := make(map[string]provider.Quote)
		for _, sym := range b.symbols {
			if pq, ok := b.cache.Latest(sym); ok {
				quoteSnap[sym] = provider.Quote{Symbol: sym, Price: pq, Timestamp: now}
			}
		}
		for _, pf := range b.portfolios {
			sum := portfolio.Evaluate(pf.Holdings, quoteSnap)
			if sum.TotalValue == 0 {
				continue
			}
			m := portfolio.EquityMark{Time: now, PortfolioName: pf.Name, Value: sum.TotalValue}
			if err := b.equityFile.Append(m); err != nil {
				fmt.Fprintf(os.Stderr, "equity append: %v\n", err)
				continue
			}
			b.bc.Send(tui.EquityMarkMsg{Mark: m})
		}
	})

	// News feed — RSS + per-ticker SEC EDGAR filings merged. Feeds default
	// to the built-in set but can be overridden (or trimmed) via news_feeds.
	if prov.NewsOn() {
		feeds := news.DefaultFeeds()
		if len(b.cfg.NewsFeeds) > 0 {
			feeds = feeds[:0]
			for _, f := range b.cfg.NewsFeeds {
				feeds = append(feeds, news.Feed{Name: f.Name, URL: f.URL})
			}
		}
		go poll(ctx, 3*time.Minute, func() {
			headlines := news.FetchAll(ctx, feeds)
			if len(b.cfg.EDGARTickers) > 0 {
				headlines = append(headlines, news.FetchEDGAR(ctx, b.cfg.EDGARTickers, 50)...)
			}
			if len(headlines) > 0 {
				b.bc.Send(tui.NewsUpdateMsg{Headlines: headlines})
			}
		})
	}
}

// startAPIIfRequested starts the read-only HTTP surface when --listen is
// set, returning a shutdown function (a no-op when the flag is empty).
// Shared by `mkt` and `mkt serve`. The inbound TradingView webhook is
// mounted only with --enable-webhook and always requires a token; a
// --require-token opt-in forces a token even on loopback for the read
// routes (loopback is not a trust boundary on multi-user hosts).
func (b *backend) startAPIIfRequested(cmd *cobra.Command) (func(), error) {
	return startReadAPI(cmd, b.cache, b.alertEngine, b.opts, b.hub.Drops)
}

// pumpStatus forwards one provider's health transitions to every attached
// session until ctx is cancelled. lastErr supplies the reason for an
// unhealthy transition when the provider tracks one.
//
// The status channels are buffered(1) and lossy by design, so this only ever
// forwards the latest state — which is all a status bar needs.
func pumpStatus(ctx context.Context, bc *broadcast.Broadcaster, name string, ch <-chan bool, lastErr func() error) {
	for {
		select {
		case <-ctx.Done():
			return
		case connected, ok := <-ch:
			if !ok {
				return
			}
			msg := tui.ConnectionStatusMsg{Provider: name, Connected: connected}
			if !connected && lastErr != nil {
				msg.Error = lastErr()
			}
			bc.Send(msg)
		}
	}
}

// alertPersistDebounce coalesces a burst of rule edits into one write. The
// TUI can produce several changes in a second (toggling down a list), and
// each write takes a backup — debouncing keeps the backup directory useful.
const alertPersistDebounce = 2 * time.Second

// startAlertPersistence writes rules back to config whenever the TUI adds,
// removes or toggles one, so alerts created in the dashboard survive a
// restart.
//
// Persistence is single-writer by construction: the callback lives on the
// backend, not on a session, so under `mkt serve` N attached SSH sessions
// share one writer rather than racing to rewrite the file. It is disabled
// entirely when the surface should not own the host's config (serve mode
// without --persist-alerts) or when the config on disk is degraded, since
// writing then would replace the user's real settings with the defaults we
// fell back to.
func (b *backend) startAlertPersistence(ctx context.Context) {
	if !b.opts.persistAlerts {
		return
	}
	if !b.writable {
		fmt.Fprintf(os.Stderr, "alerts: changes made in the TUI will not be saved while %s does not parse\n", b.configPath)
		return
	}

	// Capacity 1 plus replace-on-full: only the newest snapshot matters, and
	// the engine's callback must never block on the writer.
	pending := make(chan []alert.Rule, 1)
	b.alertEngine.SetOnRulesChanged(func(rules []alert.Rule) {
		for {
			select {
			case pending <- rules:
				return
			default:
			}
			select {
			case <-pending:
			default:
			}
		}
	})

	go func() {
		timer := time.NewTimer(time.Hour)
		if !timer.Stop() {
			<-timer.C
		}
		defer timer.Stop()

		var latest []alert.Rule
		armed := false
		for {
			select {
			case <-ctx.Done():
				// Take whatever the last edit left behind rather than
				// writing a snapshot the debounce had already superseded.
				select {
				case rules := <-pending:
					latest, armed = rules, true
				default:
				}
				if armed {
					b.persistRules(latest)
				}
				return
			case rules := <-pending:
				latest = rules
				if armed && !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(alertPersistDebounce)
				armed = true
			case <-timer.C:
				armed = false
				b.persistRules(latest)
			}
		}
	}()
}

// persistRules writes one rule snapshot back to config.yaml. A refusal is
// reported once and turns persistence off for the rest of the run rather
// than repeating the same complaint on every edit.
func (b *backend) persistRules(rules []alert.Rule) {
	if !b.writable {
		return
	}
	b.cfg.Alerts = configRules(rules)
	rep, err := config.SaveSafely(b.cfg, config.SaveOptions{
		// A TUI has no stdin to prompt on, and the removals here are exactly
		// the deletions the user just performed. SaveSafely still takes a
		// timestamped backup before every write, so the previous rule set is
		// recoverable from ~/.config/mkt.
		AssumeYes: true,
		Force:     b.opts.force,
	})
	if err != nil {
		b.writable = false
		fmt.Fprintf(os.Stderr, "alerts: not saved: %v\n", err)
		if rep != nil {
			for _, r := range rep.Removed {
				fmt.Fprintf(os.Stderr, "  - %s\n", r)
			}
		}
	}
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// state loaded once at setup and seeded into every session's model.
	baseEvents   []calendar.Event
	calEvents    []calendar.Event
	pastTriggers []alert.TriggeredAlert
	pastEquity   map[string][]portfolio.EquityMark
}

// setupBackend wires providers, hub, alert engine, and portfolios and
// loads persisted history — everything except creating a tea.Program.
// The returned cleanup closes any recording sink; callers must defer it.
func setupBackend() (*backend, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
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
	symbols := dedupeUnion(groups)

	cache := market.NewCache(cfg.SparklineLen)
	coinbaseProv := coinbase.New()
	yahooProv := yahoo.New(cfg.PollDuration())

	var coinbaseQP provider.QuoteProvider = coinbaseProv
	var yahooQP provider.QuoteProvider = yahooProv
	var closers []func()
	if recordPath := os.Getenv("MKT_RECORD"); recordPath != "" {
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
	alertEngine.AddNotifier(alert.NewDesktopNotifier())

	// Load alert rules from config.
	var rules []alert.Rule
	anyWebhook := cfg.WebhookURL != ""
	for _, r := range cfg.Alerts {
		if len(r.Webhooks) > 0 {
			anyWebhook = true
		}
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
	alertEngine.SetRules(rules)
	if anyWebhook {
		alertEngine.AddNotifier(alert.NewWebhookNotifier(cfg.WebhookURL))
	}
	if cfg.NtfyTopic != "" {
		alertEngine.AddNotifier(alert.NewNtfyNotifier(cfg.NtfyServer, cfg.NtfyTopic))
	}
	if cfg.PushoverUser != "" && cfg.PushoverToken != "" {
		alertEngine.AddNotifier(alert.NewPushoverNotifier(cfg.PushoverUser, cfg.PushoverToken))
	}

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
		equityFile:   equityFile,
		baseEvents:   events,
		calEvents:    calEvents,
		pastTriggers: pastTriggers,
		pastEquity:   pastEquity,
	}
	cleanup := func() {
		for _, c := range closers {
			c()
		}
	}
	return b, cleanup, nil
}

// buildApp constructs a fresh TUI model attached to the shared backend,
// seeded with the persisted alert / equity / calendar / notes state. Safe
// to call once per SSH session — the model is per-session, the data plane
// behind it is shared.
func (b *backend) buildApp() *tui.App {
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
	return app
}

// startDataPlane launches the shared goroutines: provider status, the hub
// fan-out, the earnings fetch, and the macro / futures / DeFi / equity /
// news pollers. Every update is delivered via the broadcaster, so it
// reaches whatever programs are attached. Call exactly once.
func (b *backend) startDataPlane(ctx context.Context) {
	// Coinbase connection status → all sessions.
	go func() {
		for connected := range b.coinbaseProv.StatusChan() {
			b.bc.Send(tui.ConnectionStatusMsg{Provider: "coinbase", Connected: connected})
		}
	}()

	// Per-ticker earnings merged into the econ calendar (fetched once,
	// concurrently, so startup isn't blocked on a flaky endpoint).
	go func() {
		earningsCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		earnings, err := (yahoo.EarningsAdapter{P: b.yahooProv}).Fetch(earningsCtx, stockTickers(b.symbols))
		if err != nil || len(earnings) == 0 {
			return
		}
		merged := append(b.baseEvents, earnings...)
		b.bc.Send(tui.CalendarUpdateMsg{Events: calendar.Upcoming(merged, time.Now().UTC(), 30*24*time.Hour)})
	}()

	// Live quote fan-out + inline alert evaluation.
	b.hub.Start(ctx, b.symbols, func(q provider.Quote) {
		b.bc.Send(tui.QuoteUpdateMsg{Quote: q})
		b.alertEngine.Check(q)
	})

	// Macro dashboard polling.
	go poll(ctx, b.cfg.PollDuration(), func() {
		quotes := b.yahooProv.FetchMacroQuotes(ctx)
		if len(quotes) > 0 {
			b.bc.Send(tui.MacroUpdateMsg{Quotes: quotes})
		}
	})

	// Crypto futures — Binance funding + OI for major perps.
	go poll(ctx, 2*time.Minute, func() {
		snaps := binance.FetchFuturesSnapshot(ctx, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
		if len(snaps) > 0 {
			b.bc.Send(tui.FuturesUpdateMsg{Snapshots: snaps})
		}
	})

	// DeFi TVL — DeFiLlama public API.
	go poll(ctx, 5*time.Minute, func() {
		chains, err := defillama.FetchChains(ctx)
		if err != nil || len(chains) == 0 {
			return
		}
		b.bc.Send(tui.DeFiUpdateMsg{Chains: chains})
	})

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

	// News feed — RSS + per-ticker SEC EDGAR filings merged.
	feeds := news.DefaultFeeds()
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

// startAPIIfRequested starts the read-only HTTP surface when --listen is
// set, returning a shutdown function (a no-op when the flag is empty).
// Shared by `mkt` and `mkt serve`.
func (b *backend) startAPIIfRequested(cmd *cobra.Command) (func(), error) {
	addr, _ := cmd.Flags().GetString("listen")
	if addr == "" {
		return func() {}, nil
	}
	token, _ := cmd.Flags().GetString("listen-token")
	if err := checkListenSafety(addr, token); err != nil {
		return nil, err
	}
	srv := api.New(addr, b.cache, b.alertEngine).WithToken(token)
	_ = srv.Start()
	fmt.Fprintf(os.Stderr, "api: listening on %s\n", addr)
	return func() { _ = srv.Shutdown(context.Background()) }, nil
}

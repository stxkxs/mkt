package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/api"
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
	"github.com/stxkxs/mkt/internal/tui/detail"
	"github.com/stxkxs/mkt/internal/tui/theme"
	watchlistview "github.com/stxkxs/mkt/internal/tui/watchlist"
)

// stockTickers returns the subset of symbols that look like stocks
// (no -USD/-USDT suffix; not a known bare-crypto symbol). Used to feed
// Yahoo's earnings endpoint, which doesn't have entries for crypto.
func stockTickers(symbols []string) []string {
	var out []string
	for _, s := range symbols {
		us := strings.ToUpper(s)
		if strings.HasSuffix(us, "-USD") || strings.HasSuffix(us, "USDT") || strings.HasPrefix(us, "FRED:") {
			continue
		}
		out = append(out, us)
	}
	return out
}

// dedupeUnion flattens every group's symbols into a deduplicated slice.
func dedupeUnion(groups []watchlistview.Group) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, g := range groups {
		for _, s := range g.Symbols {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply theme from config before creating any TUI components
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
	if recordPath := os.Getenv("MKT_RECORD"); recordPath != "" {
		sink, err := recording.NewSink(recordPath)
		if err != nil {
			return fmt.Errorf("recording: %w", err)
		}
		defer sink.Close()
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

	// Create alert engine
	var p *tea.Program
	alertEngine := alert.NewEngine(5*time.Minute, func(a alert.TriggeredAlert) {
		if p != nil {
			p.Send(tui.AlertTriggeredMsg{Alert: a})
		}
	})
	alertEngine.AddNotifier(alert.NewDesktopNotifier())

	// Load alert rules from config
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

	// Set price source for indicator-based alerts
	alertEngine.SetPriceSource(cache)

	// Route history requests: fred first (its FRED: prefix is unique), then
	// Coinbase for crypto, then Yahoo for everything else.
	fredProv := fred.New()
	histProvider := market.NewMultiHistoryProvider(fredProv, coinbaseProv, yahooProv)
	app := tui.NewApp(groups, cache, histProvider, portfolios, alertEngine, yahooProv, coinbaseProv)
	if len(pastTriggers) > 0 {
		app.LoadPastAlerts(pastTriggers)
	}

	if addr, _ := cmd.Flags().GetString("listen"); addr != "" {
		token, _ := cmd.Flags().GetString("listen-token")
		warnIfUnsafeListen(addr, token)
		srv := api.New(addr, cache, alertEngine).WithToken(token)
		_ = srv.Start()
		defer func() { _ = srv.Shutdown(context.Background()) }()
		fmt.Fprintf(os.Stderr, "api: listening on %s\n", addr)
	}

	// Portfolio equity history: load past marks and seed the model.
	equityFile := portfolio.NewEquityFile(filepath.Join(config.ConfigDir(), "equity-history.ndjson"), 1000)
	pastEquity, eqErr := equityFile.LoadByName()
	if eqErr != nil {
		fmt.Fprintf(os.Stderr, "equity history: %v\n", eqErr)
	}
	if len(pastEquity) > 0 {
		app.LoadEquityHistory(pastEquity)
	}

	// Upcoming events for the macro tab: hardcoded econ schedule plus
	// per-watchlist-ticker earnings from Yahoo. Earnings fetched
	// concurrently in the background so startup isn't blocked on a
	// flaky third-party endpoint.
	events := calendar.EconomicEvents()
	app.LoadCalendarEvents(calendar.Upcoming(events, time.Now().UTC(), 30*24*time.Hour))
	go func() {
		earningsCtx, earningsCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer earningsCancel()
		earnings, err := (yahoo.EarningsAdapter{P: yahooProv}).Fetch(earningsCtx, stockTickers(symbols))
		if err != nil || len(earnings) == 0 {
			return
		}
		merged := append(events, earnings...)
		p.Send(tui.CalendarUpdateMsg{Events: calendar.Upcoming(merged, time.Now().UTC(), 30*24*time.Hour)})
	}()

	if len(cfg.Notes) > 0 {
		app.LoadNotes(cfg.Notes)
	}

	p = tea.NewProgram(app)
	detail.SetLiveProgram(p)
	defer detail.SetLiveProgram(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for connected := range coinbaseProv.StatusChan() {
			p.Send(tui.ConnectionStatusMsg{
				Provider:  "coinbase",
				Connected: connected,
			})
		}
	}()

	hub.Start(ctx, symbols, func(q provider.Quote) {
		p.Send(tui.QuoteUpdateMsg{Quote: q})
		alertEngine.Check(q)
	})

	// Macro dashboard polling
	go poll(ctx, cfg.PollDuration(), func() {
		quotes := yahooProv.FetchMacroQuotes(ctx)
		if len(quotes) > 0 {
			p.Send(tui.MacroUpdateMsg{Quotes: quotes})
		}
	})

	// Crypto futures — Binance funding + OI for major perps.
	go poll(ctx, 2*time.Minute, func() {
		snaps := binance.FetchFuturesSnapshot(ctx, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
		if len(snaps) > 0 {
			p.Send(tui.FuturesUpdateMsg{Snapshots: snaps})
		}
	})

	// DeFi TVL — DeFiLlama public API.
	go poll(ctx, 5*time.Minute, func() {
		chains, err := defillama.FetchChains(ctx)
		if err != nil || len(chains) == 0 {
			return
		}
		p.Send(tui.DeFiUpdateMsg{Chains: chains})
	})

	// Portfolio equity-curve marking — append current portfolio values
	// to the persisted history every 5 minutes and broadcast the new
	// mark to the TUI.
	go poll(ctx, 5*time.Minute, func() {
		now := time.Now().UTC()
		quoteSnap := make(map[string]provider.Quote)
		for _, sym := range symbols {
			if pq, ok := cache.Latest(sym); ok {
				quoteSnap[sym] = provider.Quote{Symbol: sym, Price: pq, Timestamp: now}
			}
		}
		for _, pf := range portfolios {
			sum := portfolio.Evaluate(pf.Holdings, quoteSnap)
			if sum.TotalValue == 0 {
				continue
			}
			m := portfolio.EquityMark{Time: now, PortfolioName: pf.Name, Value: sum.TotalValue}
			if err := equityFile.Append(m); err != nil {
				fmt.Fprintf(os.Stderr, "equity append: %v\n", err)
				continue
			}
			p.Send(tui.EquityMarkMsg{Mark: m})
		}
	})

	// News feed — RSS + per-ticker SEC EDGAR filings merged.
	feeds := news.DefaultFeeds()
	go poll(ctx, 3*time.Minute, func() {
		headlines := news.FetchAll(ctx, feeds)
		if len(cfg.EDGARTickers) > 0 {
			headlines = append(headlines, news.FetchEDGAR(ctx, cfg.EDGARTickers, 50)...)
		}
		if len(headlines) > 0 {
			p.Send(tui.NewsUpdateMsg{Headlines: headlines})
		}
	})

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}
	return nil
}

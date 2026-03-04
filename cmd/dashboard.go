package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui"
)

func runDashboard(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	symbols := cfg.Watchlist
	cache := market.NewCache(cfg.SparklineLen)
	coinbaseProv := coinbase.New()
	yahooProv := yahoo.New(cfg.PollDuration())
	hub := market.NewHub(cache, coinbaseProv, yahooProv)

	// Convert config holdings to portfolio holdings
	var holdings []portfolio.Holding
	for _, h := range cfg.Holdings {
		holdings = append(holdings, portfolio.Holding{
			Symbol:    h.Symbol,
			Quantity:  h.Quantity,
			CostBasis: h.CostBasis,
		})
	}

	// Create alert engine
	var p *tea.Program
	alertEngine := alert.NewEngine(5*time.Minute, func(a alert.TriggeredAlert) {
		alert.Notify(a)
		if p != nil {
			p.Send(tui.AlertTriggeredMsg{Alert: a})
		}
	})

	// Load alert rules from config
	var rules []alert.Rule
	for _, r := range cfg.Alerts {
		rules = append(rules, alert.Rule{
			Symbol:    r.Symbol,
			Condition: alert.Condition(r.Condition),
			Value:     r.Value,
			Enabled:   r.Enabled,
		})
	}
	alertEngine.SetRules(rules)

	// Route history requests: Coinbase for crypto, Yahoo for stocks
	histProvider := market.NewMultiHistoryProvider(coinbaseProv, yahooProv)
	app := tui.NewApp(symbols, cache, histProvider, holdings, alertEngine)
	p = tea.NewProgram(app)

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

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}
	return nil
}

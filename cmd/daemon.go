package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/api"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

func init() {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run alerts headless (no TUI)",
		Long: `Subscribes to the configured providers, evaluates alerts, and fires
all configured notifiers (desktop, webhook, ntfy, Pushover, history)
without showing a TUI. Useful on a VPS / always-on machine. Stops on
SIGTERM or SIGINT.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Pick the union of every configured watchlist group
			symbols := append([]string(nil), cfg.Watchlist...)
			for _, w := range cfg.Watchlists {
				symbols = append(symbols, w.Symbols...)
			}
			symbols = dedupeStrings(symbols)
			if len(symbols) == 0 {
				return fmt.Errorf("no symbols configured")
			}

			cache := market.NewCache(cfg.SparklineLen)
			coinbaseProv := coinbase.New()
			yahooProv := yahoo.New(cfg.PollDuration())
			hub := market.NewHub(cache, coinbaseProv, yahooProv)

			// Alert engine + notifiers (mirror dashboard wiring).
			engine := alert.NewEngine(5*time.Minute, nil)
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
			engine.SetRules(rules)
			engine.SetPriceSource(cache)
			engine.AddNotifier(alert.NewDesktopNotifier())
			if anyWebhook {
				engine.AddNotifier(alert.NewWebhookNotifier(cfg.WebhookURL))
			}
			if cfg.NtfyTopic != "" {
				engine.AddNotifier(alert.NewNtfyNotifier(cfg.NtfyServer, cfg.NtfyTopic))
			}
			if cfg.PushoverUser != "" && cfg.PushoverToken != "" {
				engine.AddNotifier(alert.NewPushoverNotifier(cfg.PushoverUser, cfg.PushoverToken))
			}
			historyFile := alert.NewHistoryFile(filepath.Join(config.ConfigDir(), "alert-history.ndjson"), 500)
			engine.AddNotifier(alert.NewHistoryNotifier(historyFile))

			// Lifecycle: cancel on SIGTERM / SIGINT.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				sig := <-sigs
				log.Printf("daemon: caught %v, shutting down", sig)
				cancel()
			}()

			if addr, _ := cmd.Flags().GetString("listen"); addr != "" {
				token, _ := cmd.Flags().GetString("listen-token")
				warnIfUnsafeListen(addr, token)
				srv := api.New(addr, cache, engine).WithToken(token)
				_ = srv.Start()
				defer func() { _ = srv.Shutdown(context.Background()) }()
				log.Printf("daemon: api listening on %s", addr)
			}

			log.Printf("daemon: watching %d symbols, %d alert rules", len(symbols), len(rules))
			hub.Start(ctx, symbols, func(q provider.Quote) {
				engine.Check(q)
			})

			<-ctx.Done()
			return nil
		},
	}
	rootCmd.AddCommand(daemonCmd)
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

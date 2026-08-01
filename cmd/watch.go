package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/format"
)

func init() {
	watchCmd := &cobra.Command{
		Use:   "watch [symbols...]",
		Short: "Quick non-TUI price check",
		Long: `Stream live prices to stdout. Useful for scripting or quick checks.

Symbols are normalized the same way the dashboard normalizes them, so
"btc", "BTCUSDT" and "btc-usd" all stream BTC-USD. A symbol that routes
to no provider is an error, not a warning — there would be nothing left
to wait for.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runWatch,
	}
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	quoteCh := make(chan provider.Quote, 64)

	coinbaseProv := coinbase.New()
	yahooProv := yahoo.New(5 * time.Second)

	// Route symbols to providers. Canonicalize first: the providers accept
	// only the spelling they emit, so a bare "btc" used to be routed by
	// Supports and then subscribed as "BTC" — a product that does not exist.
	var cryptoSyms, stockSyms, unknown []string
	for _, s := range args {
		canon := symbol.Canonical(s)
		switch {
		case coinbaseProv.Supports(canon):
			cryptoSyms = append(cryptoSyms, canon)
		case yahooProv.Supports(canon):
			stockSyms = append(stockSyms, canon)
		default:
			unknown = append(unknown, s)
		}
	}

	// With no producer attached there is nothing that can ever arrive on
	// quoteCh, so the read loop below would print a header and then block
	// until the user gave up and hit ^C. Refuse instead.
	if len(cryptoSyms) == 0 && len(stockSyms) == 0 {
		return fmt.Errorf("no symbol routes to a provider: %s\n"+
			"  crypto looks like BTC-USD / ETH-USDT / btc; stocks like AAPL, ^GSPC, GC=F, EURUSD=X",
			strings.Join(unknown, ", "))
	}
	for _, s := range unknown {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping %q — it routes to no provider\n", s)
	}

	if len(cryptoSyms) > 0 {
		go func() {
			if err := coinbaseProv.Subscribe(ctx, cryptoSyms, quoteCh); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "coinbase subscribe: %v\n", err)
			}
		}()
	}
	if len(stockSyms) > 0 {
		go func() {
			if err := yahooProv.Subscribe(ctx, stockSyms, quoteCh); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "yahoo subscribe: %v\n", err)
			}
		}()
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-12s %12s %10s %8s  %s\n", "SYMBOL", "PRICE", "CHANGE", "VOL", "TIME")
	fmt.Fprintln(out, strings.Repeat("─", 60))

	for {
		select {
		case <-ctx.Done():
			return nil
		case q := <-quoteCh:
			sign := "+"
			if q.ChangePct < 0 {
				sign = ""
			}
			fmt.Fprintf(out, "%-12s %12s %s%6.2f%% %8s  %s\n",
				q.Symbol,
				format.FormatPrice(q.Price),
				sign, q.ChangePct,
				format.FormatVolume(q.Volume),
				q.Timestamp.Format("15:04:05"),
			)
		}
	}
}

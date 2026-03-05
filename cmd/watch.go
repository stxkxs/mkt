package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/format"
)

func init() {
	watchCmd := &cobra.Command{
		Use:   "watch [symbols...]",
		Short: "Quick non-TUI price check",
		Long:  "Stream live prices to stdout. Useful for scripting or quick checks.",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runWatch,
	}
	rootCmd.AddCommand(watchCmd)
}

func runWatch(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	symbols := args
	quoteCh := make(chan provider.Quote, 64)

	coinbaseProv := coinbase.New()
	yahooProv := yahoo.New(5 * time.Second)

	// Route symbols to providers
	var cryptoSyms, stockSyms []string
	for _, s := range symbols {
		if coinbaseProv.Supports(s) {
			cryptoSyms = append(cryptoSyms, strings.ToUpper(s))
		} else if yahooProv.Supports(s) {
			stockSyms = append(stockSyms, strings.ToUpper(s))
		} else {
			fmt.Fprintf(os.Stderr, "warning: unknown symbol %s\n", s)
		}
	}

	if len(cryptoSyms) > 0 {
		go coinbaseProv.Subscribe(ctx, cryptoSyms, quoteCh)
	}
	if len(stockSyms) > 0 {
		go yahooProv.Subscribe(ctx, stockSyms, quoteCh)
	}

	// Print header
	fmt.Printf("%-12s %12s %10s %8s  %s\n", "SYMBOL", "PRICE", "CHANGE", "VOL", "TIME")
	fmt.Println(strings.Repeat("─", 60))

	for {
		select {
		case <-ctx.Done():
			return nil
		case q := <-quoteCh:
			sign := "+"
			if q.ChangePct < 0 {
				sign = ""
			}
			fmt.Printf("%-12s %12s %s%6.2f%% %8s  %s\n",
				q.Symbol,
				format.FormatPrice(q.Price),
				sign, q.ChangePct,
				format.FormatVolume(q.Volume),
				q.Timestamp.Format("15:04:05"),
			)
		}
	}
}


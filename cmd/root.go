package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// warnIfUnsafeListen prints a stderr warning when the user has bound
// the HTTP server to a non-loopback address without setting a token —
// /webhook/tradingview can inject alerts that fan out to desktop /
// push / webhook destinations, and /alerts leaks configured rule
// destinations, so a bare public bind is almost never what they meant.
func warnIfUnsafeListen(addr, token string) {
	if token != "" {
		return
	}
	host, _, _ := strings.Cut(addr, ":")
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return
	}
	fmt.Fprintf(os.Stderr,
		"api: WARNING: --listen %s has no --listen-token; /quotes /alerts /metrics and the TradingView webhook are world-reachable. Bind to 127.0.0.1 or set --listen-token to silence this.\n",
		addr)
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "mkt",
	Short: "Real-time stock & crypto market dashboard",
	Long:  "A terminal dashboard for tracking crypto and stock prices in real-time.",
	RunE:  runDashboard,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.PersistentFlags().String("listen", "", "if set (e.g. :9999 or 127.0.0.1:9999), start a read-only HTTP server with /quotes, /alerts, /metrics, /webhook/tradingview")
	rootCmd.PersistentFlags().String("listen-token", "", "optional bearer token required on every HTTP request when --listen is set; omit only when binding to loopback")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("mkt %s (commit: %s, built: %s)\n", version, commit, date)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

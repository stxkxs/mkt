package cmd

import (
	"fmt"
	"net"
	"os"

	"github.com/spf13/cobra"
)

// checkListenSafety fails closed on an unsafe HTTP bind. A non-loopback
// address (which includes the all-interfaces form ":9999" / 0.0.0.0) with
// no token would expose /webhook/tradingview — which injects alerts that
// fan out to desktop / push / webhook destinations — and /alerts, which
// leaks configured destinations, to anyone reachable. Only an explicit
// loopback host is allowed without a token; everything else must set
// --listen-token. Returns an error the caller surfaces to refuse startup
// (previously this only printed a warning and served anyway).
func checkListenSafety(addr, token string) error {
	if token != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = "" // unparseable → treat as non-loopback and require a token
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	return fmt.Errorf("refusing to bind --listen %s without --listen-token: "+
		"the TradingView webhook can inject alerts and /alerts leaks configured "+
		"destinations to anyone reachable. Bind to 127.0.0.1:<port> for local use, "+
		"or set --listen-token to serve on %s", addr, addr)
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
	rootCmd.PersistentFlags().String("listen", "", "if set (e.g. 127.0.0.1:9999), start a read-only HTTP server with /quotes, /alerts, /metrics; any non-loopback bind requires --listen-token")
	rootCmd.PersistentFlags().String("listen-token", "", "bearer token required in the Authorization header on every HTTP request; mandatory for any non-loopback (e.g. :9999 / 0.0.0.0) bind")
	rootCmd.PersistentFlags().Bool("enable-webhook", false, "mount the inbound /webhook/tradingview alert-injection route (requires --listen-token even on loopback)")
	rootCmd.PersistentFlags().Bool("require-token", false, "require --listen-token even on a loopback bind (loopback is not a trust boundary on multi-user hosts)")
	rootCmd.PersistentFlags().Bool("no-notify", false, "disable all desktop + third-party notifiers (desktop, webhook, ntfy, Pushover); alert rules and history still run")
	rootCmd.PersistentFlags().Bool("no-desktop-notify", false, "disable the desktop notification + terminal bell (already off by default under `mkt serve`)")
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

package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

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
	return fmt.Errorf("refusing to bind --listen %s without a listen token: "+
		"the TradingView webhook can inject alerts and /alerts leaks configured "+
		"destinations to anyone reachable. Bind to 127.0.0.1:<port> for local use, "+
		"or set MKT_LISTEN_TOKEN / --listen-token-file to serve on %s", addr, addr)
}

// Environment variables that supply the listen bearer token out of band.
// A token on argv is readable by every other user on the host (ps, /proc),
// so these — and --listen-token-file — are the recommended way to pass one.
const (
	// EnvListenToken carries the token itself.
	EnvListenToken = "MKT_LISTEN_TOKEN"
	// EnvListenTokenFile names a file whose (trimmed) contents are the token.
	EnvListenTokenFile = "MKT_LISTEN_TOKEN_FILE"
	// EnvListen supplies the read-API address when --listen is not given.
	EnvListen = "MKT_LISTEN"
)

// errSilent marks a failure whose explanation the command has already
// printed in full. Execute exits non-zero without adding a second, terser
// copy of the same message — the write-safety refusals in particular are
// multi-line and self-contained.
var errSilent = errors.New("command failed")

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

	// A runtime failure is not a usage mistake: printing the full command
	// tree after "config validate" reports a bad indent buries the one line
	// the user needs. Flag-parse errors still get usage attached, via
	// SetFlagErrorFunc below.
	SilenceUsage: true,
	// Errors are rendered by Execute so a command that has already printed
	// its own explanation can opt out with errSilent.
	SilenceErrors:     true,
	PersistentPreRunE: resolveListenToken,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.PersistentFlags().String("listen", "", "if set (e.g. 127.0.0.1:9999), start a read-only HTTP server with /quotes, /alerts, /metrics; any non-loopback bind requires a listen token (env "+EnvListen+")")
	rootCmd.PersistentFlags().String("listen-token", "", "bearer token required in the Authorization header on every HTTP request; prefer --listen-token-file or "+EnvListenTokenFile+"/"+EnvListenToken+", since argv is world-readable via ps")
	rootCmd.PersistentFlags().String("listen-token-file", "", "read the listen bearer token from this file (first line, trimmed); keeps the token off argv (env "+EnvListenTokenFile+")")
	rootCmd.PersistentFlags().Bool("enable-webhook", false, "mount the inbound /webhook/tradingview alert-injection route (requires a listen token even on loopback)")
	rootCmd.PersistentFlags().Bool("require-token", false, "require a listen token even on a loopback bind (loopback is not a trust boundary on multi-user hosts)")
	rootCmd.PersistentFlags().Bool("no-notify", false, "disable all desktop + third-party notifiers (desktop, webhook, ntfy, Pushover); alert rules and history still run")
	rootCmd.PersistentFlags().Bool("no-desktop-notify", false, "disable the desktop notification + terminal bell (already off by default under `mkt serve`)")

	// Usage is silenced for runtime errors but is exactly what a mistyped
	// flag calls for, so re-attach it on that path only.
	rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return fmt.Errorf("%w\n\n%s", err, c.UsageString())
	})
}

// resolveListenToken folds the out-of-band token sources into the
// --listen-token flag before any command runs, so every consumer keeps
// reading one flag. A token passed on argv is visible to every local user
// through ps and /proc/<pid>/cmdline, so the file and environment forms
// exist to keep it off the command line; an explicit --listen-token still
// wins, but earns a warning.
//
// Precedence: --listen-token > --listen-token-file > MKT_LISTEN_TOKEN_FILE
// > MKT_LISTEN_TOKEN. Likewise --listen falls back to MKT_LISTEN.
//
// This is rootCmd.PersistentPreRunE. Cobra runs only the nearest such hook
// in the command chain, so any subcommand that adds its own must call this
// one explicitly.
func resolveListenToken(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()

	if addr, _ := flags.GetString("listen"); addr == "" {
		if env := strings.TrimSpace(os.Getenv(EnvListen)); env != "" {
			if err := flags.Set("listen", env); err != nil {
				return fmt.Errorf("resolve %s: %w", EnvListen, err)
			}
		}
	}

	if f := flags.Lookup("listen-token"); f != nil && f.Changed && f.Value.String() != "" {
		fmt.Fprintf(os.Stderr, "mkt: warning: --listen-token is visible to every local user via ps; prefer --listen-token-file or %s\n", EnvListenTokenFile)
		return nil
	}

	token, err := listenTokenFromSources(cmd)
	if err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	if err := flags.Set("listen-token", token); err != nil {
		return fmt.Errorf("set listen token: %w", err)
	}
	return nil
}

// listenTokenFromSources returns the first token found in the out-of-band
// sources, or "" when none is configured.
func listenTokenFromSources(cmd *cobra.Command) (string, error) {
	path, _ := cmd.Flags().GetString("listen-token-file")
	from := "--listen-token-file"
	if path == "" {
		path, from = os.Getenv(EnvListenTokenFile), EnvListenTokenFile
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read listen token from %s (%s): %w", path, from, err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("listen token file %s (%s) is empty", path, from)
		}
		return token, nil
	}
	return strings.TrimSpace(os.Getenv(EnvListenToken)), nil
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
		// errSilent means the command already explained itself; anything
		// else has never been printed, because SilenceErrors is set.
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

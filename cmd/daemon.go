package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func init() {
	daemonCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run alerts headless (no TUI)",
		Long: `Subscribes to the configured providers, evaluates alerts, and fires
all configured notifiers (desktop, webhook, ntfy, Pushover, history)
without showing a TUI. Useful on a VPS / always-on machine. Stops on
SIGTERM or SIGINT.

The daemon runs the same data plane as the dashboard, so it also keeps
the portfolio equity curve, news, macro, futures and calendar histories
up to date — previously those only advanced while a TUI was open.`,
		RunE: runDaemon,
	}
	daemonCmd.Flags().Bool("force", false,
		"start and keep writing config even when the config file does not parse (a timestamped backup is taken before any write)")
	rootCmd.AddCommand(daemonCmd)
}

// runDaemon runs the shared backend headless.
//
// It deliberately owns no TUI-specific state: setupBackend and startDataPlane
// are the same calls `mkt` and `mkt serve` make, and broadcast.Send with no
// attached senders is a no-op, so every poller, the equity marker and the
// history seeding all run exactly as they do under the dashboard. Keeping one
// implementation is the point — a hand-copied daemon is how it ended up
// producing no equity marks, no news and no calendar at all.
func runDaemon(cmd *cobra.Command, args []string) error {
	b, cleanup, err := setupBackend(optsFromFlags(cmd, false))
	if err != nil {
		return err
	}
	defer cleanup()

	if len(b.symbols) == 0 {
		return fmt.Errorf("no symbols configured")
	}

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

	// Read-only HTTP surface, gated identically to the dashboard path
	// (honors --require-token / --enable-webhook).
	apiShutdown, err := b.startAPIIfRequested(cmd)
	if err != nil {
		return err
	}
	defer apiShutdown()

	log.Printf("daemon: watching %d symbols, %d alert rules", len(b.symbols), len(b.alertEngine.Rules()))
	b.startDataPlane(ctx)

	<-ctx.Done()
	return nil
}

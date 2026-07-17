package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/detail"
	watchlistview "github.com/stxkxs/mkt/internal/tui/watchlist"
)

// stockTickers returns the subset of symbols that look like stocks. Used
// to feed Yahoo's earnings endpoint, which has no entries for crypto or
// FRED series. Classification is delegated to the shared symbol package.
func stockTickers(symbols []string) []string {
	var out []string
	for _, s := range symbols {
		if symbol.IsCrypto(s) || symbol.IsFRED(s) {
			continue
		}
		out = append(out, strings.ToUpper(s))
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

// runDashboard is the default command: build the shared backend, attach a
// single local program to its broadcaster, start the data plane, and run
// the TUI. `mkt serve` reuses the same backend to attach many programs.
func runDashboard(cmd *cobra.Command, args []string) error {
	b, cleanup, err := setupBackend(optsFromFlags(cmd, false))
	if err != nil {
		return err
	}
	defer cleanup()

	apiShutdown, err := b.startAPIIfRequested(cmd)
	if err != nil {
		return err
	}
	defer apiShutdown()

	app := b.buildApp()
	p := tea.NewProgram(app)
	b.bc.Add(p)

	// The order-book level-2 streamer pushes into the live program. This
	// global is single-program by design; `mkt serve` leaves it unset and
	// falls back to REST snapshots per session.
	detail.SetLiveProgram(p)
	defer detail.SetLiveProgram(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.startDataPlane(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}
	return nil
}

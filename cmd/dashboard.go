package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/detail"
	watchlistview "github.com/stxkxs/mkt/internal/tui/watchlist"
)

// stockTickers returns the subset of symbols Yahoo's earnings endpoint can
// answer for: plain equity tickers. Crypto and FRED series have no earnings,
// and neither do the index / futures / FX pseudo-tickers (^GSPC, GC=F,
// EURUSD=X) that symbol.IsStock now routes to Yahoo — asking about those
// wastes a request per symbol and returns nothing. Symbols are canonicalized
// so the earnings lookup uses the same spelling as everything else.
func stockTickers(symbols []string) []string {
	var out []string
	for _, s := range symbols {
		c := symbol.Canonical(s)
		if c == "" || symbol.IsCrypto(c) || symbol.IsFRED(c) {
			continue
		}
		if strings.ContainsAny(c, "^=") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dedupeUnion flattens every group's symbols into a deduplicated slice,
// canonicalized so a hand-typed `btc` or `AAPL ` lines up with the quotes
// the providers emit.
func dedupeUnion(groups []watchlistview.Group) []string {
	var flat []string
	for _, g := range groups {
		flat = append(flat, g.Symbols...)
	}
	return canonicalDedupe(flat)
}

// subscribeSymbols returns every symbol the data plane must subscribe to:
// the watchlist union plus every symbol named by a portfolio — both the
// snapshot holdings and the transaction ledger.
//
// Holdings are the reason this is not just the watchlist. `mkt portfolio
// import` (the documented way to get holdings in) does not touch the
// watchlist, so a holding that is not also watched would never be
// subscribed, never be priced, and would show up in the portfolio tab as an
// invented break-even row folded into the total. Transactions are included
// too: a ledger can name a symbol that has since been sold to zero, and
// pricing it is what makes realized-vs-unrealized numbers add up.
func subscribeSymbols(groups []watchlistview.Group, portfolios []config.Portfolio) []string {
	var flat []string
	for _, g := range groups {
		flat = append(flat, g.Symbols...)
	}
	for _, p := range portfolios {
		for _, h := range p.Holdings {
			flat = append(flat, h.Symbol)
		}
		for _, t := range p.Transactions {
			flat = append(flat, t.Symbol)
		}
	}
	return canonicalDedupe(flat)
}

// canonicalDedupe canonicalizes every symbol, drops blanks, and removes
// duplicates while preserving first-seen order.
func canonicalDedupe(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		c := symbol.Canonical(s)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// runDashboard is the default command: build the shared backend, attach a
// single local program to its broadcaster, start the data plane, and run
// the TUI. `mkt serve` reuses the same backend to attach many programs.
//
// The program is registered with the broadcaster before the data plane
// starts, so the first round of poller output (macro, news, futures) is not
// broadcast into an empty room. Routing is only resolved by hub.Start, so
// the wiring pass runs a second time afterwards to hand the model the
// symbols no provider accepted — still before p.Run, so nothing races the
// Bubbletea update loop.
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// From here until Run returns the TUI owns the terminal, so nothing may
	// write to it but bubbletea. Everything the data plane logs goes to a
	// file instead.
	restoreLog := captureLog()
	defer restoreLog()

	app := b.buildApp(ctx)
	p := tea.NewProgram(app)
	b.bc.Add(p)

	// The order-book level-2 streamer pushes into the live program. This
	// global is single-program by design; `mkt serve` leaves it unset and
	// falls back to REST snapshots per session.
	detail.SetLiveProgram(p)
	defer detail.SetLiveProgram(nil)

	b.startDataPlane(ctx)
	b.applyWiring(ctx, app)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}
	return nil
}

package config

import (
	"testing"

	"github.com/stxkxs/mkt/internal/symbol"
)

// The shipped default counts, pinned. These numbers show up in the docs and
// in the `mkt config validate` output, so a silent drift here turns the
// documentation into a lie. Update the constants deliberately, in the same
// change that adds or removes a default.
const (
	wantDefaultWatchlist  = 163
	wantDefaultWatchlists = 11
	wantDefaultPortfolios = 12
	wantDefaultAlerts     = 11
)

func TestDefaultCounts(t *testing.T) {
	t.Parallel()
	if got := len(DefaultWatchlist); got != wantDefaultWatchlist {
		t.Errorf("len(DefaultWatchlist) = %d, want %d", got, wantDefaultWatchlist)
	}
	if got := len(DefaultWatchlists); got != wantDefaultWatchlists {
		t.Errorf("len(DefaultWatchlists) = %d, want %d", got, wantDefaultWatchlists)
	}
	if got := len(DefaultPortfolios); got != wantDefaultPortfolios {
		t.Errorf("len(DefaultPortfolios) = %d, want %d", got, wantDefaultPortfolios)
	}
	if got := len(DefaultAlerts); got != wantDefaultAlerts {
		t.Errorf("len(DefaultAlerts) = %d, want %d", got, wantDefaultAlerts)
	}
}

// Every symbol we ship must already be in canonical form. If it is not, the
// quotes streaming back from the providers are keyed differently than the
// config, and the row sits there forever showing no price.
func TestDefaultSymbolsAreCanonical(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, where, s string) {
		t.Helper()
		if c := symbol.Canonical(s); c != s {
			t.Errorf("%s: %q is not canonical, want %q", where, s, c)
		}
	}
	for _, s := range DefaultWatchlist {
		check(t, "DefaultWatchlist", s)
	}
	for _, w := range DefaultWatchlists {
		for _, s := range w.Symbols {
			check(t, "DefaultWatchlists["+w.Name+"]", s)
		}
	}
	for _, p := range DefaultPortfolios {
		for _, h := range p.Holdings {
			check(t, "DefaultPortfolios["+p.Name+"]", h.Symbol)
		}
	}
	for _, r := range DefaultAlerts {
		check(t, "DefaultAlerts", r.Symbol)
	}
	for s := range DefaultNotes {
		check(t, "DefaultNotes", s)
	}
	for _, s := range DefaultEDGARTickers {
		check(t, "DefaultEDGARTickers", s)
	}
}

// Every default symbol must route to exactly one provider — a symbol no
// provider claims is a permanently blank row.
func TestDefaultSymbolsRoute(t *testing.T) {
	t.Parallel()
	for _, s := range DefaultWatchlist {
		n := 0
		for _, ok := range []bool{symbol.IsFRED(s), symbol.IsCrypto(s), symbol.IsStock(s)} {
			if ok {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%q is claimed by %d providers, want exactly 1", s, n)
		}
	}
}

// Tickers that no longer resolve. EATON was never a ticker (Eaton is ETN),
// SQ was retired when Block moved to XYZ, and GOLD now belongs to a thinly
// traded instrument that is not Barrick (which trades as B) — so a GOLD row
// showed plausible but wrong prices. These are the exact regressions the
// data fix addressed; keep them out.
func TestDefaultsHaveNoRetiredTickers(t *testing.T) {
	t.Parallel()
	retired := map[string]string{
		"EATON": "not a ticker; Eaton Corporation is ETN",
		"SQ":    "retired; Block trades as XYZ",
		"GOLD":  "not Barrick; Barrick Mining trades as B",
	}
	seen := func(t *testing.T, where, s string) {
		t.Helper()
		if why, bad := retired[s]; bad {
			t.Errorf("%s still ships %q: %s", where, s, why)
		}
	}
	for _, s := range DefaultWatchlist {
		seen(t, "DefaultWatchlist", s)
	}
	for _, w := range DefaultWatchlists {
		for _, s := range w.Symbols {
			seen(t, "DefaultWatchlists["+w.Name+"]", s)
		}
	}
	for _, p := range DefaultPortfolios {
		for _, h := range p.Holdings {
			seen(t, "DefaultPortfolios["+p.Name+"]", h.Symbol)
		}
	}
}

// The flat watchlist is the "everything" group; a duplicate would render the
// same row twice.
func TestDefaultWatchlistHasNoDuplicates(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, len(DefaultWatchlist))
	for _, s := range DefaultWatchlist {
		if seen[s] {
			t.Errorf("DefaultWatchlist contains %q twice", s)
		}
		seen[s] = true
	}
}

// Every symbol held in a default portfolio or watched by a default alert
// should also be in the flat watchlist, so a fresh install can navigate to
// it. Crypto pairs and index symbols included.
func TestDefaultPortfolioSymbolsAreWatched(t *testing.T) {
	t.Parallel()
	watched := make(map[string]bool, len(DefaultWatchlist))
	for _, s := range DefaultWatchlist {
		watched[s] = true
	}
	for _, p := range DefaultPortfolios {
		for _, h := range p.Holdings {
			if !watched[h.Symbol] {
				t.Errorf("portfolio %q holds %s, which is not in DefaultWatchlist", p.Name, h.Symbol)
			}
		}
	}
	for _, r := range DefaultAlerts {
		if !watched[r.Symbol] {
			t.Errorf("default alert on %s, which is not in DefaultWatchlist", r.Symbol)
		}
	}
}

// Holdings carry a display name; a blank one renders an empty column.
func TestDefaultHoldingsAreLabelled(t *testing.T) {
	t.Parallel()
	for _, p := range DefaultPortfolios {
		if p.Name == "" {
			t.Error("a default portfolio has no name")
		}
		for _, h := range p.Holdings {
			if h.Name == "" {
				t.Errorf("portfolio %q: holding %s has no display name", p.Name, h.Symbol)
			}
			if h.Quantity <= 0 {
				t.Errorf("portfolio %q: holding %s has quantity %v", p.Name, h.Symbol, h.Quantity)
			}
			if h.CostBasis <= 0 {
				t.Errorf("portfolio %q: holding %s has cost basis %v", p.Name, h.Symbol, h.CostBasis)
			}
		}
	}
}

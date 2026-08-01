package cmd

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

const (
	// seedConcurrency bounds how many history requests are in flight. The
	// providers rate-limit internally, so this only controls how deep the
	// queue in front of that limiter gets — it does not set the pace.
	seedConcurrency = 4

	// seedTimeout caps one symbol's history request. It has to be generous:
	// the provider limiter paces a large watchlist over minutes, and a
	// deadline shorter than the queue makes every late request fail
	// instantly with "would exceed context deadline" rather than wait its
	// turn. The budget below is what actually stops the backfill.
	seedTimeout = 2 * time.Minute

	// seedBudget caps the whole backfill. Whatever is not seeded by then
	// simply fills from live ticks instead, so this is a bound on effort,
	// not a correctness deadline.
	seedBudget = 10 * time.Minute

	// seedInterval is the bar size the ring is filled with. Daily bars are
	// what make indicator alerts mean what they say: RSI(14) over 14 days
	// rather than over 14 poll ticks (~3.5 minutes of noise).
	seedInterval = provider.Interval1d

	// defaultSeedBars mirrors market.NewCache's default ring size, used when
	// sparkline_len is unset.
	defaultSeedBars = 60
)

// seedCache backfills every subscribed symbol's ring buffer from history so
// the dashboard is useful from its first paint.
//
// Without it the cache is fed only by live ticks: a stock sparkline stays
// blank for the ~15 minutes it takes to poll enough points, and an RSI(14)
// or SMA-cross alert evaluates over a handful of consecutive polls instead
// of over days. Seeding is strictly additive — market.Cache.Seed refuses to
// touch a symbol twice and never sets a latest price, so a historical close
// can never be mistaken for a live quote, and live ticks that land during
// the backfill win.
//
// Symbols are seeded in subscription order, which puts the first watchlist
// group first, so the rows the user is looking at fill in before the tail of
// a long watchlist. Failures are expected in bulk (a rate-limited provider
// with 150 symbols queued, a ticker with no history) and are counted rather
// than logged one by one — a per-symbol log line here would scroll a hundred
// lines of noise over the dashboard on every start.
func (b *backend) seedCache(ctx context.Context) {
	limit := b.cfg.SparklineLen
	if limit <= 0 {
		limit = defaultSeedBars
	}

	ctx, cancel := context.WithTimeout(ctx, seedBudget)
	defer cancel()

	var (
		seeded, failed atomic.Int64
		firstErrOnce   sync.Once
		firstErr       error
		firstErrSymbol string
	)

	sem := make(chan struct{}, seedConcurrency)
	var wg sync.WaitGroup
	for _, sym := range b.symbols {
		if b.cache.Seeded(sym) {
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			defer func() { <-sem }()

			n, err := b.seedSymbol(ctx, sym, limit)
			switch {
			case err != nil:
				failed.Add(1)
				firstErrOnce.Do(func() { firstErr, firstErrSymbol = err, sym })
			case n > 0:
				seeded.Add(1)
			}
		}(sym)
	}
	wg.Wait()

	// One line, and only when there is something to say. A fully successful
	// backfill is the expected case and stays silent.
	if f := failed.Load(); f > 0 && ctx.Err() == nil {
		log.Printf("history backfill: seeded %d/%d symbols, %d unavailable (first: %s: %v)",
			seeded.Load(), seeded.Load()+f, f, firstErrSymbol, firstErr)
	}
}

// seedSymbol backfills one symbol, returning how many bars were stored.
func (b *backend) seedSymbol(ctx context.Context, sym string, limit int) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, seedTimeout)
	defer cancel()

	candles, err := b.histProvider.History(reqCtx, provider.HistoryParams{
		Symbol:   sym,
		Interval: seedInterval,
		Limit:    limit,
	})
	if err != nil {
		return 0, err
	}
	if len(candles) == 0 {
		return 0, nil
	}
	b.cache.SeedCandles(sym, candles)
	return len(candles), nil
}

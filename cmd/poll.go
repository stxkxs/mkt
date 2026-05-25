package cmd

import (
	"context"
	"time"
)

// poll runs fetch immediately, then on every tick of interval until
// ctx is cancelled. Used by the background polling goroutines that
// feed the macro / futures / DeFi / equity / news views. Replaces the
// repeated `ticker := NewTicker; fetch(); for { select { Done / C } }`
// boilerplate.
func poll(ctx context.Context, interval time.Duration, fetch func()) {
	fetch()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetch()
		}
	}
}

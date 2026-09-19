package correlation

import (
	"math"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/market"
)

// benchModel is a full window of symbols over full rings: the shape the tab
// has on a wide terminal watching a real watchlist.
func benchModel(n int) Model {
	cache := market.NewCache(60)
	base := time.Unix(1_700_000_000, 0).UTC()
	syms := symbols(n)
	for i, s := range syms {
		prices := make([]float64, 60)
		for j := range prices {
			prices[j] = 100 + float64(i) + 5*math.Sin(float64(j+i)/4)
		}
		push(cache, s, base, 30*time.Second, prices)
	}
	m := New(syms, cache)
	m.SetSize(200, 40)
	return m
}

// The pair measures what a frame over an unchanged cache costs against what
// it would cost to rebuild: the memoized run must allocate nothing, because
// every allocation on this path is one Bubbletea makes after every Update.
func BenchmarkMatrixMemoized(b *testing.B) {
	m := benchModel(20)
	syms, _ := m.window()
	_ = m.matrixFor(syms)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.matrixFor(syms)
	}
}

// A nil memo is the uncached path, which is also what a Model built by hand
// rather than by New takes.
func BenchmarkMatrixUncached(b *testing.B) {
	m := benchModel(20)
	syms, _ := m.window()
	m.memo = nil
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.matrixFor(syms)
	}
}

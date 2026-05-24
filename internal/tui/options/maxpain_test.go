package options

import (
	"math"
	"testing"

	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

func TestMaxPainSimple(t *testing.T) {
	// Two strikes: 100 and 110.
	// At settle=100: calls payout (100-100)*OI=0 + (110-100 negative, skip)=0; puts (100<100 skip)=0 → 0
	// At settle=110: calls (110-100)*1000=10000 + (110-110)*OI=0 → 10000; puts skip → 10000
	// So MaxPain = 100.
	chain := yahoo.OptionsChain{
		Calls: []yahoo.Option{
			{Strike: 100, OpenInterest: 1000},
			{Strike: 110, OpenInterest: 500},
		},
		Puts: []yahoo.Option{
			{Strike: 100, OpenInterest: 1000},
			{Strike: 110, OpenInterest: 500},
		},
	}
	got := MaxPain(chain)
	if got != 100 {
		t.Errorf("got %v, want 100", got)
	}
}

func TestMaxPainEmpty(t *testing.T) {
	got := MaxPain(yahoo.OptionsChain{})
	if !math.IsNaN(got) {
		t.Errorf("empty should yield NaN, got %v", got)
	}
}

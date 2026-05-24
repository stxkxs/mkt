package options

import (
	"math"

	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

// MaxPain returns the strike that minimizes the total cash payout
// option-writers would owe option-holders at expiration. With no
// strikes returns NaN.
func MaxPain(chain yahoo.OptionsChain) float64 {
	strikes := uniqueStrikes(chain)
	if len(strikes) == 0 {
		return math.NaN()
	}
	bestStrike := strikes[0]
	bestPain := math.Inf(1)
	for _, s := range strikes {
		pain := totalPainAtStrike(chain, s)
		if pain < bestPain {
			bestPain = pain
			bestStrike = s
		}
	}
	return bestStrike
}

func totalPainAtStrike(chain yahoo.OptionsChain, settle float64) float64 {
	var pain float64
	for _, c := range chain.Calls {
		if settle > c.Strike {
			pain += (settle - c.Strike) * float64(c.OpenInterest)
		}
	}
	for _, p := range chain.Puts {
		if settle < p.Strike {
			pain += (p.Strike - settle) * float64(p.OpenInterest)
		}
	}
	return pain
}

func uniqueStrikes(chain yahoo.OptionsChain) []float64 {
	seen := map[float64]struct{}{}
	for _, c := range chain.Calls {
		seen[c.Strike] = struct{}{}
	}
	for _, p := range chain.Puts {
		seen[p.Strike] = struct{}{}
	}
	out := make([]float64, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}

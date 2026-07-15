// Package defillama fetches DeFi TVL data from DeFiLlama's public API.
// No API key required.
package defillama

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
)

// BaseURL is the DeFiLlama API root; exported so tests can override.
var BaseURL = "https://api.llama.fi"

var client = &http.Client{Timeout: 10 * time.Second}

// TVLSnapshot is a per-chain total-value-locked entry with short-term
// change metrics. Zero values indicate missing data.
type TVLSnapshot struct {
	Chain    string
	TVL      float64
	Change1d float64 // percent change over 1 day
	Change7d float64 // percent change over 7 days
}

type apiChain struct {
	Name       string  `json:"name"`
	TVL        float64 `json:"tvl"`
	Change1d   float64 `json:"change_1d"`
	Change7d   float64 `json:"change_7d"`
	GeckoID    string  `json:"gecko_id"`
	TokenSym   string  `json:"tokenSymbol"`
	CMCID      string  `json:"cmcId"`
	ChainIDNum int     `json:"chainId"`
}

// FetchChains returns DeFi TVL per chain, sorted descending by TVL.
func FetchChains(ctx context.Context) ([]TVLSnapshot, error) {
	var raw []apiChain
	if err := httpx.GetJSON(ctx, client, BaseURL+"/v2/chains", map[string]string{"Accept": "application/json"}, &raw); err != nil {
		return nil, fmt.Errorf("defillama: %w", err)
	}
	out := make([]TVLSnapshot, 0, len(raw))
	for _, c := range raw {
		if c.Name == "" {
			continue
		}
		out = append(out, TVLSnapshot{
			Chain:    c.Name,
			TVL:      c.TVL,
			Change1d: c.Change1d,
			Change7d: c.Change7d,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TVL > out[j].TVL })
	return out, nil
}

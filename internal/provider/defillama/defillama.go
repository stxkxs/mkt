// Package defillama fetches DeFi TVL data from DeFiLlama's public API.
// No API key required.
package defillama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
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
	url := BaseURL + "/v2/chains"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("defillama: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("defillama: get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("defillama: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("defillama: read: %w", err)
	}
	var raw []apiChain
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("defillama: decode: %w", err)
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

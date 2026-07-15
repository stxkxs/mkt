// Package binance fetches public Binance futures data (funding rate
// and open interest). No API key required.
package binance

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
)

var jsonHeaders = map[string]string{"Accept": "application/json"}

// PremiumIndexURL and OpenInterestURL are exported so tests can override
// them with httptest servers.
var (
	PremiumIndexURL = "https://fapi.binance.com/fapi/v1/premiumIndex"
	OpenInterestURL = "https://fapi.binance.com/fapi/v1/openInterest"
)

var client = &http.Client{Timeout: 10 * time.Second}

// FuturesSnapshot is the per-symbol futures snapshot.
type FuturesSnapshot struct {
	Symbol       string
	FundingRate  float64 // fraction; 0.0001 = 0.01% per 8h
	MarkPrice    float64
	OpenInterest float64 // contracts
}

type premiumIndexResp struct {
	Symbol          string `json:"symbol"`
	MarkPrice       string `json:"markPrice"`
	LastFundingRate string `json:"lastFundingRate"`
}

type openInterestResp struct {
	OpenInterest string `json:"openInterest"`
	Symbol       string `json:"symbol"`
}

// FetchFuturesSnapshot pulls premium-index and open-interest data for
// every symbol concurrently. A failed call on one endpoint leaves the
// corresponding field zero rather than dropping the symbol.
func FetchFuturesSnapshot(ctx context.Context, symbols []string) []FuturesSnapshot {
	if len(symbols) == 0 {
		return nil
	}
	out := make([]FuturesSnapshot, len(symbols))
	var wg sync.WaitGroup
	for i, s := range symbols {
		wg.Add(1)
		go func(idx int, sym string) {
			defer wg.Done()
			out[idx] = fetchOne(ctx, sym)
		}(i, s)
	}
	wg.Wait()
	return out
}

func fetchOne(ctx context.Context, symbol string) FuturesSnapshot {
	snap := FuturesSnapshot{Symbol: symbol}
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var pi premiumIndexResp
		if err := httpx.GetJSON(ctx, client, PremiumIndexURL+"?symbol="+symbol, jsonHeaders, &pi); err == nil {
			snap.FundingRate, _ = strconv.ParseFloat(pi.LastFundingRate, 64)
			snap.MarkPrice, _ = strconv.ParseFloat(pi.MarkPrice, 64)
		}
	}()

	go func() {
		defer wg.Done()
		var oi openInterestResp
		if err := httpx.GetJSON(ctx, client, OpenInterestURL+"?symbol="+symbol, jsonHeaders, &oi); err == nil {
			snap.OpenInterest, _ = strconv.ParseFloat(oi.OpenInterest, 64)
		}
	}()

	wg.Wait()
	return snap
}

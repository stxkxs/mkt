// Package binance fetches public Binance futures data (funding rate
// and open interest). No API key required.
package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

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
		if err := getJSON(ctx, PremiumIndexURL+"?symbol="+symbol, &pi); err == nil {
			snap.FundingRate, _ = strconv.ParseFloat(pi.LastFundingRate, 64)
			snap.MarkPrice, _ = strconv.ParseFloat(pi.MarkPrice, 64)
		}
	}()

	go func() {
		defer wg.Done()
		var oi openInterestResp
		if err := getJSON(ctx, OpenInterestURL+"?symbol="+symbol, &oi); err == nil {
			snap.OpenInterest, _ = strconv.ParseFloat(oi.OpenInterest, 64)
		}
	}()

	wg.Wait()
	return snap
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", url, err)
	}
	return json.Unmarshal(body, out)
}

// Package binance fetches public Binance futures data (funding rate
// and open interest). No API key required.
package binance

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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
//
// Funding, MarkPrice and OpenInterest are only meaningful when the
// corresponding Have* flag is set. Binance answers HTTP 451 to requests from
// restricted jurisdictions (the US among them), and a zeroed snapshot rendered
// as "funding +0.0000% OI 0" is indistinguishable from a real flat market —
// so absence is tracked explicitly rather than encoded as zero.
type FuturesSnapshot struct {
	Symbol       string
	FundingRate  float64 // fraction; 0.0001 = 0.01% per 8h
	MarkPrice    float64
	OpenInterest float64 // contracts

	HavePremium bool  // premium-index call succeeded (FundingRate, MarkPrice)
	HaveOI      bool  // open-interest call succeeded (OpenInterest)
	Err         error // why the snapshot is incomplete; nil when fully populated
}

// OK reports whether every field of the snapshot came from Binance.
func (s FuturesSnapshot) OK() bool { return s.HavePremium && s.HaveOI }

// Unavailable reports whether nothing at all could be fetched, which is what
// a geo-block or an outage looks like. Callers should render these as
// unavailable rather than as zeros.
func (s FuturesSnapshot) Unavailable() bool { return !s.HavePremium && !s.HaveOI }

// Restricted reports whether the failure was Binance refusing the request on
// jurisdiction grounds (HTTP 451), which is permanent for this host rather
// than a transient outage worth retrying loudly.
func (s FuturesSnapshot) Restricted() bool {
	var se *httpx.StatusError
	return errors.As(s.Err, &se) && se.Code == http.StatusUnavailableForLegalReasons
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

// FetchFuturesSnapshot pulls premium-index and open-interest data for every
// symbol concurrently. Symbols are always returned in the order given; a
// failed endpoint leaves its Have* flag false and records the error, so the
// caller can tell "no funding" apart from "funding is zero".
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
	var (
		mu             sync.Mutex
		premErr, oiErr error
	)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var pi premiumIndexResp
		err := httpx.GetJSON(ctx, client, PremiumIndexURL+"?symbol="+url.QueryEscape(symbol), jsonHeaders, &pi)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			premErr = err
			return
		}
		snap.FundingRate, _ = strconv.ParseFloat(pi.LastFundingRate, 64)
		snap.MarkPrice, _ = strconv.ParseFloat(pi.MarkPrice, 64)
		snap.HavePremium = true
	}()

	go func() {
		defer wg.Done()
		var oi openInterestResp
		err := httpx.GetJSON(ctx, client, OpenInterestURL+"?symbol="+url.QueryEscape(symbol), jsonHeaders, &oi)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			oiErr = err
			return
		}
		snap.OpenInterest, _ = strconv.ParseFloat(oi.OpenInterest, 64)
		snap.HaveOI = true
	}()

	wg.Wait()
	// Prefer the premium-index error: it is the call that carries funding and
	// mark price, and both endpoints fail identically under a 451.
	if premErr != nil {
		snap.Err = premErr
	} else if oiErr != nil {
		snap.Err = oiErr
	}
	return snap
}

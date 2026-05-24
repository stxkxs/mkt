package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newJSONServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestFetchHappyPath(t *testing.T) {
	pi := newJSONServer(t, `{"symbol":"BTCUSDT","markPrice":"60000.5","lastFundingRate":"0.0001"}`, 200)
	defer pi.Close()
	oi := newJSONServer(t, `{"openInterest":"123456.78","symbol":"BTCUSDT"}`, 200)
	defer oi.Close()

	prevPI, prevOI := PremiumIndexURL, OpenInterestURL
	PremiumIndexURL = pi.URL
	OpenInterestURL = oi.URL
	defer func() { PremiumIndexURL, OpenInterestURL = prevPI, prevOI }()

	got := FetchFuturesSnapshot(context.Background(), []string{"BTCUSDT"})
	if len(got) != 1 {
		t.Fatalf("want 1 snapshot, got %d", len(got))
	}
	s := got[0]
	if s.Symbol != "BTCUSDT" {
		t.Errorf("symbol = %q", s.Symbol)
	}
	if s.MarkPrice != 60000.5 {
		t.Errorf("markPrice = %v want 60000.5", s.MarkPrice)
	}
	if s.FundingRate != 0.0001 {
		t.Errorf("fundingRate = %v want 0.0001", s.FundingRate)
	}
	if s.OpenInterest != 123456.78 {
		t.Errorf("openInterest = %v want 123456.78", s.OpenInterest)
	}
}

func TestFetchPartialFailure(t *testing.T) {
	// Premium succeeds, openInterest fails — snapshot should still come back
	// with MarkPrice/FundingRate set and OpenInterest = 0.
	pi := newJSONServer(t, `{"symbol":"BTCUSDT","markPrice":"60000","lastFundingRate":"0.0002"}`, 200)
	defer pi.Close()
	oi := newJSONServer(t, "", 500)
	defer oi.Close()

	prevPI, prevOI := PremiumIndexURL, OpenInterestURL
	PremiumIndexURL = pi.URL
	OpenInterestURL = oi.URL
	defer func() { PremiumIndexURL, OpenInterestURL = prevPI, prevOI }()

	got := FetchFuturesSnapshot(context.Background(), []string{"BTCUSDT"})
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].FundingRate != 0.0002 {
		t.Errorf("funding lost: %v", got[0].FundingRate)
	}
	if got[0].OpenInterest != 0 {
		t.Errorf("OI should be zero on failure, got %v", got[0].OpenInterest)
	}
}

func TestFetchEmptySymbols(t *testing.T) {
	if got := FetchFuturesSnapshot(context.Background(), nil); got != nil {
		t.Errorf("nil symbols → nil, got %v", got)
	}
}

func TestFetchMalformedJSON(t *testing.T) {
	pi := newJSONServer(t, "not json", 200)
	defer pi.Close()
	oi := newJSONServer(t, "not json", 200)
	defer oi.Close()

	prevPI, prevOI := PremiumIndexURL, OpenInterestURL
	PremiumIndexURL = pi.URL
	OpenInterestURL = oi.URL
	defer func() { PremiumIndexURL, OpenInterestURL = prevPI, prevOI }()

	got := FetchFuturesSnapshot(context.Background(), []string{"BTCUSDT"})
	if len(got) != 1 {
		t.Fatalf("want 1 snapshot")
	}
	if got[0].FundingRate != 0 || got[0].OpenInterest != 0 {
		t.Errorf("malformed JSON should yield zeros, got %+v", got[0])
	}
}

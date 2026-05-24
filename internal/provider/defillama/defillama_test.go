package defillama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleChains = `[
  {"name":"Ethereum","tvl":60000000000,"change_1d":1.5,"change_7d":-2.3,"gecko_id":"ethereum"},
  {"name":"Tron","tvl":8000000000,"change_1d":-0.5,"change_7d":1.2},
  {"name":"Solana","tvl":12000000000,"change_1d":2.1,"change_7d":5.7},
  {"name":"","tvl":1,"change_1d":0,"change_7d":0}
]`

func newChainsServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestFetchChainsHappyPath(t *testing.T) {
	srv := newChainsServer(t, sampleChains, 200)
	defer srv.Close()
	prev := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = prev }()

	got, err := FetchChains(context.Background())
	if err != nil {
		t.Fatalf("FetchChains: %v", err)
	}
	// 3 valid entries (empty name skipped)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	// Sorted by TVL desc → Ethereum, Solana, Tron
	if got[0].Chain != "Ethereum" {
		t.Errorf("first chain = %q, want Ethereum", got[0].Chain)
	}
	if got[1].Chain != "Solana" {
		t.Errorf("second chain = %q, want Solana", got[1].Chain)
	}
	if got[2].Chain != "Tron" {
		t.Errorf("third chain = %q, want Tron", got[2].Chain)
	}
	if got[0].Change1d != 1.5 || got[0].Change7d != -2.3 {
		t.Errorf("Ethereum change wrong: %+v", got[0])
	}
}

func TestFetchChains500ReturnsError(t *testing.T) {
	srv := newChainsServer(t, "", 500)
	defer srv.Close()
	prev := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = prev }()

	_, err := FetchChains(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestFetchChainsMalformedReturnsError(t *testing.T) {
	srv := newChainsServer(t, "not json", 200)
	defer srv.Close()
	prev := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = prev }()

	_, err := FetchChains(context.Background())
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

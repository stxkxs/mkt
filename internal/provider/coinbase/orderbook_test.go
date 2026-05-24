package coinbase

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleBook = `{
  "sequence": 12345,
  "bids": [
    ["100.00","1.0","2"],
    ["99.50","2.0","3"],
    ["99.00","0.5","1"]
  ],
  "asks": [
    ["100.50","1.5","2"],
    ["101.00","3.0","4"],
    ["101.50","0.25","1"]
  ]
}`

func TestFetchOrderBookHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleBook))
	}))
	defer srv.Close()
	prev := OrderBookURL
	OrderBookURL = srv.URL
	defer func() { OrderBookURL = prev }()

	p := New()
	book, err := p.FetchOrderBook(context.Background(), "BTC-USD")
	if err != nil {
		t.Fatalf("FetchOrderBook: %v", err)
	}
	if book.Sequence != 12345 {
		t.Errorf("sequence: got %d, want 12345", book.Sequence)
	}
	if len(book.Bids) != 3 || len(book.Asks) != 3 {
		t.Fatalf("expected 3/3, got %d/%d", len(book.Bids), len(book.Asks))
	}
	// Bids descending: 100, 99.50, 99
	if book.Bids[0].Price != 100 || book.Bids[2].Price != 99 {
		t.Errorf("bids not sorted desc: %+v", book.Bids)
	}
	// Asks ascending: 100.50, 101, 101.50
	if book.Asks[0].Price != 100.5 || book.Asks[2].Price != 101.5 {
		t.Errorf("asks not sorted asc: %+v", book.Asks)
	}
}

func TestOrderBookDepth(t *testing.T) {
	book := OrderBook{
		Bids: []Level{{Price: 100, Size: 1}, {Price: 99, Size: 2}, {Price: 98, Size: 5}},
		Asks: []Level{{Price: 101, Size: 1.5}, {Price: 102, Size: 0.5}},
	}
	bid, ask := OrderBookDepth(book, 2)
	if math.Abs(bid-3) > 1e-9 {
		t.Errorf("top-2 bid depth: got %v want 3", bid)
	}
	if math.Abs(ask-2) > 1e-9 {
		t.Errorf("top-2 ask depth: got %v want 2", ask)
	}
}

func TestFetchOrderBook500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	prev := OrderBookURL
	OrderBookURL = srv.URL
	defer func() { OrderBookURL = prev }()

	p := New()
	_, err := p.FetchOrderBook(context.Background(), "BTC-USD")
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

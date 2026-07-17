package market

import (
	"testing"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestLatestQuoteRetainsFullQuote(t *testing.T) {
	c := NewCache(60)
	c.Push(provider.Quote{Symbol: "BTC-USD", Price: 60000, Change: 1200, ChangePct: 2.0})
	c.Push(provider.Quote{Symbol: "BTC-USD", Price: 61000, Change: 2200, ChangePct: 3.7})

	q, ok := c.LatestQuote("BTC-USD")
	if !ok {
		t.Fatal("LatestQuote(BTC-USD): not found")
	}
	if q.Price != 61000 || q.ChangePct != 3.7 || q.Change != 2200 {
		t.Errorf("got %+v, want latest push (price=61000 change=2200 pct=3.7)", q)
	}

	// Price ring still tracks history for sparklines.
	if prices := c.Prices("BTC-USD"); len(prices) != 2 {
		t.Errorf("Prices len = %d, want 2", len(prices))
	}
}

func TestLatestQuoteUnknownSymbol(t *testing.T) {
	c := NewCache(60)
	if _, ok := c.LatestQuote("NOPE"); ok {
		t.Error("LatestQuote(NOPE): want not found")
	}
}

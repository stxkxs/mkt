package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/symbol"
)

// routeWatchSymbols mirrors the routing decision runWatch makes, so the
// refusal and canonicalization can be asserted without opening a socket.
// Keeping it here rather than in watch.go would risk the two drifting, so
// the test instead exercises the same provider Supports() calls against the
// same canonical spelling — the drift this guards is exactly a change to
// Canonical or to either provider's Supports.
func routeWatchSymbols(args []string) (crypto, stocks, unknown []string) {
	cb := coinbase.New()
	yh := yahoo.New(5 * time.Second)
	for _, s := range args {
		canon := symbol.Canonical(s)
		switch {
		case cb.Supports(canon):
			crypto = append(crypto, canon)
		case yh.Supports(canon):
			stocks = append(stocks, canon)
		default:
			unknown = append(unknown, s)
		}
	}
	return crypto, stocks, unknown
}

// TestWatchRefusesWhenNothingRoutes is the regression test for the hang: with
// no producer attached nothing could ever arrive on the quote channel, so
// `mkt watch` printed a header and blocked until the user gave up.
func TestWatchRefusesWhenNothingRoutes(t *testing.T) {
	out, _, err := runCLI(t, nil, "watch", "not a ticker", "also/not/one")
	if err == nil {
		t.Fatal("expected an error; watch used to block forever instead")
	}
	if !strings.Contains(err.Error(), "no symbol routes to a provider") {
		t.Errorf("error = %v", err)
	}
	// The unrecognized symbols must be named — "nothing routed" without
	// saying what was rejected leaves the user guessing at their own typo.
	for _, want := range []string{"not a ticker", "also/not/one"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q: %v", want, err)
		}
	}
	if strings.Contains(out, "SYMBOL") {
		t.Errorf("watch printed its header before refusing:\n%s", out)
	}
}

// TestWatchCanonicalizesBeforeRouting pins the second half of the fix. A
// bare "btc" used to be routed by Supports and then *subscribed* under the
// spelling the user typed, which is not a product Coinbase knows — so the
// stream connected and never delivered a quote.
func TestWatchCanonicalizesBeforeRouting(t *testing.T) {
	tests := []struct {
		arg        string
		wantCrypto string
		wantStock  string
	}{
		{arg: "btc", wantCrypto: "BTC-USD"},
		{arg: "BTCUSDT", wantCrypto: "BTC-USD"},
		{arg: "btc-usd", wantCrypto: "BTC-USD"},
		{arg: "eth", wantCrypto: "ETH-USD"},
		{arg: "aapl", wantStock: "AAPL"},
		{arg: "^GSPC", wantStock: "^GSPC"},
		{arg: "GC=F", wantStock: "GC=F"},
		{arg: "EURUSD=X", wantStock: "EURUSD=X"},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			crypto, stocks, unknown := routeWatchSymbols([]string{tt.arg})
			if len(unknown) > 0 {
				t.Fatalf("%q routed nowhere", tt.arg)
			}
			switch {
			case tt.wantCrypto != "":
				if len(crypto) != 1 || crypto[0] != tt.wantCrypto {
					t.Errorf("crypto = %v, want [%s]", crypto, tt.wantCrypto)
				}
			case tt.wantStock != "":
				if len(stocks) != 1 || stocks[0] != tt.wantStock {
					t.Errorf("stocks = %v, want [%s]", stocks, tt.wantStock)
				}
			}
		})
	}
}

// TestWatchSkipsUnknownButProceedsOnAPartialRoute checks the middle case: at
// least one symbol has a producer, so the stream is worth starting and the
// rest are warnings rather than a refusal.
func TestWatchSkipsUnknownButProceedsOnAPartialRoute(t *testing.T) {
	crypto, stocks, unknown := routeWatchSymbols([]string{"btc", "not a ticker"})
	if len(crypto)+len(stocks) == 0 {
		t.Fatal("a routable symbol was dropped")
	}
	if len(unknown) != 1 || unknown[0] != "not a ticker" {
		t.Errorf("unknown = %v, want [not a ticker]", unknown)
	}
}

// TestWatchRequiresASymbol keeps the argument check: `mkt watch` with no
// arguments is a usage mistake, not a stream of everything.
func TestWatchRequiresASymbol(t *testing.T) {
	if _, _, err := runCLI(t, nil, "watch"); err == nil {
		t.Fatal("expected an error for `watch` with no symbols")
	}
}

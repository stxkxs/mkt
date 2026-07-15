package symbol

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		sym    string
		crypto bool
		fred   bool
		stock  bool
	}{
		// Coinbase pair format
		{"BTC-USD", true, false, false},
		{"eth-usdt", true, false, false}, // case-insensitive
		// Binance bare-pair format
		{"BTCUSDT", true, false, false},
		{"ETHBUSD", true, false, false},
		// Known bare bases — including ones Yahoo's old subset missed
		{"BTC", true, false, false},
		{"LINK", true, false, false},
		{"ATOM", true, false, false},
		{"WIF", true, false, false},
		// FRED series
		{"FRED:DGS10", false, true, false},
		{"fred:unrate", false, true, false},
		// Stocks
		{"AAPL", false, false, true},
		{"BRK.B", false, false, true},
		{"BRK-B", false, false, true},
		// Non-crypto pair-ish that isn't a known base: still a stock shape
		{"MSFT", false, false, true},
		// Too long / empty are not stocks
		{"TOOLONGTICKER", false, false, false},
		{"", false, false, false},
	}
	for _, tc := range tests {
		if got := IsCrypto(tc.sym); got != tc.crypto {
			t.Errorf("IsCrypto(%q) = %v, want %v", tc.sym, got, tc.crypto)
		}
		if got := IsFRED(tc.sym); got != tc.fred {
			t.Errorf("IsFRED(%q) = %v, want %v", tc.sym, got, tc.fred)
		}
		if got := IsStock(tc.sym); got != tc.stock {
			t.Errorf("IsStock(%q) = %v, want %v", tc.sym, got, tc.stock)
		}
	}
}

func TestMutuallyExclusive(t *testing.T) {
	// A symbol must fall into at most one of crypto / FRED / stock.
	for _, s := range []string{"BTC-USD", "AAPL", "FRED:DGS10", "BTCUSDT", "BRK.B", "LINK"} {
		n := 0
		if IsCrypto(s) {
			n++
		}
		if IsFRED(s) {
			n++
		}
		if IsStock(s) {
			n++
		}
		if n > 1 {
			t.Errorf("%q classified into %d categories, want ≤1", s, n)
		}
	}
}

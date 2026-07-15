// Package symbol is the single source of truth for classifying a market
// symbol as crypto, a FRED economic series, or a stock. Providers used to
// each carry their own (divergent) copy of "what is a crypto symbol",
// which only stayed correct because of provider ordering in the hub chain.
// Centralizing it here removes that hidden coupling: coinbase.Supports,
// yahoo.Supports, and the earnings ticker filter all delegate to these
// functions, so the lists can never drift apart again.
package symbol

import "strings"

// FREDPrefix marks macro/economic series handled by the FRED provider
// (history only), e.g. "FRED:DGS10".
const FREDPrefix = "FRED:"

// cryptoBases are bare tickers we treat as crypto and route to Coinbase as
// <base>-USD even without an explicit quote-currency suffix. This is the
// canonical set; add new coins here and every provider picks them up.
var cryptoBases = map[string]bool{
	"BTC": true, "ETH": true, "SOL": true, "XRP": true,
	"ADA": true, "DOGE": true, "AVAX": true, "DOT": true,
	"MATIC": true, "LINK": true, "UNI": true, "ATOM": true,
	"LTC": true, "NEAR": true, "FIL": true, "APT": true,
	"ARB": true, "OP": true, "SUI": true, "SEI": true,
	"BNB": true, "PEPE": true, "SHIB": true, "WIF": true,
}

// IsFRED reports whether the symbol addresses a FRED economic series.
func IsFRED(s string) bool {
	return strings.HasPrefix(strings.ToUpper(s), FREDPrefix)
}

// IsCrypto reports whether the symbol denotes a cryptocurrency — whether
// written as a Coinbase pair (BTC-USD, ETH-USDT), a Binance-style bare
// pair (BTCUSDT), or a known bare base (BTC). FRED series are never crypto.
func IsCrypto(s string) bool {
	u := strings.ToUpper(s)
	if IsFRED(u) {
		return false
	}
	if strings.Contains(u, "-") {
		return strings.HasSuffix(u, "-USD") || strings.HasSuffix(u, "-USDT")
	}
	if strings.HasSuffix(u, "USDT") || strings.HasSuffix(u, "BUSD") {
		return true
	}
	return cryptoBases[u]
}

// IsStock reports whether the symbol should route to the stock provider
// (Yahoo): not FRED, not crypto, and shaped like a ticker (1–10 letters,
// optionally with '.' or '-' as in BRK.B / BRK-B).
func IsStock(s string) bool {
	if IsFRED(s) || IsCrypto(s) {
		return false
	}
	u := strings.ToUpper(s)
	for _, c := range u {
		if !((c >= 'A' && c <= 'Z') || c == '.' || c == '-') {
			return false
		}
	}
	return len(u) >= 1 && len(u) <= 10
}

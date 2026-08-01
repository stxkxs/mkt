// Package symbol is the single source of truth for classifying and
// normalizing a market symbol as crypto, a FRED economic series, or a
// stock. Providers used to each carry their own (divergent) copy of "what
// is a crypto symbol", which only stayed correct because of provider
// ordering in the hub chain. Centralizing it here removes that hidden
// coupling: coinbase.Supports, yahoo.Supports, fred.Supports and the
// earnings ticker filter all delegate to these functions, so the lists can
// never drift apart again.
//
// Classification and canonicalization share one implementation
// (classify), which is what guarantees a symbol can never be claimed by
// two classifiers and that Canonical always agrees with IsCrypto/IsFRED/
// IsStock about what a symbol is.
package symbol

import (
	"slices"
	"strings"
)

// FREDPrefix marks macro/economic series handled by the FRED provider
// (history only), e.g. "FRED:DGS10". This package owns the prefix; the
// FRED provider and anything else that needs to spot a series symbol
// references this constant rather than spelling the literal again.
const FREDPrefix = "FRED:"

// cryptoBases are bare tickers we treat as crypto and route to Coinbase as
// <base>-USD even without an explicit quote-currency suffix. This is the
// canonical set; add new coins here and every provider picks them up.
var cryptoBases = map[string]bool{
	"BTC": true, "ETH": true, "SOL": true, "XRP": true,
	"ADA": true, "DOGE": true, "AVAX": true, "DOT": true,
	"POL": true, "LINK": true, "UNI": true, "ATOM": true,
	"LTC": true, "NEAR": true, "FIL": true, "APT": true,
	"ARB": true, "OP": true, "SUI": true, "SEI": true,
	"BNB": true, "PEPE": true, "SHIB": true, "WIF": true,
}

// cryptoRenames maps retired base tickers onto their current one. Polygon
// migrated MATIC to POL and exchanges retired the MATIC-USD product, so a
// watchlist or portfolio holding "MATIC" would silently stop pricing. We
// rewrite the old ticker instead of dropping it: Canonical("MATIC") is
// "POL-USD", so existing config keeps working without the user editing it.
var cryptoRenames = map[string]string{
	"MATIC": "POL",
}

// pairQuotes are the quote currencies accepted in the explicit
// "<base>-<quote>" pair form.
var pairQuotes = []string{"USD", "USDT", "BUSD"}

// bareQuotes are the quote currencies unambiguous enough to accept glued
// straight onto an unknown base (BTCUSDT, PEPEBUSD). USD is deliberately
// absent: too many equity and FX symbols end in those letters, so a bare
// *USD pair is only crypto behind a base we already know.
var bareQuotes = []string{"USDT", "BUSD"}

// maxTickerLen bounds the ticker-shaped branch. Real Yahoo symbols are
// well under this (DX-Y.NYB and EURUSD=X are the longest mkt ships);
// anything longer is a typo or free text, not a symbol.
const maxTickerLen = 10

// class enumerates the mutually exclusive routing buckets a symbol can
// fall into. classUnknown means "shaped like nothing we can quote".
type class int

const (
	classUnknown class = iota
	classFRED
	classCrypto
	classStock
)

// classify uppercases and trims s, decides which provider bucket owns it,
// and returns the canonical spelling for that bucket. Every exported
// function in this package is a thin wrapper over it.
func classify(s string) (class, string) {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "" {
		return classUnknown, ""
	}
	if strings.HasPrefix(u, FREDPrefix) {
		return classFRED, FREDPrefix + strings.TrimSpace(strings.TrimPrefix(u, FREDPrefix))
	}
	if base, ok := cryptoBase(u); ok {
		return classCrypto, base + "-USD"
	}
	if isTickerShape(u) {
		return classStock, u
	}
	return classUnknown, u
}

// cryptoBase reports the crypto base asset an already-uppercased symbol
// denotes, after applying any ticker rename. It accepts the Coinbase pair
// form (BTC-USD, ETH-USDT), the Binance bare-pair form (BTCUSDT,
// PEPEBUSD), a bare known base (BTC) and a bare known base against USD
// (BTCUSD).
func cryptoBase(u string) (string, bool) {
	// Explicit pair: the quote currency after the dash decides. This is
	// what keeps DX-Y.NYB and BRK-B out of crypto — their "quote" is not
	// a currency we recognize.
	if i := strings.IndexByte(u, '-'); i > 0 {
		base, quote := u[:i], u[i+1:]
		if slices.Contains(pairQuotes, quote) && isBaseShape(base) {
			return renameBase(base), true
		}
		return "", false
	}

	// Binance-style bare pair. USDT/BUSD are unambiguous crypto quote
	// currencies, so any plausible base in front of one is crypto even if
	// we have never heard of the coin.
	for _, q := range bareQuotes {
		if base := strings.TrimSuffix(u, q); base != u && isBaseShape(base) {
			return renameBase(base), true
		}
	}

	// A bare *USD pair is only crypto when we know the base: equity and
	// FX symbols end in USD-ish letters often enough that claiming all of
	// them would steal symbols from Yahoo.
	if base := strings.TrimSuffix(u, "USD"); base != u && isKnownBase(base) {
		return renameBase(base), true
	}

	if isKnownBase(u) {
		return renameBase(u), true
	}
	return "", false
}

// isBaseShape reports whether b can be the base asset of a crypto pair:
// short and alphanumeric, so punctuated tickers stay with the stock
// classifier.
func isBaseShape(b string) bool {
	if b == "" || len(b) > maxTickerLen {
		return false
	}
	for _, c := range b {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// isKnownBase reports whether b is a coin we route to Coinbase, including
// tickers we only still recognize because of a rename.
func isKnownBase(b string) bool {
	if cryptoBases[b] {
		return true
	}
	_, renamed := cryptoRenames[b]
	return renamed
}

// renameBase applies a ticker migration (MATIC -> POL), returning b
// unchanged when there is nothing to migrate.
func renameBase(b string) string {
	if to, ok := cryptoRenames[b]; ok {
		return to
	}
	return b
}

// isTickerShape reports whether an uppercased symbol looks like something
// the stock provider can quote. Yahoo's universe is wider than plain
// equity tickers and mkt itself polls the wider set on the macro tab, so
// this accepts indices (^GSPC, ^VIX), futures (GC=F, CL=F), FX pairs
// (EURUSD=X), exchange-qualified tickers (DX-Y.NYB) and share classes
// (BRK.B, BRK-B) alongside ordinary tickers.
func isTickerShape(u string) bool {
	body := strings.TrimPrefix(u, "^")
	if body == "" || len(body) > maxTickerLen {
		return false
	}
	letters := 0
	for _, c := range body {
		switch {
		case c >= 'A' && c <= 'Z':
			letters++
		case c >= '0' && c <= '9', c == '.', c == '-', c == '=':
		default:
			return false
		}
	}
	return letters > 0
}

// Canonical normalizes a user-supplied symbol to the exact form providers
// emit as Quote.Symbol, so a symbol typed by hand keys the same cache
// entry as the quotes streaming in for it:
//
//	"btc", "BTCUSDT", "btc-usd" -> "BTC-USD"
//	"aapl"                      -> "AAPL"
//	"fred:dgs10"                -> "FRED:DGS10"
//
// Anything we do not recognize is returned trimmed and uppercased and
// otherwise untouched (^GSPC, GC=F, EURUSD=X, BRK.B). Canonical is
// idempotent: Canonical(Canonical(s)) == Canonical(s) for every input.
func Canonical(s string) string {
	_, c := classify(s)
	return c
}

// IsFRED reports whether the symbol addresses a FRED economic series.
func IsFRED(s string) bool {
	c, _ := classify(s)
	return c == classFRED
}

// IsCrypto reports whether the symbol denotes a cryptocurrency — whether
// written as a Coinbase pair (BTC-USD, ETH-USDT), a Binance-style bare
// pair (BTCUSDT), or a known bare base (BTC). FRED series are never
// crypto.
func IsCrypto(s string) bool {
	c, _ := classify(s)
	return c == classCrypto
}

// IsStock reports whether the symbol should route to the stock provider
// (Yahoo): not FRED, not crypto, and shaped like something Yahoo quotes —
// a ticker (AAPL, BRK.B), an index (^GSPC), a future (GC=F), or an FX
// pair (EURUSD=X).
func IsStock(s string) bool {
	c, _ := classify(s)
	return c == classStock
}

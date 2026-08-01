package symbol

import "testing"

// defaultWatchlist mirrors config.DefaultWatchlist. It is copied rather
// than imported so this package — which everything else classifies
// symbols with — keeps zero dependencies on the config layer.
var defaultWatchlist = []string{
	"BTC-USD", "ETH-USD", "SOL-USD", "XRP-USD", "ADA-USD", "DOGE-USD", "AVAX-USD", "LINK-USD", "DOT-USD",
	"NEAR-USD", "SUI-USD", "ARB-USD", "OP-USD", "PEPE-USD", "AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA",
	"META", "AMD", "NFLX", "COIN", "LMT", "RTX", "NOC", "GD", "LHX", "BA", "HII", "KTOS", "LDOS", "XOM", "CVX", "OXY",
	"HAL", "DVN", "COP", "SLB", "EOG", "FANG", "PSX", "MPC", "VLO", "FRO", "STNG", "INSW", "CCJ", "UEC", "DNN", "LEU",
	"NNE", "SMR", "PANW", "CRWD", "FTNT", "ZS", "NET", "S", "GLD", "NEM", "GOLD", "AEM", "WPM", "RGLD", "MP", "VALE",
	"ZIM", "AVGO", "ARM", "TSM", "MRVL", "SMCI", "VRT", "ANET", "DELL", "CRM", "PLTR", "SNOW", "O", "AMT", "DHI",
	"LEN", "SCHD", "SQ", "SHOP", "SOFI", "ABBV", "KRE", "CAT", "DE", "URI", "VMC", "MLM", "NUE", "STLD", "PWR", "INTC",
	"AMAT", "LRCX", "EATON", "FAST", "XYL", "UNP", "CSX", "AAON", "ENPH", "FSLR", "CEG", "VST", "NEE", "ALB", "RIVN",
	"LLY", "JNJ", "MRK", "PFE", "NVO", "AMGN", "HIMS", "TDOC", "ISRG", "MDT", "ABT", "SYK", "UNH", "HUM", "WELL",
	"VRTX", "REGN", "DXCM", "PODD", "CRL", "WST", "PTON", "ROK", "TER", "CGNX", "GXO", "UBER", "ADI", "ON", "AVAV",
	"FCX", "SCCO", "TECK", "SQM", "BHP", "RIO", "MOS", "NTR", "ADM", "MARA", "CLSK", "RIOT", "WULF", "MSTR", "HOOD",
	"RKLB", "ASTS", "GSAT", "IRDM",
}

// macroSymbols mirrors yahoo.MacroSymbols — the symbols mkt itself polls
// on the macro tab. They must all classify as stocks or the macro tab
// silently loses its data.
var macroSymbols = []string{
	"^TNX", "^IRX", "^VIX", "DX-Y.NYB", "GC=F", "CL=F", "^GSPC", "BTC-USD",
}

func TestCanonical(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// Crypto: every spelling collapses to the Coinbase product ID.
		{"BTC-USD", "BTC-USD"},
		{"btc-usd", "BTC-USD"},
		{"  btc-usd  ", "BTC-USD"},
		{"btc", "BTC-USD"},
		{"BTC", "BTC-USD"},
		{"BtC", "BTC-USD"},
		{"BTCUSD", "BTC-USD"},
		{"BTCUSDT", "BTC-USD"},
		{"btcusdt", "BTC-USD"},
		{"BTC-USDT", "BTC-USD"},
		{"eth-usdt", "ETH-USD"},
		{"ETHBUSD", "ETH-USD"},
		{"pepe", "PEPE-USD"},
		{"WIF-USD", "WIF-USD"},
		// Unknown coin against an unambiguous crypto quote is still crypto.
		{"tiausdt", "TIA-USD"},

		// MATIC migrated to POL; the old ticker keeps pricing.
		{"MATIC", "POL-USD"},
		{"matic", "POL-USD"},
		{"MATIC-USD", "POL-USD"},
		{"MATICUSDT", "POL-USD"},
		{"POL", "POL-USD"},
		{"POL-USD", "POL-USD"},

		// FRED series.
		{"FRED:DGS10", "FRED:DGS10"},
		{"fred:dgs10", "FRED:DGS10"},
		{"  fred:unrate ", "FRED:UNRATE"},
		{"FRED: dgs10", "FRED:DGS10"},

		// Stocks and everything else: uppercased, otherwise untouched.
		{"aapl", "AAPL"},
		{"AAPL", "AAPL"},
		{" msft ", "MSFT"},
		{"brk.b", "BRK.B"},
		{"BRK-B", "BRK-B"},
		{"^gspc", "^GSPC"},
		{"^VIX", "^VIX"},
		{"gc=f", "GC=F"},
		{"CL=F", "CL=F"},
		{"dx-y.nyb", "DX-Y.NYB"},
		{"eurusd=x", "EURUSD=X"},

		// Degenerate input.
		{"", ""},
		{"   ", ""},
		{"toolongticker", "TOOLONGTICKER"},
	}
	for _, tc := range tests {
		if got := Canonical(tc.in); got != tc.want {
			t.Errorf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCanonicalIdempotent(t *testing.T) {
	var inputs []string
	inputs = append(inputs, defaultWatchlist...)
	inputs = append(inputs, macroSymbols...)
	inputs = append(inputs,
		"", "   ", "btc", "BTCUSDT", "btc-usd", "BTC-USDT", "ethbusd", "matic",
		"MATIC-USD", "MATICUSDT", "pol", "aapl", "brk.b", "BRK-B", "^gspc",
		"gc=f", "eurusd=x", "dx-y.nyb", "fred:dgs10", "FRED:DGS10", "FRED:",
		"toolongticker", "USD", "USDT", "BUSD", "-USD", "GOLD", "O", "S",
	)
	for _, in := range inputs {
		once := Canonical(in)
		twice := Canonical(once)
		if once != twice {
			t.Errorf("Canonical not idempotent for %q: %q -> %q", in, once, twice)
		}
	}
}

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
		{"MATIC", true, false, false}, // renamed base still routes to crypto
		{"POL", true, false, false},
		// FRED series
		{"FRED:DGS10", false, true, false},
		{"fred:unrate", false, true, false},
		// Stocks
		{"AAPL", false, false, true},
		{"BRK.B", false, false, true},
		{"BRK-B", false, false, true},
		// Non-crypto pair-ish that isn't a known base: still a stock shape
		{"MSFT", false, false, true},
		// Indices, futures, FX and exchange-qualified tickers mkt polls
		// itself — these must route to Yahoo, not fall through unclaimed.
		{"^GSPC", false, false, true},
		{"^TNX", false, false, true},
		{"^VIX", false, false, true},
		{"^IRX", false, false, true},
		{"GC=F", false, false, true},
		{"CL=F", false, false, true},
		{"DX-Y.NYB", false, false, true},
		{"EURUSD=X", false, false, true},
		{"eurusd=x", false, false, true},
		// A bare quote currency is nobody's coin.
		{"USD", false, false, true},
		{"USDT", false, false, true},
		// Too long / empty / whitespace are not stocks
		{"TOOLONGTICKER", false, false, false},
		{"", false, false, false},
		{"   ", false, false, false},
		{"^", false, false, false},
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
	var syms []string
	syms = append(syms, defaultWatchlist...)
	syms = append(syms, macroSymbols...)
	syms = append(syms,
		"BTC-USD", "AAPL", "FRED:DGS10", "fred:dgs10", "BTCUSDT", "BRK.B",
		"LINK", "MATIC", "POL-USD", "^GSPC", "GC=F", "EURUSD=X", "DX-Y.NYB",
		"USD", "USDT", "", "TOOLONGTICKER",
	)
	for _, s := range syms {
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

func TestDefaultsAreCanonicalAndRoutable(t *testing.T) {
	// Everything mkt ships must already be in canonical form (otherwise
	// the shipped config would key different cache entries than the live
	// quotes) and must be claimed by a provider.
	for _, s := range append(append([]string{}, defaultWatchlist...), macroSymbols...) {
		if got := Canonical(s); got != s {
			t.Errorf("default symbol %q is not canonical: Canonical = %q", s, got)
		}
		if !IsCrypto(s) && !IsStock(s) && !IsFRED(s) {
			t.Errorf("default symbol %q is not claimed by any provider", s)
		}
	}
}

func TestFREDPrefixIsSourceOfTruth(t *testing.T) {
	if FREDPrefix != "FRED:" {
		t.Fatalf("FREDPrefix = %q, want %q", FREDPrefix, "FRED:")
	}
	// The bare prefix still routes to FRED — the provider is the one that
	// reports an empty series id, not the classifier.
	if !IsFRED(FREDPrefix) {
		t.Errorf("IsFRED(%q) = false, want true", FREDPrefix)
	}
	if Canonical("fred:") != FREDPrefix {
		t.Errorf("Canonical(%q) = %q, want %q", "fred:", Canonical("fred:"), FREDPrefix)
	}
}

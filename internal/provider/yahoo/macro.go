package yahoo

import (
	"context"
	"sync"

	"github.com/stxkxs/mkt/internal/provider"
)

// MacroSymbol defines a macro indicator with its Yahoo symbol and label.
type MacroSymbol struct {
	Symbol string
	Label  string
}

// MacroSymbols is the fixed set of macro indicators.
var MacroSymbols = []MacroSymbol{
	{Symbol: "^TNX", Label: "10Y Treasury"},
	{Symbol: "^IRX", Label: "13W T-Bill"},
	{Symbol: "^VIX", Label: "VIX"},
	{Symbol: "DX-Y.NYB", Label: "Dollar (DXY)"},
	{Symbol: "GC=F", Label: "Gold"},
	{Symbol: "CL=F", Label: "WTI Crude"},
	{Symbol: "^GSPC", Label: "S&P 500"},
	{Symbol: "BTC-USD", Label: "Bitcoin"},
}

// FetchMacroQuotes fetches quotes for all macro symbols using the chart API.
func (p *Provider) FetchMacroQuotes(ctx context.Context) []provider.Quote {
	if err := p.initSession(ctx); err != nil {
		_ = err
	}

	var mu sync.Mutex
	var quotes []provider.Quote

	var wg sync.WaitGroup
	for _, ms := range MacroSymbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			q, err := p.fetchQuoteViaChart(ctx, sym)
			if err != nil {
				return
			}
			mu.Lock()
			quotes = append(quotes, q)
			mu.Unlock()
		}(ms.Symbol)
	}
	wg.Wait()

	return quotes
}

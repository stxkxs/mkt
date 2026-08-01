package yahoo

import (
	"context"
	"fmt"
	"net/url"
)

// SymbolSummary holds fundamental data for a symbol.
type SymbolSummary struct {
	Symbol     string
	MarketCap  float64
	PE         float64
	ForwardPE  float64
	EPS        float64
	DivYield   float64
	Week52High float64
	Week52Low  float64
	Sector     string
	Industry   string
}

// FetchSummary retrieves fundamental data using Yahoo's quoteSummary endpoint.
func (p *Provider) FetchSummary(ctx context.Context, symbol string) (SymbolSummary, error) {
	if err := p.initSession(ctx); err != nil {
		_ = err // non-fatal
	}

	endpoint := fmt.Sprintf("%s/v10/finance/quoteSummary/%s?modules=summaryDetail,defaultKeyStatistics,summaryProfile",
		baseURL, url.PathEscape(symbol))
	endpoint += p.crumbParam("&")

	var result quoteSummaryResponse
	if err := p.getJSON(ctx, endpoint, yahooHeaders, &result); err != nil {
		p.resetCrumbOnAuthError(err)
		return SymbolSummary{}, fmt.Errorf("yahoo summary: %w", err)
	}

	if result.QuoteSummary.Error != nil {
		return SymbolSummary{}, fmt.Errorf("yahoo error: %s", result.QuoteSummary.Error.Description)
	}

	if len(result.QuoteSummary.Result) == 0 {
		return SymbolSummary{}, fmt.Errorf("no summary data for %s", symbol)
	}

	r := result.QuoteSummary.Result[0]
	s := SymbolSummary{
		Symbol:     symbol,
		MarketCap:  r.SummaryDetail.MarketCap.Raw,
		PE:         r.SummaryDetail.TrailingPE.Raw,
		ForwardPE:  r.SummaryDetail.ForwardPE.Raw,
		EPS:        r.DefaultKeyStatistics.TrailingEps.Raw,
		DivYield:   r.SummaryDetail.DividendYield.Raw * 100,
		Week52High: r.SummaryDetail.FiftyTwoWeekHigh.Raw,
		Week52Low:  r.SummaryDetail.FiftyTwoWeekLow.Raw,
	}
	if r.SummaryProfile != nil {
		s.Sector = r.SummaryProfile.Sector
		s.Industry = r.SummaryProfile.Industry
	}
	return s, nil
}

// Response types for quoteSummary endpoint

type quoteSummaryResponse struct {
	QuoteSummary struct {
		Result []quoteSummaryResult `json:"result"`
		Error  *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteSummary"`
}

type quoteSummaryResult struct {
	SummaryDetail        summaryDetailModule        `json:"summaryDetail"`
	DefaultKeyStatistics defaultKeyStatisticsModule `json:"defaultKeyStatistics"`
	SummaryProfile       *summaryProfileModule      `json:"summaryProfile"`
}

type yahooRawFmt struct {
	Raw float64 `json:"raw"`
	Fmt string  `json:"fmt"`
}

type summaryDetailModule struct {
	MarketCap        yahooRawFmt `json:"marketCap"`
	TrailingPE       yahooRawFmt `json:"trailingPE"`
	ForwardPE        yahooRawFmt `json:"forwardPE"`
	DividendYield    yahooRawFmt `json:"dividendYield"`
	FiftyTwoWeekHigh yahooRawFmt `json:"fiftyTwoWeekHigh"`
	FiftyTwoWeekLow  yahooRawFmt `json:"fiftyTwoWeekLow"`
}

type defaultKeyStatisticsModule struct {
	TrailingEps yahooRawFmt `json:"trailingEps"`
}

type summaryProfileModule struct {
	Sector   string `json:"sector"`
	Industry string `json:"industry"`
}

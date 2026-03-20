package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	url := fmt.Sprintf("%s/v10/finance/quoteSummary/%s?modules=summaryDetail,defaultKeyStatistics,summaryProfile",
		baseURL, symbol)
	if p.crumb != "" {
		url += "&crumb=" + p.crumb
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SymbolSummary{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return SymbolSummary{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return SymbolSummary{}, err
	}

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		p.mu.Lock()
		p.crumb = ""
		p.mu.Unlock()
		return SymbolSummary{}, fmt.Errorf("yahoo auth error %d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return SymbolSummary{}, fmt.Errorf("yahoo summary error %d", resp.StatusCode)
	}

	var result quoteSummaryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return SymbolSummary{}, fmt.Errorf("parse summary: %w", err)
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

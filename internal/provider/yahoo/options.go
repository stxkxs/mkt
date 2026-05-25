package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// OptionsBaseURL is the v7 options endpoint base; exported so tests can
// substitute an httptest server.
var OptionsBaseURL = "https://query1.finance.yahoo.com/v7/finance/options"

// OptionsChain is the parsed options snapshot for one expiration.
type OptionsChain struct {
	Symbol     string
	Expiration time.Time
	Calls      []Option
	Puts       []Option
}

// Option is one strike on the call or put side.
type Option struct {
	Strike       float64
	Last         float64
	Bid          float64
	Ask          float64
	Volume       int
	OpenInterest int
	IV           float64 // implied volatility, fraction
}

type optionsResp struct {
	OptionChain struct {
		Result []struct {
			ExpirationDates []int64 `json:"expirationDates"`
			Options         []struct {
				Expiration int64       `json:"expirationDate"`
				Calls      []apiOption `json:"calls"`
				Puts       []apiOption `json:"puts"`
			} `json:"options"`
		} `json:"result"`
	} `json:"optionChain"`
}

type apiOption struct {
	ContractSymbol    string  `json:"contractSymbol"`
	Strike            float64 `json:"strike"`
	LastPrice         float64 `json:"lastPrice"`
	Bid               float64 `json:"bid"`
	Ask               float64 `json:"ask"`
	Volume            int     `json:"volume"`
	OpenInterest      int     `json:"openInterest"`
	ImpliedVolatility float64 `json:"impliedVolatility"`
}

// FetchOptionsChain returns the nearest expiration's calls and puts for
// the given symbol. Reuses the existing Yahoo session for the crumb
// when available.
func (p *Provider) FetchOptionsChain(ctx context.Context, symbol string) (OptionsChain, error) {
	if err := p.initSession(ctx); err != nil {
		// Non-fatal — some endpoints work without crumb
		_ = err
	}
	endpoint := fmt.Sprintf("%s/%s", OptionsBaseURL, url.PathEscape(symbol))
	if p.crumb != "" {
		endpoint += "?crumb=" + url.QueryEscape(p.crumb)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OptionsChain{}, fmt.Errorf("options: build: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return OptionsChain{}, fmt.Errorf("options %s: %w", symbol, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OptionsChain{}, fmt.Errorf("options %s: status %d", symbol, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OptionsChain{}, fmt.Errorf("options %s: read: %w", symbol, err)
	}
	var raw optionsResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return OptionsChain{}, fmt.Errorf("options %s: decode: %w", symbol, err)
	}
	if len(raw.OptionChain.Result) == 0 || len(raw.OptionChain.Result[0].Options) == 0 {
		return OptionsChain{Symbol: symbol}, nil
	}
	o := raw.OptionChain.Result[0].Options[0]
	out := OptionsChain{
		Symbol:     symbol,
		Expiration: time.Unix(o.Expiration, 0).UTC(),
		Calls:      convert(o.Calls),
		Puts:       convert(o.Puts),
	}
	return out, nil
}

func convert(in []apiOption) []Option {
	out := make([]Option, len(in))
	for i, o := range in {
		out[i] = Option{
			Strike:       o.Strike,
			Last:         o.LastPrice,
			Bid:          o.Bid,
			Ask:          o.Ask,
			Volume:       o.Volume,
			OpenInterest: o.OpenInterest,
			IV:           o.ImpliedVolatility,
		}
	}
	return out
}

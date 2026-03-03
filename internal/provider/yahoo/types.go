package yahoo

// chartResponse is the Yahoo Finance v8 chart API response.
type chartResponse struct {
	Chart struct {
		Result []chartResult `json:"result"`
		Error  *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

type chartResult struct {
	Meta       chartMeta  `json:"meta"`
	Timestamp  []int64    `json:"timestamp"`
	Indicators indicators `json:"indicators"`
}

type chartMeta struct {
	Currency             string  `json:"currency"`
	Symbol               string  `json:"symbol"`
	RegularMarketPrice   float64 `json:"regularMarketPrice"`
	ChartPreviousClose   float64 `json:"chartPreviousClose"`
	PreviousClose        float64 `json:"previousClose"`
	RegularMarketDayHigh float64 `json:"regularMarketDayHigh"`
	RegularMarketDayLow  float64 `json:"regularMarketDayLow"`
	RegularMarketVolume  int64   `json:"regularMarketVolume"`
}

type indicators struct {
	Quote []quoteIndicator `json:"quote"`
}

type quoteIndicator struct {
	Open   []*float64 `json:"open"`
	High   []*float64 `json:"high"`
	Low    []*float64 `json:"low"`
	Close  []*float64 `json:"close"`
	Volume []*float64 `json:"volume"`
}

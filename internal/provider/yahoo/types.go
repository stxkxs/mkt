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

// batchQuoteResponse is the Yahoo Finance v7 batch quote API response.
type batchQuoteResponse struct {
	QuoteResponse struct {
		Result []batchQuoteResult `json:"result"`
		Error  *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"quoteResponse"`
}

type batchQuoteResult struct {
	Symbol                     string  `json:"symbol"`
	RegularMarketPrice         float64 `json:"regularMarketPrice"`
	RegularMarketChange        float64 `json:"regularMarketChange"`
	RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
	RegularMarketVolume        float64 `json:"regularMarketVolume"`
	RegularMarketDayHigh       float64 `json:"regularMarketDayHigh"`
	RegularMarketDayLow        float64 `json:"regularMarketDayLow"`
	RegularMarketPreviousClose float64 `json:"regularMarketPreviousClose"`
}

package binance

// miniTickerEvent is the Binance WebSocket 24hr Mini Ticker payload.
// Stream: <symbol>@miniTicker
type miniTickerEvent struct {
	EventType   string `json:"e"` // "24hrMiniTicker"
	EventTime   int64  `json:"E"`
	Symbol      string `json:"s"`
	Close       string `json:"c"` // current close price
	Open        string `json:"o"` // open price
	High        string `json:"h"` // high price
	Low         string `json:"l"` // low price
	Volume      string `json:"v"` // total traded base asset volume
	QuoteVolume string `json:"q"` // total traded quote asset volume
}

// klineResponse is the Binance REST API /api/v3/klines response element.
// Each element is an array: [openTime, open, high, low, close, volume, closeTime, ...]
type klineResponse = []any

// combinedStreamMsg wraps Binance combined stream messages.
type combinedStreamMsg struct {
	Stream string          `json:"stream"`
	Data   miniTickerEvent `json:"data"`
}

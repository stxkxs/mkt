package coinbase

// subscribeMsg is sent to Coinbase to subscribe to channels.
type subscribeMsg struct {
	Type       string   `json:"type"`
	ProductIDs []string `json:"product_ids"`
	Channel    string   `json:"channel"`
}

// wsMessage is the top-level Coinbase WebSocket message.
type wsMessage struct {
	Channel   string    `json:"channel"`
	ClientID  string    `json:"client_id"`
	Timestamp string    `json:"timestamp"`
	Events    []wsEvent `json:"events"`
}

// wsEvent contains ticker data.
type wsEvent struct {
	Type    string     `json:"type"` // "snapshot" or "update"
	Tickers []wsTicker `json:"tickers"`
}

// wsTicker is a single ticker update from Coinbase.
type wsTicker struct {
	Type               string `json:"type"` // "SPOT"
	ProductID          string `json:"product_id"`
	Price              string `json:"price"`
	Volume24H          string `json:"volume_24_h"`
	Low24H             string `json:"low_24_h"`
	High24H            string `json:"high_24_h"`
	Low52W             string `json:"low_52_w"`
	High52W            string `json:"high_52_w"`
	PricePercentChg24H string `json:"price_percent_chg_24_h"`
	BestBid            string `json:"best_bid"`
	BestBidQty         string `json:"best_bid_quantity"`
	BestAsk            string `json:"best_ask"`
	BestAskQty         string `json:"best_ask_quantity"`
}

package provider

import "time"

// AssetType distinguishes crypto from stocks.
type AssetType int

const (
	AssetCrypto AssetType = iota
	AssetStock
)

func (a AssetType) String() string {
	switch a {
	case AssetCrypto:
		return "crypto"
	case AssetStock:
		return "stock"
	default:
		return "unknown"
	}
}

// Quote represents a real-time price update.
type Quote struct {
	Symbol    string
	Price     float64
	Change    float64   // absolute change
	ChangePct float64   // percentage change
	Volume    float64
	High24h   float64
	Low24h    float64
	Bid       float64
	Ask       float64
	Asset     AssetType
	Provider  string
	Timestamp time.Time
}

// OHLCV represents a single candlestick.
type OHLCV struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Interval defines a chart time interval.
type Interval string

const (
	Interval1m  Interval = "1m"
	Interval5m  Interval = "5m"
	Interval15m Interval = "15m"
	Interval1h  Interval = "1h"
	Interval4h  Interval = "4h"
	Interval1d  Interval = "1d"
	Interval1w  Interval = "1w"
)

// HistoryParams configures a history request.
type HistoryParams struct {
	Symbol   string
	Interval Interval
	Limit    int
	Start    time.Time
	End      time.Time
}

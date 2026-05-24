// Package recording captures live quote streams to NDJSON and replays them
// as a QuoteProvider. The wire format is one JSON object per line with a
// schema version field so future format changes remain readable.
package recording

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// SchemaVersion is the on-disk wire format version. Bump when the wire
// type changes incompatibly; the replay decoder rejects newer versions.
const SchemaVersion = 1

// wireQuote is the JSON representation of a provider.Quote.
type wireQuote struct {
	V         int     `json:"v"`
	Symbol    string  `json:"symbol"`
	Price     float64 `json:"price"`
	Change    float64 `json:"change,omitempty"`
	ChangePct float64 `json:"change_pct,omitempty"`
	Volume    float64 `json:"volume,omitempty"`
	High24h   float64 `json:"high_24h,omitempty"`
	Low24h    float64 `json:"low_24h,omitempty"`
	Bid       float64 `json:"bid,omitempty"`
	Ask       float64 `json:"ask,omitempty"`
	Asset     int     `json:"asset"`
	Provider  string  `json:"provider,omitempty"`
	Timestamp int64   `json:"ts"` // unix nanoseconds
}

func encode(q provider.Quote) ([]byte, error) {
	w := wireQuote{
		V:         SchemaVersion,
		Symbol:    q.Symbol,
		Price:     q.Price,
		Change:    q.Change,
		ChangePct: q.ChangePct,
		Volume:    q.Volume,
		High24h:   q.High24h,
		Low24h:    q.Low24h,
		Bid:       q.Bid,
		Ask:       q.Ask,
		Asset:     int(q.Asset),
		Provider:  q.Provider,
		Timestamp: q.Timestamp.UnixNano(),
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func decode(line []byte) (provider.Quote, error) {
	var w wireQuote
	if err := json.Unmarshal(line, &w); err != nil {
		return provider.Quote{}, err
	}
	if w.V == 0 {
		return provider.Quote{}, fmt.Errorf("missing version field")
	}
	if w.V > SchemaVersion {
		return provider.Quote{}, fmt.Errorf("unsupported version %d (max %d)", w.V, SchemaVersion)
	}
	return provider.Quote{
		Symbol:    w.Symbol,
		Price:     w.Price,
		Change:    w.Change,
		ChangePct: w.ChangePct,
		Volume:    w.Volume,
		High24h:   w.High24h,
		Low24h:    w.Low24h,
		Bid:       w.Bid,
		Ask:       w.Ask,
		Asset:     provider.AssetType(w.Asset),
		Provider:  w.Provider,
		Timestamp: time.Unix(0, w.Timestamp),
	}, nil
}

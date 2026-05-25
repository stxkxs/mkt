package coinbase

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

// OrderBookURL is the REST level-2 endpoint base; exported so tests can
// substitute an httptest server.
var OrderBookURL = "https://api.exchange.coinbase.com/products"

// Level is a single price-and-size order book entry.
type Level struct {
	Price float64
	Size  float64
}

// OrderBook is a parsed level-2 snapshot.
type OrderBook struct {
	ProductID string
	Sequence  int64
	Bids      []Level // descending by Price
	Asks      []Level // ascending by Price
}

type rawBook struct {
	Sequence int64       `json:"sequence"`
	Bids     [][3]string `json:"bids"` // [price, size, num_orders]
	Asks     [][3]string `json:"asks"`
}

// FetchOrderBook returns a level-2 snapshot for the given Coinbase
// product (e.g., BTC-USD). Bids are sorted descending, asks ascending.
func (p *Provider) FetchOrderBook(ctx context.Context, productID string) (OrderBook, error) {
	productID = toCoinbaseSymbol(productID)
	endpoint := fmt.Sprintf("%s/%s/book?level=2", OrderBookURL, url.PathEscape(productID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OrderBook{}, fmt.Errorf("orderbook: build: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return OrderBook{}, fmt.Errorf("orderbook %s: %w", productID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OrderBook{}, fmt.Errorf("orderbook %s: status %d", productID, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OrderBook{}, fmt.Errorf("orderbook %s: read: %w", productID, err)
	}
	var raw rawBook
	if err := json.Unmarshal(body, &raw); err != nil {
		return OrderBook{}, fmt.Errorf("orderbook %s: decode: %w", productID, err)
	}
	book := OrderBook{
		ProductID: productID,
		Sequence:  raw.Sequence,
		Bids:      parseLevels(raw.Bids),
		Asks:      parseLevels(raw.Asks),
	}
	sort.Slice(book.Bids, func(i, j int) bool { return book.Bids[i].Price > book.Bids[j].Price })
	sort.Slice(book.Asks, func(i, j int) bool { return book.Asks[i].Price < book.Asks[j].Price })
	return book, nil
}

func parseLevels(rows [][3]string) []Level {
	out := make([]Level, 0, len(rows))
	for _, r := range rows {
		price, err := strconv.ParseFloat(r[0], 64)
		if err != nil {
			continue
		}
		size, err := strconv.ParseFloat(r[1], 64)
		if err != nil {
			continue
		}
		out = append(out, Level{Price: price, Size: size})
	}
	return out
}

// OrderBookDepth returns the cumulative size across the top n levels on
// each side. Useful for a quick imbalance gauge.
func OrderBookDepth(book OrderBook, n int) (bidVol, askVol float64) {
	for i, l := range book.Bids {
		if i >= n {
			break
		}
		bidVol += l.Size
	}
	for i, l := range book.Asks {
		if i >= n {
			break
		}
		askVol += l.Size
	}
	return bidVol, askVol
}

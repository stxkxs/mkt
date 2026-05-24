// Package fred implements a HistoryProvider for FRED (St. Louis Fed)
// economic series. Symbols use the "FRED:<series_id>" prefix and data
// is fetched via the public CSV endpoint at fredgraph — no API key.
package fred

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

// Prefix marks symbols this provider handles.
const Prefix = "FRED:"

// DefaultBaseURL is the public CSV endpoint base.
const DefaultBaseURL = "https://fred.stlouisfed.org/graph/fredgraph.csv"

// Provider implements provider.HistoryProvider for FRED series.
type Provider struct {
	baseURL string
	client  *http.Client
}

// New returns a Provider using the default base URL and a 10s timeout.
func New() *Provider {
	return &Provider{
		baseURL: DefaultBaseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// SetBaseURL overrides the CSV endpoint (used by tests).
func (p *Provider) SetBaseURL(u string) { p.baseURL = u }

// Name implements provider.HistoryProvider.
func (p *Provider) Name() string { return "fred" }

// Supports implements provider.HistoryProvider. Returns true iff the
// symbol carries the FRED: prefix.
func (p *Provider) Supports(symbol string) bool {
	return strings.HasPrefix(symbol, Prefix)
}

// History fetches the series and returns OHLCV with open/high/low/close
// all equal to the observation value.
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	series := strings.TrimPrefix(params.Symbol, Prefix)
	if series == "" {
		return nil, fmt.Errorf("fred: empty series id")
	}
	url := fmt.Sprintf("%s?id=%s", p.baseURL, series)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fred: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fred: get %s: %w", series, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fred: %s returned status %d", series, resp.StatusCode)
	}
	rows, err := parseCSV(resp.Body)
	if err != nil {
		return nil, err
	}
	rows = filterByDate(rows, params.Start, params.End)
	if params.Limit > 0 && len(rows) > params.Limit {
		rows = rows[len(rows)-params.Limit:]
	}
	return rows, nil
}

func parseCSV(r io.Reader) ([]provider.OHLCV, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	var out []provider.OHLCV
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		date, val := parts[0], strings.TrimSpace(parts[1])
		if val == "" || val == "." {
			continue
		}
		t, err := time.Parse("2006-01-02", date)
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(val, 64)
		if err != nil {
			continue
		}
		out = append(out, provider.OHLCV{
			Time:  t,
			Open:  v,
			High:  v,
			Low:   v,
			Close: v,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("fred: parse: %w", err)
	}
	return out, nil
}

func filterByDate(rows []provider.OHLCV, start, end time.Time) []provider.OHLCV {
	if start.IsZero() && end.IsZero() {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if !start.IsZero() && r.Time.Before(start) {
			continue
		}
		if !end.IsZero() && r.Time.After(end) {
			continue
		}
		out = append(out, r)
	}
	return out
}

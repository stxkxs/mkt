// Package fred implements a HistoryProvider for FRED (St. Louis Fed)
// economic series. Symbols use the "FRED:<series_id>" prefix and data
// is fetched via the public CSV endpoint at fredgraph — no API key.
package fred

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/symbol"
)

// DefaultBaseURL is the public CSV endpoint base.
const DefaultBaseURL = "https://fred.stlouisfed.org/graph/fredgraph.csv"

// maxObservations bounds how many rows parseCSV will accumulate. httpx
// already caps the transferred bytes, but a 16 MiB CSV still decodes to
// hundreds of thousands of OHLCV structs; this is the second half of that
// cap, on the parsed side. The longest daily series FRED publishes is a
// few tens of thousands of rows, so this only ever trips on a response
// that is not really a FRED series.
const maxObservations = 200_000

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
// symbol carries the FRED: prefix, in any case — config files and CLI
// arguments are hand-written, and a lowercase "fred:dgs10" used to match
// no provider at all and silently route nowhere.
func (p *Provider) Supports(s string) bool {
	return symbol.IsFRED(s)
}

// History fetches the series and returns OHLCV with open/high/low/close
// all equal to the observation value.
func (p *Provider) History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error) {
	series, err := seriesID(params.Symbol)
	if err != nil {
		return nil, err
	}
	// Escape the id rather than interpolating it: it comes from user
	// config, and a raw '&' or '?' in it would otherwise let the caller
	// append query parameters to the fredgraph request.
	endpoint := fmt.Sprintf("%s?id=%s", p.baseURL, url.QueryEscape(series))
	body, err := httpx.Get(ctx, p.client, endpoint, map[string]string{"Accept": "text/csv"})
	if err != nil {
		return nil, fmt.Errorf("fred: get %s: %w", series, err)
	}
	rows, err := parseCSV(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	rows = filterByDate(rows, params.Start, params.End)
	if params.Limit > 0 && len(rows) > params.Limit {
		rows = rows[len(rows)-params.Limit:]
	}
	return rows, nil
}

// seriesID strips the FRED: prefix (case-insensitively) and returns the
// canonical uppercase series id. FRED series ids are uppercase
// alphanumerics, so anything else is rejected before it reaches the wire.
func seriesID(sym string) (string, error) {
	s := strings.TrimSpace(sym)
	if !symbol.IsFRED(s) {
		return "", fmt.Errorf("fred: %q is not a FRED symbol", sym)
	}
	series := strings.ToUpper(strings.TrimSpace(s[len(symbol.FREDPrefix):]))
	if series == "" {
		return "", fmt.Errorf("fred: empty series id")
	}
	for _, c := range series {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", fmt.Errorf("fred: invalid series id %q", series)
		}
	}
	return series, nil
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
		if len(out) >= maxObservations {
			return nil, fmt.Errorf("fred: parse: more than %d observations", maxObservations)
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

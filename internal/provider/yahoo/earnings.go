package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/provider/calendar"
)

// QuoteSummaryURL is the v10 quoteSummary endpoint base; exported so
// tests can swap in an httptest server.
var QuoteSummaryURL = "https://query2.finance.yahoo.com/v10/finance/quoteSummary"

type quoteSummaryResp struct {
	QuoteSummary struct {
		Result []struct {
			CalendarEvents struct {
				Earnings struct {
					EarningsDate []rawTimestamp `json:"earningsDate"`
				} `json:"earnings"`
			} `json:"calendarEvents"`
		} `json:"result"`
	} `json:"quoteSummary"`
}

type rawTimestamp struct {
	Raw int64  `json:"raw"`
	Fmt string `json:"fmt"`
}

// FetchEarnings returns upcoming earnings dates for the given tickers
// as calendar.Events. Looks up Yahoo's v10 quoteSummary calendarEvents
// module concurrently per ticker. Failures on individual tickers are
// skipped silently (logged via stderr would be too noisy for many
// symbols). Use it with the EarningsSource interface from D5.
func (p *Provider) FetchEarnings(ctx context.Context, tickers []string) ([]calendar.Event, error) {
	if len(tickers) == 0 {
		return nil, nil
	}
	// Best-effort session init; failure is non-fatal — some endpoints
	// work without a crumb.
	_ = p.initSession(ctx)

	var mu sync.Mutex
	var out []calendar.Event
	var wg sync.WaitGroup
	for _, t := range tickers {
		wg.Add(1)
		go func(ticker string) {
			defer wg.Done()
			evs := p.fetchEarningsOne(ctx, ticker)
			if len(evs) == 0 {
				return
			}
			mu.Lock()
			out = append(out, evs...)
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

func (p *Provider) fetchEarningsOne(ctx context.Context, ticker string) []calendar.Event {
	url := fmt.Sprintf("%s/%s?modules=calendarEvents", QuoteSummaryURL, ticker)
	if p.crumb != "" {
		url += "&crumb=" + p.crumb
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var raw quoteSummaryResp
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	if len(raw.QuoteSummary.Result) == 0 {
		return nil
	}
	dates := raw.QuoteSummary.Result[0].CalendarEvents.Earnings.EarningsDate
	if len(dates) == 0 {
		return nil
	}
	var out []calendar.Event
	for _, d := range dates {
		if d.Raw == 0 {
			continue
		}
		out = append(out, calendar.Event{
			Time:       time.Unix(d.Raw, 0).UTC(),
			Title:      ticker + " earnings",
			Type:       calendar.Earnings,
			Importance: 2,
			Symbol:     ticker,
		})
	}
	return out
}

// EarningsAdapter wraps a Provider as a calendar.EarningsSource so it
// can be plugged into the calendar UI without leaking yahoo types.
type EarningsAdapter struct {
	P *Provider
}

// Fetch implements calendar.EarningsSource.
func (a EarningsAdapter) Fetch(ctx context.Context, tickers []string) ([]calendar.Event, error) {
	return a.P.FetchEarnings(ctx, tickers)
}

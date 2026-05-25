package news

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// DefaultEDGARBase is the SEC EDGAR Atom-feed endpoint base.
const DefaultEDGARBase = "https://www.sec.gov/cgi-bin/browse-edgar"

// edgarUserAgent satisfies SEC's required descriptive User-Agent.
const edgarUserAgent = "mkt (https://github.com/stxkxs/mkt)"

// EDGARBaseURL is exported so tests can swap in an httptest server.
var EDGARBaseURL = DefaultEDGARBase

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title    string     `xml:"title"`
	Updated  string     `xml:"updated"`
	Links    []atomLink `xml:"link"`
	Category struct {
		Term string `xml:"term,attr"`
	} `xml:"category"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// FetchEDGAR pulls recent SEC filings for each ticker concurrently and
// returns them sorted by PubTime descending, capped to limit. limit <= 0
// is treated as unlimited. Tickers are queried in parallel.
func FetchEDGAR(ctx context.Context, tickers []string, limit int) []Headline {
	if len(tickers) == 0 {
		return nil
	}
	var mu sync.Mutex
	var all []Headline
	var wg sync.WaitGroup
	for _, t := range tickers {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		wg.Add(1)
		go func(ticker string) {
			defer wg.Done()
			items := fetchEDGARTicker(ctx, ticker)
			mu.Lock()
			all = append(all, items...)
			mu.Unlock()
		}(t)
	}
	wg.Wait()

	sort.Slice(all, func(i, j int) bool { return all[i].PubTime.After(all[j].PubTime) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

func fetchEDGARTicker(ctx context.Context, ticker string) []Headline {
	reqCtx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("%s?action=getcompany&CIK=%s&type=&dateb=&owner=include&count=40&output=atom",
		EDGARBaseURL, url.QueryEscape(ticker))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", edgarUserAgent)
	req.Header.Set("Accept", "application/atom+xml")

	resp, err := feedClient.Do(req)
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
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil
	}

	var out []Headline
	for _, e := range feed.Entries {
		title := strings.TrimSpace(e.Title)
		if title == "" {
			continue
		}
		category := strings.TrimSpace(e.Category.Term)
		// Many entries lead with the filing type — keep what we know.
		if category == "" {
			category = "Filing"
		}
		var link string
		for _, l := range e.Links {
			if l.Rel == "alternate" || l.Rel == "" {
				link = l.Href
				break
			}
		}
		t := parseTime(e.Updated)
		out = append(out, Headline{
			Title:    title,
			Link:     link,
			Source:   "SEC:" + strings.ToUpper(ticker),
			PubTime:  t,
			Category: category,
		})
	}
	return out
}

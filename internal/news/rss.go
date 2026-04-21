package news

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Headline represents a single news item.
type Headline struct {
	Title   string
	Link    string
	Source  string
	PubTime time.Time
}

// Feed defines an RSS source.
type Feed struct {
	Name string
	URL  string
}

// DefaultFeeds returns the built-in RSS feed list.
func DefaultFeeds() []Feed {
	return []Feed{
		{Name: "Yahoo", URL: "https://finance.yahoo.com/news/rssindex"},
		{Name: "MarketWatch", URL: "http://feeds.marketwatch.com/marketwatch/topstories"},
		{Name: "CNBC", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114"},
	}
}

type rssRoot struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
}

// FetchAll fetches all feeds concurrently, deduplicates by URL, sorts by time descending, returns top 50.
func FetchAll(ctx context.Context, feeds []Feed) []Headline {
	var mu sync.Mutex
	var all []Headline

	var wg sync.WaitGroup
	for _, f := range feeds {
		wg.Add(1)
		go func(feed Feed) {
			defer wg.Done()
			items := fetchFeed(ctx, feed)
			mu.Lock()
			all = append(all, items...)
			mu.Unlock()
		}(f)
	}
	wg.Wait()

	// Deduplicate by URL
	seen := make(map[string]bool)
	deduped := all[:0]
	for _, h := range all {
		if !seen[h.Link] {
			seen[h.Link] = true
			deduped = append(deduped, h)
		}
	}

	// Sort by time descending
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].PubTime.After(deduped[j].PubTime)
	})

	if len(deduped) > 50 {
		deduped = deduped[:50]
	}
	return deduped
}

// feedTimeout bounds a single feed fetch so one slow source cannot stall FetchAll.
const feedTimeout = 8 * time.Second

var feedClient = &http.Client{Timeout: feedTimeout}

func fetchFeed(ctx context.Context, feed Feed) []Headline {
	reqCtx, cancel := context.WithTimeout(ctx, feedTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := feedClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var rss rssRoot
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil
	}

	var headlines []Headline
	for _, item := range rss.Channel.Items {
		t := parseTime(item.PubDate)
		headlines = append(headlines, Headline{
			Title:   item.Title,
			Link:    item.Link,
			Source:  feed.Name,
			PubTime: t,
		})
	}
	return headlines
}

func parseTime(s string) time.Time {
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// TimeAgo returns a human-readable relative time string.
func TimeAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh ago", h)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}

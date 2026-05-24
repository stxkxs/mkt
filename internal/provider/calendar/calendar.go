// Package calendar provides economic-release and earnings calendar data
// types and a curated 2026 schedule for the major US macro events. The
// earnings adapter is intentionally left as an interface for a focused
// follow-up.
package calendar

import (
	"context"
	"sort"
	"time"
)

// EventType identifies the kind of calendar event.
type EventType int

const (
	EconRelease EventType = iota
	Earnings
)

func (t EventType) String() string {
	switch t {
	case Earnings:
		return "Earnings"
	}
	return "Economic"
}

// Event is a single scheduled item on the calendar. Symbol is empty for
// economic releases.
type Event struct {
	Time       time.Time
	Title      string
	Type       EventType
	Importance int // 1-3; 3 = market-moving
	Symbol     string
}

// EconomicEvents returns the curated 2026 economic event schedule sorted
// by time. The data is baked into events_2026.go.
func EconomicEvents() []Event {
	out := make([]Event, len(econEvents2026))
	copy(out, econEvents2026)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// Upcoming returns events that occur within [now, now+window), sorted
// ascending by time.
func Upcoming(events []Event, now time.Time, window time.Duration) []Event {
	deadline := now.Add(window)
	var out []Event
	for _, e := range events {
		if e.Time.Before(now) {
			continue
		}
		if !e.Time.Before(deadline) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// EarningsSource fetches upcoming earnings for the given tickers. A
// concrete adapter (Yahoo, Nasdaq, etc.) is a follow-up; the calendar
// tab can plug one in once available.
type EarningsSource interface {
	Fetch(ctx context.Context, tickers []string) ([]Event, error)
}

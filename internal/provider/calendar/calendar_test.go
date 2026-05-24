package calendar

import (
	"testing"
	"time"
)

func TestEconomicEventsSorted(t *testing.T) {
	evs := EconomicEvents()
	if len(evs) == 0 {
		t.Fatal("expected non-empty schedule")
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Time.Before(evs[i-1].Time) {
			t.Fatalf("not sorted: %v before %v", evs[i].Time, evs[i-1].Time)
		}
	}
}

func TestEconomicEventsCoverage(t *testing.T) {
	evs := EconomicEvents()
	counts := map[string]int{}
	for _, e := range evs {
		switch {
		case e.Title == "" || e.Type != EconRelease:
			t.Errorf("malformed event: %+v", e)
		case e.Title[:3] == "CPI":
			counts["CPI"]++
		case e.Title[:8] == "Nonfarm ":
			counts["NFP"]++
		case e.Title[:4] == "FOMC":
			counts["FOMC"]++
		case e.Title[:3] == "GDP":
			counts["GDP"]++
		}
	}
	if counts["CPI"] != 12 {
		t.Errorf("CPI count = %d, want 12", counts["CPI"])
	}
	if counts["NFP"] != 12 {
		t.Errorf("NFP count = %d, want 12", counts["NFP"])
	}
	if counts["FOMC"] != 8 {
		t.Errorf("FOMC count = %d, want 8", counts["FOMC"])
	}
	if counts["GDP"] != 4 {
		t.Errorf("GDP count = %d, want 4", counts["GDP"])
	}
}

func TestUpcomingFilters(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	got := Upcoming(EconomicEvents(), now, 30*24*time.Hour)
	for _, e := range got {
		if e.Time.Before(now) {
			t.Errorf("Upcoming returned past event: %v", e.Time)
		}
		if e.Time.Sub(now) > 30*24*time.Hour {
			t.Errorf("Upcoming returned out-of-window event: %v", e.Time)
		}
	}
	if len(got) == 0 {
		t.Errorf("expected some upcoming events in March 2026")
	}
}

func TestUpcomingExactBoundary(t *testing.T) {
	now := time.Date(2026, 3, 18, 18, 0, 0, 0, time.UTC) // exactly a FOMC time
	window := time.Hour
	got := Upcoming(EconomicEvents(), now, window)
	// Event exactly at now is treated as "not before now" → included.
	// Event exactly at now+window is excluded (deadline is exclusive).
	if len(got) == 0 {
		t.Errorf("event at exactly now should be included")
	}
}

func TestEventTypeString(t *testing.T) {
	if Earnings.String() != "Earnings" {
		t.Errorf("Earnings.String() = %q", Earnings.String())
	}
	if EconRelease.String() != "Economic" {
		t.Errorf("EconRelease.String() = %q", EconRelease.String())
	}
}

package calendar

import "time"

// econEvents2026 is the curated 2026 US economic event schedule. Dates
// and times are sourced from public Fed / BLS / BEA calendars. All
// times are UTC at the published release time.
var econEvents2026 = []Event{
	// FOMC meetings (2-day events; we list the decision day at 18:00 UTC)
	{Time: time.Date(2026, 1, 28, 19, 0, 0, 0, time.UTC), Title: "FOMC Decision", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 3, 18, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision + SEP", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 4, 29, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 6, 17, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision + SEP", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 9, 16, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision + SEP", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC), Title: "FOMC Decision", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 12, 9, 19, 0, 0, 0, time.UTC), Title: "FOMC Decision + SEP", Type: EconRelease, Importance: 3},

	// CPI (typically 8:30 AM ET = 13:30 UTC, mid-month)
	{Time: time.Date(2026, 1, 14, 13, 30, 0, 0, time.UTC), Title: "CPI (Dec 2025)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 2, 11, 13, 30, 0, 0, time.UTC), Title: "CPI (Jan)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 3, 11, 13, 30, 0, 0, time.UTC), Title: "CPI (Feb)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 4, 14, 13, 30, 0, 0, time.UTC), Title: "CPI (Mar)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 5, 13, 13, 30, 0, 0, time.UTC), Title: "CPI (Apr)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 6, 10, 13, 30, 0, 0, time.UTC), Title: "CPI (May)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 7, 15, 13, 30, 0, 0, time.UTC), Title: "CPI (Jun)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 8, 12, 13, 30, 0, 0, time.UTC), Title: "CPI (Jul)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 9, 10, 13, 30, 0, 0, time.UTC), Title: "CPI (Aug)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 10, 14, 13, 30, 0, 0, time.UTC), Title: "CPI (Sep)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 11, 12, 13, 30, 0, 0, time.UTC), Title: "CPI (Oct)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 12, 10, 13, 30, 0, 0, time.UTC), Title: "CPI (Nov)", Type: EconRelease, Importance: 3},

	// Nonfarm Payrolls (first Friday of each month, 8:30 AM ET)
	{Time: time.Date(2026, 1, 9, 13, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Dec 2025)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 2, 6, 13, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Jan)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 3, 6, 13, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Feb)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 4, 3, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Mar)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 5, 1, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Apr)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 6, 5, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (May)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 7, 3, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Jun)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 8, 7, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Jul)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 9, 4, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Aug)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 10, 2, 12, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Sep)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 11, 6, 13, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Oct)", Type: EconRelease, Importance: 3},
	{Time: time.Date(2026, 12, 4, 13, 30, 0, 0, time.UTC), Title: "Nonfarm Payrolls (Nov)", Type: EconRelease, Importance: 3},

	// GDP — advance estimates (quarterly, ~4 weeks after quarter end)
	{Time: time.Date(2026, 1, 29, 13, 30, 0, 0, time.UTC), Title: "GDP Q4 2025 Advance", Type: EconRelease, Importance: 2},
	{Time: time.Date(2026, 4, 30, 12, 30, 0, 0, time.UTC), Title: "GDP Q1 Advance", Type: EconRelease, Importance: 2},
	{Time: time.Date(2026, 7, 30, 12, 30, 0, 0, time.UTC), Title: "GDP Q2 Advance", Type: EconRelease, Importance: 2},
	{Time: time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC), Title: "GDP Q3 Advance", Type: EconRelease, Importance: 2},
}

package calendar

import (
	"strings"
	"testing"
	"time"
)

// Every source publishes in US Eastern time, so the UTC hour stored for a
// release depends on whether daylight saving is in force that day. A row
// written with the winter offset in summer is an hour late on screen.
func TestEventTimesAreTheirEasternReleaseTimes(t *testing.T) {
	et, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	want := map[string]string{
		"FOMC":             "14:00",
		"CPI":              "08:30",
		"Nonfarm Payrolls": "08:30",
		"GDP":              "08:30",
	}
	checked := 0
	for _, e := range econEvents2026 {
		for prefix, clock := range want {
			if !strings.HasPrefix(e.Title, prefix) {
				continue
			}
			checked++
			if got := e.Time.In(et).Format("15:04"); got != clock {
				t.Errorf("%s on %s: %s ET, want %s ET", e.Title, e.Time.Format("2006-01-02"), got, clock)
			}
		}
	}
	if checked != len(econEvents2026) {
		t.Errorf("checked %d of %d events; every row needs an expected release time", checked, len(econEvents2026))
	}
}

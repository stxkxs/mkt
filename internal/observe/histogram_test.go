package observe

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHistogramBucketEdgeIsInclusive(t *testing.T) {
	// le semantics: a value sitting exactly on a bound belongs to that
	// bound's bucket. Getting this wrong shifts every boundary observation
	// one bucket up and quietly inflates every quantile read off the series.
	h := NewHistogram("observe_test_edge_seconds", "test histogram", []float64{0.01, 0.1, 1})
	h.Observe(0.1)
	got := h.Buckets()
	want := []uint64{0, 1, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cumulative buckets = %v, want %v (0.1 belongs to le=0.1)", got, want)
		}
	}
}

func TestHistogramBucketPlacement(t *testing.T) {
	h := NewHistogram("observe_test_placement_seconds", "test histogram", []float64{1, 2, 3})
	for _, v := range []float64{0.5, 1, 1.5, 2, 2.5, 3, 3.5, 100} {
		h.Observe(v)
	}
	// le=1 holds 0.5 and 1; le=2 adds 1.5 and 2; le=3 adds 2.5 and 3; the
	// remaining two land past the last bound, in the implicit +Inf bucket.
	want := []uint64{2, 4, 6}
	got := h.Buckets()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cumulative buckets = %v, want %v", got, want)
		}
	}
	if h.Count() != 8 {
		t.Errorf("Count = %d, want 8", h.Count())
	}
}

func TestHistogramCountAndSum(t *testing.T) {
	h := NewHistogram("observe_test_countsum_seconds", "test histogram", []float64{0.5, 5})
	values := []float64{0.25, 0.75, 4, 7}
	var want float64
	for _, v := range values {
		h.Observe(v)
		want += v
	}
	if got := h.Count(); got != uint64(len(values)) {
		t.Errorf("Count = %d, want %d", got, len(values))
	}
	if got := h.Sum(); got != want {
		t.Errorf("Sum = %v, want %v", got, want)
	}
}

func TestHistogramTextCountMatchesInfBucket(t *testing.T) {
	h := NewHistogram("observe_test_text_seconds", "test histogram", []float64{0.01, 0.1})
	h.Observe(0.005)
	h.Observe(0.05)
	h.Observe(2)
	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(h.text()), "\n") {
		name, value, _ := strings.Cut(line, " ")
		lines[name] = value
	}
	for name, want := range map[string]string{
		`observe_test_text_seconds_bucket{le="0.01"}`: "1",
		`observe_test_text_seconds_bucket{le="0.1"}`:  "2",
		`observe_test_text_seconds_bucket{le="+Inf"}`: "3",
		"observe_test_text_seconds_count":             "3",
	} {
		if got := lines[name]; got != want {
			t.Errorf("%s = %q, want %q\n%s", name, got, want, h.text())
		}
	}
	// _count is what a scraper divides _sum by; if it can disagree with the
	// +Inf bucket, a rate() over the two series reports different traffic.
	if lines["observe_test_text_seconds_count"] != lines[`observe_test_text_seconds_bucket{le="+Inf"}`] {
		t.Errorf("_count and the +Inf bucket disagree:\n%s", h.text())
	}
	sum, err := strconv.ParseFloat(lines["observe_test_text_seconds_sum"], 64)
	if err != nil {
		t.Fatalf("_sum is not a float: %v", err)
	}
	if want := 0.005 + 0.05 + 2; sum != want {
		t.Errorf("_sum = %v, want %v", sum, want)
	}
	// The exposition format puts HELP before TYPE, and both before the
	// first sample of the series.
	want := "# HELP observe_test_text_seconds test histogram\n" +
		"# TYPE observe_test_text_seconds histogram\n"
	if !strings.HasPrefix(h.text(), want) {
		t.Errorf("missing or misplaced HELP/TYPE lines:\n%s", h.text())
	}
}

func TestHistogramObserveDuration(t *testing.T) {
	h := NewHistogram("observe_test_duration_seconds", "test histogram", []float64{1})
	h.ObserveDuration(1500 * time.Millisecond)
	if got := h.Sum(); got != 1.5 {
		t.Errorf("Sum = %v, want 1.5 (durations are recorded in seconds)", got)
	}
	if got := h.Buckets()[0]; got != 0 {
		t.Errorf("le=1 bucket = %d, want 0: 1.5s belongs past the last bound", got)
	}
}

func TestHistogramConcurrentObserve(t *testing.T) {
	h := NewHistogram("observe_test_concurrent_seconds", "test histogram", []float64{0.5, 1})
	const workers, each = 50, 200
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				h.Observe(0.25)
			}
		}()
	}
	wg.Wait()
	if got := h.Count(); got != workers*each {
		t.Errorf("Count = %d, want %d", got, workers*each)
	}
	if got, want := h.Sum(), float64(workers*each)*0.25; got != want {
		t.Errorf("Sum = %v, want %v", got, want)
	}
}

func TestNewHistogramSortsAndCopiesBounds(t *testing.T) {
	bounds := []float64{1, 0.1, 0.01}
	h := NewHistogram("observe_test_bounds_seconds", "test histogram", bounds)
	bounds[0] = 99
	if got := h.Bounds(); got[0] != 0.01 || got[1] != 0.1 || got[2] != 1 {
		t.Errorf("Bounds = %v, want ascending and independent of the caller's slice", got)
	}
}

func TestNewHistogramDropsRepeatedBounds(t *testing.T) {
	h := NewHistogram("observe_test_repeated_seconds", "test histogram", []float64{2, 1, 1, 2, 3})
	if got := h.Bounds(); len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("Bounds = %v, want one entry per distinct edge", got)
	}
	h.Observe(1)
	h.Observe(3)
	seen := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(h.text()), "\n") {
		if le, ok := strings.CutPrefix(line, "observe_test_repeated_seconds_bucket{"); ok {
			seen[strings.SplitN(le, "}", 2)[0]]++
		}
	}
	for le, n := range seen {
		if n > 1 {
			t.Errorf("bucket %s emitted %d times: one le is one series", le, n)
		}
	}
	if got, want := h.Count(), uint64(2); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
	if got, want := h.Buckets(), []uint64{1, 1, 2}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("Buckets = %v, want %v", got, want)
	}
}

// text renders one histogram in isolation, without the rest of the registry.
func (h *Histogram) text() string {
	var sb strings.Builder
	h.writeText(&sb)
	return sb.String()
}

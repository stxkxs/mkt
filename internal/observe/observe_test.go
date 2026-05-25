package observe

import (
	"sync"
	"testing"
)

func TestCounterIncAndValue(t *testing.T) {
	c := NewCounter("observe_test_inc_total")
	start := c.Value()
	c.Inc()
	c.Inc()
	c.Add(3)
	if got := c.Value() - start; got != 5 {
		t.Errorf("after 2 Inc + 1 Add(3): delta=%d, want 5", got)
	}
}

func TestCounterConcurrentInc(t *testing.T) {
	c := NewCounter("observe_test_concurrent_total")
	start := c.Value()
	const workers = 100
	const each = 100
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				c.Inc()
			}
		}()
	}
	wg.Wait()
	if got := c.Value() - start; got != workers*each {
		t.Errorf("concurrent inc: delta=%d, want %d", got, workers*each)
	}
}

func TestSnapshotIncludesAllCounters(t *testing.T) {
	c := NewCounter("observe_test_snapshot_total")
	c.Inc()
	snap := Snapshot()
	v, ok := snap["observe_test_snapshot_total"]
	if !ok {
		t.Fatal("snapshot missing counter")
	}
	if v == 0 {
		t.Errorf("snapshot value for counter: got %d, want >0", v)
	}
}

func TestSortedNamesIsLexicographic(t *testing.T) {
	NewCounter("observe_test_z_total")
	NewCounter("observe_test_a_total")
	names := SortedNames()
	var seen []string
	for _, n := range names {
		if n == "observe_test_z_total" || n == "observe_test_a_total" {
			seen = append(seen, n)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected both test counters, got %v", seen)
	}
	if seen[0] != "observe_test_a_total" || seen[1] != "observe_test_z_total" {
		t.Errorf("not lex-sorted: %v", seen)
	}
}

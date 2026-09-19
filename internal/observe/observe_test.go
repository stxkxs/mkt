package observe

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCounterIncAndValue(t *testing.T) {
	c := NewCounter("observe_test_inc_total", "test counter")
	start := c.Value()
	c.Inc()
	c.Inc()
	c.Add(3)
	if got := c.Value() - start; got != 5 {
		t.Errorf("after 2 Inc + 1 Add(3): delta=%d, want 5", got)
	}
}

func TestCounterConcurrentInc(t *testing.T) {
	c := NewCounter("observe_test_concurrent_total", "test counter")
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

// A scrape lists series in lexicographic order, so diffing two scrapes
// shows what changed rather than what moved.
func TestTextIsOrderedByName(t *testing.T) {
	NewCounter("observe_test_order_z_total", "test counter")
	NewCounter("observe_test_order_a_total", "test counter")

	text := Text()
	a := strings.Index(text, "observe_test_order_a_total")
	z := strings.Index(text, "observe_test_order_z_total")
	if a < 0 || z < 0 {
		t.Fatalf("both counters should appear in the scrape:\n%s", text)
	}
	if a > z {
		t.Errorf("series are not ordered by name: a at %d, z at %d", a, z)
	}
}

// An operator reads HELP instead of inferring meaning from a name, so every
// series carries one.
func TestEverySeriesCarriesHelp(t *testing.T) {
	NewCounter("observe_test_help_total", "a described counter")
	NewGauge("observe_test_help_level", "a described gauge")
	NewHistogram("observe_test_help_seconds", "a described histogram", []float64{1})

	text := Text()
	for _, want := range []string{
		"# HELP observe_test_help_total a described counter\n",
		"# HELP observe_test_help_level a described gauge\n",
		"# HELP observe_test_help_seconds a described histogram\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("scrape missing %q:\n%s", want, text)
		}
	}
}

// A newline or backslash in a description would end the HELP line early and
// corrupt every series after it.
func TestHelpIsEscaped(t *testing.T) {
	NewCounter("observe_test_escape_total", "one\ntwo\\three")
	if !strings.Contains(Text(), `# HELP observe_test_escape_total one\ntwo\\three`+"\n") {
		t.Errorf("HELP not escaped:\n%s", Text())
	}
}

func TestGaugeSetAndValue(t *testing.T) {
	g := NewGauge("observe_test_gauge_level", "test gauge")
	g.Set(12.5)
	if got := g.Value(); got != 12.5 {
		t.Errorf("Value = %v, want 12.5", got)
	}
	g.Set(-3)
	if got := g.Value(); got != -3 {
		t.Errorf("Value = %v, want -3: a gauge moves in both directions", got)
	}
}

func TestGaugeConcurrentSetAndRead(t *testing.T) {
	// A gauge is a float64 behind a single atomic word: a reader must never
	// see a value nobody wrote, which is what a torn read of a plain float64
	// would hand back under a concurrent store.
	g := NewGauge("observe_test_gauge_concurrent_level", "test gauge")
	values := []float64{1.5, -2.25, 1e9, 0}
	written := make(map[float64]bool, len(values))
	for _, v := range values {
		written[v] = true
	}

	var writers, readers sync.WaitGroup
	stop := make(chan struct{})
	for _, v := range values {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 2000 {
				g.Set(v)
			}
		}()
	}
	var unwritten atomic.Value
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if got := g.Value(); !written[got] {
					unwritten.Store(got)
					return
				}
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()
	if v := unwritten.Load(); v != nil {
		t.Errorf("read %v, which no writer ever set", v)
	}
	g.Set(42)
	if got := g.Value(); got != 42 {
		t.Errorf("Value = %v, want 42", got)
	}
}

func TestRegisterCounterFuncReadsAtScrapeTime(t *testing.T) {
	var n uint64
	RegisterCounterFunc("observe_test_func_total", "test counter", func() uint64 { return n })
	if got := seriesLine(t, "observe_test_func_total"); got != "observe_test_func_total 0" {
		t.Errorf("before increment: %q", got)
	}
	n = 9
	if got := seriesLine(t, "observe_test_func_total"); got != "observe_test_func_total 9" {
		t.Errorf("after increment: %q", got)
	}
	if !strings.Contains(Text(), "# TYPE observe_test_func_total counter\n") {
		t.Error("func counter missing its TYPE line")
	}
}

func TestRegisterGaugeFuncReadsAtScrapeTime(t *testing.T) {
	level := 0.0
	RegisterGaugeFunc("observe_test_func_level", "test gauge", func() float64 { return level })
	level = 2.5
	if got := seriesLine(t, "observe_test_func_level"); got != "observe_test_func_level 2.5" {
		t.Errorf("gauge func line = %q", got)
	}
	if !strings.Contains(Text(), "# TYPE observe_test_func_level gauge\n") {
		t.Error("func gauge missing its TYPE line")
	}
}

func TestOneNameRendersOneSeries(t *testing.T) {
	// A scrape carrying the same series twice is rejected by the collector
	// reading it, so the registry keeps one metric per name.
	NewCounter("observe_test_duplicate_total", "test counter")
	NewCounter("observe_test_duplicate_total", "test counter")
	if got := strings.Count(Text(), "# TYPE observe_test_duplicate_total counter\n"); got != 1 {
		t.Errorf("TYPE lines for one name: %d, want 1", got)
	}
	if got := strings.Count(Text(), "\nobserve_test_duplicate_total "); got != 1 {
		t.Errorf("sample lines for one name: %d, want 1", got)
	}
}

func TestTextIsLexicographic(t *testing.T) {
	NewCounter("observe_test_order_b_total", "test counter")
	NewCounter("observe_test_order_a_total", "test counter")
	text := Text()
	a := strings.Index(text, "observe_test_order_a_total")
	b := strings.Index(text, "observe_test_order_b_total")
	if a < 0 || b < 0 || a > b {
		t.Errorf("series are not emitted in name order (a=%d, b=%d)", a, b)
	}
}

// seriesLine returns the sample line Text emits for name, without its TYPE
// line. It fails the test when the series is absent.
func seriesLine(t *testing.T, name string) string {
	t.Helper()
	for _, line := range strings.Split(Text(), "\n") {
		if strings.HasPrefix(line, name+" ") {
			return line
		}
	}
	t.Fatalf("series %q missing from Text()", name)
	return ""
}

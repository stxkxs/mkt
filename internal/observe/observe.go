// Package observe is a tiny, dependency-free metrics registry behind the
// /metrics endpoint.
//
// It carries counters, gauges and histograms rather than pulling in
// prometheus/client_golang, which preserves the project's "single binary,
// minimal deps" property. Every metric self-registers at construction and
// Text renders the whole registry in Prometheus text exposition format, so
// a series reaches /metrics by being constructed and by nothing else.
//
// Series are label-free: one name is one series, which bounds cardinality by
// construction. A value that lives in another package's own atomics is
// published with RegisterCounterFunc or RegisterGaugeFunc, which read it at
// scrape time instead of requiring that package to push.
//
// Every type here is safe for concurrent use — providers record from their
// own goroutines while an HTTP handler renders — and the record paths use
// atomics so a scrape cannot stall a fetch.
package observe

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// metric is the registry's view of one series: a stable name plus the
// Prometheus text it renders to.
type metric interface {
	Name() string
	writeText(*strings.Builder)
}

var (
	registryMu sync.Mutex
	registry   = make(map[string]metric)
)

// register publishes m under its name. A name identifies one series, so
// registering a name that is already present replaces it: a scrape carrying
// the same series twice is rejected by the collector reading it.
func register(m metric) {
	registryMu.Lock()
	registry[m.Name()] = m
	registryMu.Unlock()
}

// Counter is a monotonic uint64 with a stable Prometheus-style name.
type Counter struct {
	name string
	help string
	val  atomic.Uint64
}

// NewCounter constructs a counter and registers it. Names follow Prometheus
// conventions: snake_case, _total suffix. help is the one-line description
// a scrape carries; it is what an operator reads instead of guessing from
// the name.
func NewCounter(name, help string) *Counter {
	c := &Counter{name: name, help: help}
	register(c)
	return c
}

// Inc increments the counter by one.
func (c *Counter) Inc() { c.val.Add(1) }

// Add increments the counter by n.
func (c *Counter) Add(n uint64) { c.val.Add(n) }

// Name returns the counter's stable name (used in /metrics output).
func (c *Counter) Name() string { return c.name }

// Value returns the current count.
func (c *Counter) Value() uint64 { return c.val.Load() }

func (c *Counter) writeText(sb *strings.Builder) {
	writeTyped(sb, c.name, c.help, "counter", strconv.FormatUint(c.val.Load(), 10))
}

// Gauge is a float64 level that moves in both directions — a queue depth, a
// backlog, a ratio.
type Gauge struct {
	name string
	help string
	bits atomic.Uint64 // math.Float64bits of the current value
}

// NewGauge constructs a gauge and registers it. Names follow Prometheus
// conventions: snake_case, and a unit suffix where the level has one.
func NewGauge(name, help string) *Gauge {
	g := &Gauge{name: name, help: help}
	register(g)
	return g
}

// Set replaces the gauge's value.
func (g *Gauge) Set(v float64) { g.bits.Store(math.Float64bits(v)) }

// Name returns the gauge's stable name (used in /metrics output).
func (g *Gauge) Name() string { return g.name }

// Value returns the current level.
func (g *Gauge) Value() float64 { return math.Float64frombits(g.bits.Load()) }

func (g *Gauge) writeText(sb *strings.Builder) {
	writeTyped(sb, g.name, g.help, "gauge", formatFloat(g.Value()))
}

// funcMetric publishes a value another package owns, read at scrape time. It
// exists for numbers that are derived rather than accumulated here: a hub's
// drop counts live in the hub's own atomics, and its observer backlog is a
// maximum across observers that only the hub can compute.
type funcMetric struct {
	name  string
	help  string
	kind  string
	value func() string
}

func (f *funcMetric) Name() string { return f.name }

func (f *funcMetric) writeText(sb *strings.Builder) {
	writeTyped(sb, f.name, f.help, f.kind, f.value())
}

// RegisterCounterFunc publishes a monotonic series whose value fn returns,
// read once per scrape. fn must not block: it runs inside the /metrics
// handler.
func RegisterCounterFunc(name, help string, fn func() uint64) {
	register(&funcMetric{name: name, help: help, kind: "counter", value: func() string {
		return strconv.FormatUint(fn(), 10)
	}})
}

// RegisterGaugeFunc publishes a level whose value fn returns, read once per
// scrape. fn must not block: it runs inside the /metrics handler.
func RegisterGaugeFunc(name, help string, fn func() float64) {
	register(&funcMetric{name: name, help: help, kind: "gauge", value: func() string {
		return formatFloat(fn())
	}})
}

// Text renders every registered metric in Prometheus text exposition format,
// names in lexicographic order so a scrape diff stays stable.
//
// Rendering happens outside the registry lock. A func metric reads another
// package's state and takes that package's lock, so holding this one across
// the call would pin two lock orders together.
func Text() string {
	var sb strings.Builder
	for _, m := range registered() {
		m.writeText(&sb)
	}
	return sb.String()
}

// registered returns every metric, ordered by name.
func registered() []metric {
	registryMu.Lock()
	defer registryMu.Unlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]metric, len(names))
	for i, name := range names {
		out[i] = registry[name]
	}
	return out
}

// writeTyped emits one scalar series: its HELP and TYPE lines and its
// value. A series without a description makes an operator infer its meaning
// from its name, which is the guess a runbook exists to remove.
func writeTyped(sb *strings.Builder, name, help, kind, value string) {
	writeHelp(sb, name, help)
	sb.WriteString("# TYPE ")
	sb.WriteString(name)
	sb.WriteByte(' ')
	sb.WriteString(kind)
	sb.WriteByte('\n')
	sb.WriteString(name)
	sb.WriteByte(' ')
	sb.WriteString(value)
	sb.WriteByte('\n')
}

// writeHelp emits a series' HELP line. A newline or backslash in a
// description would break the exposition format, so both are escaped as the
// format requires.
func writeHelp(sb *strings.Builder, name, help string) {
	if help == "" {
		return
	}
	sb.WriteString("# HELP ")
	sb.WriteString(name)
	sb.WriteByte(' ')
	sb.WriteString(escapeHelp(help))
	sb.WriteByte('\n')
}

// escapeHelp applies the two escapes the text exposition format defines for
// a HELP line.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\n", "\\n")
}

// formatFloat renders a float the way the Prometheus text format reads it:
// shortest round-trippable form, with Go's spellings of the special values
// (NaN, +Inf, -Inf) which that format accepts verbatim.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

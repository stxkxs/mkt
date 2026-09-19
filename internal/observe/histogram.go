package observe

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Histogram counts observations into fixed buckets and tracks their running
// count and sum. It exposes the three series Prometheus expects of a
// histogram — _bucket, _sum and _count — and carries no labels beyond the
// bucket's le, so one histogram is one bounded set of series.
//
// Buckets are fixed at construction: a quantile read from this is only as
// good as the bounds chosen for it, and a bound cannot be added later
// without breaking the series a dashboard already reads.
type Histogram struct {
	name    string
	help    string
	bounds  []float64       // inclusive upper edges, ascending
	counts  []atomic.Uint64 // one per bound plus the implicit +Inf bucket
	sumBits atomic.Uint64   // math.Float64bits of the running sum
}

// NewHistogram constructs a histogram over bounds — the inclusive upper edge
// of each bucket — and registers it. An observation lands in the first
// bucket whose bound is greater than or equal to it; one past the last bound
// lands in the implicit +Inf bucket. bounds is copied, sorted and deduped, so
// callers can share a slice of bucket edges. help is the one-line
// description a scrape carries.
func NewHistogram(name, help string, bounds []float64) *Histogram {
	edges := append([]float64(nil), bounds...)
	sort.Float64s(edges)
	edges = dedupe(edges)
	h := &Histogram{
		name:   name,
		help:   help,
		bounds: edges,
		counts: make([]atomic.Uint64, len(edges)+1),
	}
	register(h)
	return h
}

// dedupe drops repeated edges from an ascending slice, in place. A repeated
// edge emits one le twice, and a collector rejects a scrape carrying the same
// series twice — so one careless bucket list would cost every series on the
// endpoint, not just the histogram that carries it.
func dedupe(ascending []float64) []float64 {
	out := ascending[:0]
	for _, v := range ascending {
		if len(out) == 0 || v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// Observe records one value.
func (h *Histogram) Observe(v float64) {
	// SearchFloat64s returns the first index whose bound is >= v, which is
	// exactly the le semantics of a Prometheus bucket: a value sitting on a
	// bound belongs to that bound's bucket, not the next one.
	h.counts[sort.SearchFloat64s(h.bounds, v)].Add(1)
	addFloat(&h.sumBits, v)
}

// ObserveDuration records d in seconds, the unit every duration series in
// this tree is expressed in.
func (h *Histogram) ObserveDuration(d time.Duration) { h.Observe(d.Seconds()) }

// Name returns the histogram's stable name. The emitted series suffix it
// with _bucket, _sum and _count.
func (h *Histogram) Name() string { return h.name }

// Count returns the number of recorded observations.
func (h *Histogram) Count() uint64 {
	var total uint64
	for i := range h.counts {
		total += h.counts[i].Load()
	}
	return total
}

// Sum returns the sum of every recorded observation.
func (h *Histogram) Sum() float64 { return math.Float64frombits(h.sumBits.Load()) }

// Bounds returns the bucket edges, ascending.
func (h *Histogram) Bounds() []float64 { return append([]float64(nil), h.bounds...) }

// Buckets returns the cumulative count for each bound in Bounds order. The
// count past the last bound is the difference between Count and the last
// entry.
func (h *Histogram) Buckets() []uint64 {
	out := make([]uint64, len(h.bounds))
	var cum uint64
	for i := range h.bounds {
		cum += h.counts[i].Load()
		out[i] = cum
	}
	return out
}

func (h *Histogram) writeText(sb *strings.Builder) {
	writeHelp(sb, h.name, h.help)
	sb.WriteString("# TYPE ")
	sb.WriteString(h.name)
	sb.WriteString(" histogram\n")
	// _count is accumulated from the same bucket loads rather than read
	// separately, so it always equals the +Inf bucket. An observation racing
	// the scrape lands in the next one whole instead of splitting across the
	// two series.
	var cum uint64
	for i, bound := range h.bounds {
		cum += h.counts[i].Load()
		h.writeBucket(sb, formatFloat(bound), cum)
	}
	cum += h.counts[len(h.bounds)].Load()
	h.writeBucket(sb, "+Inf", cum)
	sb.WriteString(h.name)
	sb.WriteString("_sum ")
	sb.WriteString(formatFloat(h.Sum()))
	sb.WriteByte('\n')
	sb.WriteString(h.name)
	sb.WriteString("_count ")
	sb.WriteString(strconv.FormatUint(cum, 10))
	sb.WriteByte('\n')
}

// writeBucket emits one cumulative bucket line.
func (h *Histogram) writeBucket(sb *strings.Builder, le string, cum uint64) {
	sb.WriteString(h.name)
	sb.WriteString(`_bucket{le="`)
	sb.WriteString(le)
	sb.WriteString(`"} `)
	sb.WriteString(strconv.FormatUint(cum, 10))
	sb.WriteByte('\n')
}

// addFloat adds v to a float64 held as bits. The compare-and-swap retries
// under contention, which keeps the record path off a mutex: providers
// record from their own goroutines on every fetch.
func addFloat(bits *atomic.Uint64, v float64) {
	for {
		old := bits.Load()
		next := math.Float64bits(math.Float64frombits(old) + v)
		if bits.CompareAndSwap(old, next) {
			return
		}
	}
}

package correlation

import "time"

// matrixMemo holds the last computed matrix together with the inputs it was
// computed from. Building the matrix takes the cache lock once per symbol,
// allocates a sample slice for each, resamples every series onto the bucket
// grid and then fills k² cells over m buckets. The result changes only when
// one of the memo's key fields does, and a render that changes none of them
// is the common case: Bubbletea calls View after every Update, and a quote
// for a symbol this tab does not even show still drives one.
//
// The memo is reached through a pointer because View has a value receiver and
// Update returns a new Model — a value field could not be written from the
// place that needs the result. Every copy of a Model therefore shares one
// memo, which is safe because the memo is a pure function cache: the stored
// matrix is handed back only when the stored key equals the key the caller
// arrived with, so a copy carrying a different bucket, window or cache
// generation recomputes instead of reading another copy's answer. Copies with
// differing keys cost a recompute each; none of them can surface a matrix
// that does not belong to its own inputs.
//
// The matrix handed back is shared with every later caller holding the same
// key, so callers read it and do not write to it.
type matrixMemo struct {
	syms   []string
	bucket time.Duration
	gen    uint64
	matrix [][]float64
	valid  bool

	// computes counts matrix builds. Not rebuilding on an unchanged key is
	// the property this type exists for, and the count is the only thing
	// that observes whether it holds — the rendered grid is identical
	// either way.
	computes int
}

// fresh reports whether the memo already holds the matrix for these inputs.
// syms is the window, which moves with the symbol set, the scroll offset and
// the terminal size, so comparing it covers all three.
func (mm *matrixMemo) fresh(syms []string, bucket time.Duration, gen uint64) bool {
	if !mm.valid || mm.bucket != bucket || mm.gen != gen || len(mm.syms) != len(syms) {
		return false
	}
	for i, s := range syms {
		if mm.syms[i] != s {
			return false
		}
	}
	return true
}

// store records a freshly built matrix under the inputs that produced it.
// The window is copied because it aliases the caller's symbol slice, which
// SetSymbols is free to replace.
func (mm *matrixMemo) store(syms []string, bucket time.Duration, gen uint64, matrix [][]float64) {
	mm.syms = append(mm.syms[:0:0], syms...)
	mm.bucket = bucket
	mm.gen = gen
	mm.matrix = matrix
	mm.valid = true
	mm.computes++
}

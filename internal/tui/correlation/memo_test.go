package correlation

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
)

var memoSymbols = []string{"AAA", "BBB", "CCC"}

// memoModel is a sized tab over three fully sampled series, wide enough for
// the whole symbol list to sit in the window.
func memoModel() (Model, *market.Cache) {
	cache := market.NewCache(120)
	base := time.Unix(1_700_000_000, 0).UTC()
	for i, s := range memoSymbols {
		prices := make([]float64, 40)
		for j := range prices {
			prices[j] = 100 + float64(i)*10 + 5*math.Sin(float64(j+i*3)/4)
		}
		push(cache, s, base, time.Minute, prices)
	}
	m := New(memoSymbols, cache)
	m.SetSize(120, 40)
	return m, cache
}

// render draws the tab and reports how many times the matrix has been built
// in total.
func render(m Model) int {
	_ = m.View()
	return m.memo.computes
}

func TestMatrixBuiltOncePerUnchangedFrame(t *testing.T) {
	m, _ := memoModel()
	if got := render(m); got != 1 {
		t.Fatalf("first render built the matrix %d times, want 1", got)
	}
	for range 50 {
		m = press(m, "x") // a key the tab ignores; Bubbletea renders anyway
		render(m)
	}
	if got := m.memo.computes; got != 1 {
		t.Errorf("51 renders over an unchanged cache built the matrix %d times, want 1", got)
	}
}

func TestMatrixRebuiltAfterTick(t *testing.T) {
	m, cache := memoModel()
	before := render(m)

	cache.Push(provider.Quote{
		Symbol:    "AAA",
		Price:     123,
		Timestamp: time.Unix(1_700_000_000, 0).UTC().Add(time.Hour),
	})

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after a tick landed, want %d", got, before+1)
	}
}

// A tick for a symbol the tab does not show still moves the cache, so the
// generation is a signal to recompute and not a claim that the result
// differs. Correctness comes first: the tab must never paint a matrix that
// predates data it could have read.
func TestMatrixRebuiltAfterTickForUnshownSymbol(t *testing.T) {
	m, cache := memoModel()
	before := render(m)

	cache.Push(provider.Quote{Symbol: "ZZZ", Price: 1, Timestamp: time.Now()})

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after an unrelated tick, want %d", got, before+1)
	}
}

func TestMatrixRebuiltAfterSeed(t *testing.T) {
	m, cache := memoModel()
	before := render(m)

	if !cache.Seed("DDD", []float64{1, 2, 3}) {
		t.Fatal("Seed(DDD) reported no seed on a fresh symbol")
	}

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after a backfill, want %d", got, before+1)
	}
}

func TestMatrixRebuiltAfterBucketChange(t *testing.T) {
	m, _ := memoModel()
	before := render(m)

	bucket := m.Bucket()
	m = press(m, "b")
	if m.Bucket() == bucket {
		t.Fatal("b did not change the resampling bucket")
	}

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after a bucket change, want %d", got, before+1)
	}
}

func TestMatrixRebuiltAfterSymbolSetChange(t *testing.T) {
	m, _ := memoModel()
	before := render(m)

	m.SetSymbols([]string{"AAA", "CCC"})

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after SetSymbols, want %d", got, before+1)
	}
}

func TestMatrixRebuiltAfterResizeThatMovesTheWindow(t *testing.T) {
	m, _ := memoModel()
	before := render(m)

	m.SetSize(20, 40)
	if syms, _ := m.window(); len(syms) == len(memoSymbols) {
		t.Fatalf("resize left the window at %d symbols; pick a narrower width", len(syms))
	}

	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after a resize, want %d", got, before+1)
	}
}

// The key is the window, not the terminal size: a resize that leaves the same
// symbols on screen leaves the matrix unchanged too.
func TestMatrixNotRebuiltWhenResizeLeavesTheWindow(t *testing.T) {
	m, _ := memoModel()
	before := render(m)

	widest, _ := m.window()
	m.SetSize(121, 41)
	if syms, _ := m.window(); len(syms) != len(widest) {
		t.Fatalf("resize moved the window to %d symbols; pick a width that does not", len(syms))
	}

	if got := render(m); got != before {
		t.Errorf("computes = %d after a resize that kept the window, want %d", got, before)
	}
}

func TestScrollingRebuildsTheMatrix(t *testing.T) {
	m := New(symbols(30), market.NewCache(60))
	m.SetSize(80, 20)
	before := render(m)

	m = press(m, "l")
	if got := render(m); got != before+1 {
		t.Errorf("computes = %d after scrolling the window, want %d", got, before+1)
	}
}

// The memo must be indistinguishable from computing every time. A second
// model over the same cache never memoizes across these steps, because each
// step changes its key too, so its answer is the uncached one.
func TestMemoAgreesWithAFreshComputation(t *testing.T) {
	m, cache := memoModel()

	steps := []struct {
		name string
		do   func(*Model)
	}{
		{"initial", func(*Model) {}},
		{"tick", func(*Model) {
			cache.Push(provider.Quote{Symbol: "BBB", Price: 99, Timestamp: time.Now()})
		}},
		{"bucket", func(m *Model) { *m = press(*m, "b") }},
		{"resize", func(m *Model) { m.SetSize(60, 30) }},
		{"symbols", func(m *Model) { m.SetSymbols([]string{"CCC", "AAA"}) }},
		{"repeat", func(*Model) {}},
	}

	for _, step := range steps {
		step.do(&m)
		syms, _ := m.window()

		fresh := New(m.symbols, cache)
		fresh.SetSize(m.width, m.height)
		fresh.bucketI = m.bucketI
		fresh.offset = m.offset

		want := fresh.matrixFor(syms)
		got := m.matrixFor(syms)
		if len(got) != len(want) {
			t.Fatalf("%s: matrix is %dx? , want %d rows", step.name, len(got), len(want))
		}
		for i := range want {
			for j := range want[i] {
				a, b := got[i][j], want[i][j]
				if math.IsNaN(a) && math.IsNaN(b) {
					continue
				}
				if a != b {
					t.Errorf("%s: cell[%d][%d] = %v, uncached = %v", step.name, i, j, a, b)
				}
			}
		}
	}
}

// Copies of a Model share one memo, so a key that does not belong to the copy
// doing the reading must never hand back a matrix. Alternating two copies
// that differ only in bucket costs a rebuild each and returns each copy's own
// answer.
func TestSharedMemoNeverServesAnotherCopysKey(t *testing.T) {
	base, _ := memoModel()
	other := press(base, "b")
	if base.Bucket() == other.Bucket() {
		t.Fatal("the two copies share a bucket; nothing is being tested")
	}

	if base.memo != other.memo {
		t.Fatal("the copies do not share a memo; this test is not covering what it names")
	}

	syms, _ := base.window()
	wantBase := slices.Clone(base.matrixFor(syms))
	wantOther := slices.Clone(other.matrixFor(syms))

	for range 5 {
		checkSameMatrix(t, "base", base.matrixFor(syms), wantBase)
		checkSameMatrix(t, "other", other.matrixFor(syms), wantOther)
	}
	if got, want := base.memo.computes, 2+5*2; got != want {
		t.Errorf("computes = %d, want %d: alternating keys rebuild every time", got, want)
	}
}

func checkSameMatrix(t *testing.T, name string, got, want [][]float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d rows, want %d", name, len(got), len(want))
	}
	for i := range want {
		for j := range want[i] {
			a, b := got[i][j], want[i][j]
			if math.IsNaN(a) && math.IsNaN(b) {
				continue
			}
			if a != b {
				t.Fatalf("%s: cell[%d][%d] = %v, want %v", name, i, j, a, b)
			}
		}
	}
}

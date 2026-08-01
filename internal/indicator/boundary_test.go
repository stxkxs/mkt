package indicator

import (
	"math"
	"testing"

	"github.com/stxkxs/mkt/internal/provider"
)

// boundaryCase is one adversarial OHLCV series. The TUI renders indicator
// output straight into a character grid, so an infinity leaking out of any of
// these corrupts a frame.
type boundaryCase struct {
	name                     string
	highs, lows, closes, vol []float64
}

func boundaryCases() []boundaryCase {
	build := func(name string, n int, f func(i int) (h, l, c, v float64)) boundaryCase {
		bc := boundaryCase{name: name}
		for i := range n {
			h, l, c, v := f(i)
			bc.highs = append(bc.highs, h)
			bc.lows = append(bc.lows, l)
			bc.closes = append(bc.closes, c)
			bc.vol = append(bc.vol, v)
		}
		return bc
	}
	return []boundaryCase{
		{name: "empty"},
		build("single bar", 1, func(int) (float64, float64, float64, float64) {
			return 1, 1, 1, 1
		}),
		build("all zeros", 40, func(int) (float64, float64, float64, float64) {
			return 0, 0, 0, 0
		}),
		build("negative prices", 40, func(i int) (float64, float64, float64, float64) {
			return -100 + float64(i), -102 + float64(i), -101 + float64(i), 5
		}),
		build("denormal range", 40, func(i int) (float64, float64, float64, float64) {
			return 1e-300 * float64(i+1), 0, 5e-301 * float64(i+1), 1e-300
		}),
		build("near-overflow magnitudes", 40, func(i int) (float64, float64, float64, float64) {
			s := float64(i%2)*2 - 1
			return s * 1e307, -s * 1e307, s * 1e307, 1e307
		}),
		build("inverted bars", 40, func(i int) (float64, float64, float64, float64) {
			return float64(i), float64(i) + 10, float64(i) + 5, 1
		}),
		build("scattered NaN and Inf", 40, func(i int) (float64, float64, float64, float64) {
			switch i % 7 {
			case 3:
				return math.NaN(), math.NaN(), math.NaN(), math.NaN()
			case 5:
				return math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(1)
			}
			return 101 + float64(i), 99 + float64(i), 100 + float64(i), 10
		}),
	}
}

var boundaryPeriods = []int{-1, 0, 1, 2, 3, 14, 1000}

// TestIndicatorsNeverLeakInfinity sweeps every series indicator over
// degenerate inputs — too few points, flat and inverted ranges, zero and
// negative prices, denormal and near-overflow magnitudes, embedded NaN and
// Inf — and asserts that nothing panics, that every result keeps the input
// length, and that no infinity ever reaches a caller. NaN is the package's
// documented "undefined here" marker and is allowed.
func TestIndicatorsNeverLeakInfinity(t *testing.T) {
	for _, bc := range boundaryCases() {
		for _, period := range boundaryPeriods {
			n := len(bc.closes)

			check := func(label string, got []float64) {
				t.Helper()
				if len(got) != n {
					t.Fatalf("%s/period=%d/%s: length %d, want %d",
						bc.name, period, label, len(got), n)
				}
				for i, v := range got {
					if math.IsInf(v, 0) {
						t.Fatalf("%s/period=%d/%s: i=%d leaked %v",
							bc.name, period, label, i, v)
					}
				}
			}

			check("SMA", SMA(bc.closes, period))
			check("EMA", EMA(bc.closes, period))
			check("RSI", RSI(bc.closes, period))
			check("Stddev", Stddev(bc.closes, period))
			check("ATR", ATR(bc.highs, bc.lows, bc.closes, period))
			check("VWAP", VWAP(bc.highs, bc.lows, bc.closes, bc.vol))
			check("OBV", OBV(bc.closes, bc.vol))

			bb := Bollinger(bc.closes, period, 2.0)
			check("Bollinger.Upper", bb.Upper)
			check("Bollinger.Middle", bb.Middle)
			check("Bollinger.Lower", bb.Lower)

			k, d := Stochastic(bc.highs, bc.lows, bc.closes, period, 3)
			check("Stochastic.K", k)
			check("Stochastic.D", d)

			adx, plusDI, minusDI := ADX(bc.highs, bc.lows, bc.closes, period)
			check("ADX", adx)
			check("ADX.+DI", plusDI)
			check("ADX.-DI", minusDI)

			macd := MACD(bc.closes, period, period+12, 9)
			check("MACD.MACD", macd.MACD)
			check("MACD.Signal", macd.Signal)
			check("MACD.Histogram", macd.Histogram)
		}
	}
}

// TestBoundedIndicatorsStayInRange guards the indicators whose definitions
// pin them to a fixed interval. A value outside it would be plotted off-grid.
func TestBoundedIndicatorsStayInRange(t *testing.T) {
	inRange := func(t *testing.T, label string, got []float64) {
		t.Helper()
		for i, v := range got {
			if math.IsNaN(v) {
				continue
			}
			if v < 0 || v > 100 {
				t.Fatalf("%s: i=%d = %v outside [0, 100]", label, i, v)
			}
		}
	}
	for _, bc := range boundaryCases() {
		for _, period := range boundaryPeriods {
			inRange(t, bc.name+"/RSI", RSI(bc.closes, period))
			k, d := Stochastic(bc.highs, bc.lows, bc.closes, period, 3)
			inRange(t, bc.name+"/Stochastic.K", k)
			inRange(t, bc.name+"/Stochastic.D", d)
			adx, plusDI, minusDI := ADX(bc.highs, bc.lows, bc.closes, period)
			inRange(t, bc.name+"/ADX", adx)
			inRange(t, bc.name+"/ADX.+DI", plusDI)
			inRange(t, bc.name+"/ADX.-DI", minusDI)
		}
	}
}

// TestCandleIndicatorsNeverLeakInfinity covers the OHLCV-shaped indicators
// that the sweep above cannot express as parallel float slices.
func TestCandleIndicatorsNeverLeakInfinity(t *testing.T) {
	for _, bc := range boundaryCases() {
		candles := make([]provider.OHLCV, len(bc.closes))
		for i := range candles {
			candles[i] = provider.OHLCV{
				Open:   bc.closes[i],
				High:   bc.highs[i],
				Low:    bc.lows[i],
				Close:  bc.closes[i],
				Volume: bc.vol[i],
			}
		}

		if got := Patterns(candles); len(got) != len(candles) {
			t.Fatalf("%s: Patterns length %d, want %d", bc.name, len(got), len(candles))
		}

		for _, numBins := range []int{-1, 0, 1, 7, 500} {
			bins := VolumeProfile(candles, numBins)
			for i, b := range bins {
				if math.IsInf(b.PriceMin, 0) || math.IsInf(b.PriceMax, 0) ||
					math.IsNaN(b.PriceMin) || math.IsNaN(b.PriceMax) {
					t.Fatalf("%s/numBins=%d: bin %d bounds %v..%v",
						bc.name, numBins, i, b.PriceMin, b.PriceMax)
				}
				if math.IsInf(b.Volume, 0) || math.IsNaN(b.Volume) {
					t.Fatalf("%s/numBins=%d: bin %d volume %v",
						bc.name, numBins, i, b.Volume)
				}
			}
			if idx, vol := POC(bins); idx >= len(bins) || (idx < 0 && vol != 0) {
				t.Fatalf("%s/numBins=%d: POC returned idx=%d vol=%v",
					bc.name, numBins, idx, vol)
			}
		}
	}
}

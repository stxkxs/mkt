package indicator

import (
	"math"
	"testing"

	"github.com/stxkxs/mkt/internal/provider"
)

func TestVolumeProfile(t *testing.T) {
	t.Run("bin bounds cover the full price range", func(t *testing.T) {
		candles := []provider.OHLCV{
			{High: 110, Low: 90, Close: 100, Volume: 10},
			{High: 100, Low: 80, Close: 90, Volume: 20},
		}
		bins := VolumeProfile(candles, 4)
		if len(bins) != 4 {
			t.Fatalf("want 4 bins, got %d", len(bins))
		}
		if math.Abs(bins[0].PriceMin-80) > 1e-9 {
			t.Errorf("first bin should start at min low (80), got %v", bins[0].PriceMin)
		}
		if math.Abs(bins[3].PriceMax-110) > 1e-9 {
			t.Errorf("last bin should end at max high (110), got %v", bins[3].PriceMax)
		}
	})

	t.Run("total volume preserved", func(t *testing.T) {
		candles := []provider.OHLCV{
			{High: 110, Low: 90, Close: 100, Volume: 10},
			{High: 100, Low: 80, Close: 90, Volume: 20},
			{High: 105, Low: 95, Close: 100, Volume: 5},
		}
		bins := VolumeProfile(candles, 5)
		var total float64
		for _, b := range bins {
			total += b.Volume
		}
		if math.Abs(total-35) > 1e-9 {
			t.Errorf("total volume should be 35, got %v", total)
		}
	})

	t.Run("flat price range returns empty", func(t *testing.T) {
		candles := []provider.OHLCV{
			{High: 100, Low: 100, Close: 100, Volume: 10},
			{High: 100, Low: 100, Close: 100, Volume: 20},
		}
		bins := VolumeProfile(candles, 5)
		if len(bins) != 0 {
			t.Errorf("flat range should yield empty profile, got %d bins", len(bins))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		bins := VolumeProfile(nil, 5)
		if len(bins) != 0 {
			t.Errorf("want empty, got %d", len(bins))
		}
	})

	t.Run("zero numBins returns empty", func(t *testing.T) {
		candles := []provider.OHLCV{{High: 110, Low: 90, Close: 100, Volume: 10}}
		bins := VolumeProfile(candles, 0)
		if len(bins) != 0 {
			t.Errorf("zero numBins should yield empty, got %d", len(bins))
		}
	})

	t.Run("POC identifies highest-volume bin", func(t *testing.T) {
		bins := []VolumeBin{
			{PriceMin: 0, PriceMax: 10, Volume: 100},
			{PriceMin: 10, PriceMax: 20, Volume: 500},
			{PriceMin: 20, PriceMax: 30, Volume: 200},
		}
		idx, vol := POC(bins)
		if idx != 1 || vol != 500 {
			t.Errorf("POC: got idx=%d vol=%v, want idx=1 vol=500", idx, vol)
		}
	})

	t.Run("POC on empty returns -1", func(t *testing.T) {
		idx, vol := POC(nil)
		if idx != -1 || vol != 0 {
			t.Errorf("POC empty: got idx=%d vol=%v, want -1 0", idx, vol)
		}
	})

	t.Run("negative numBins returns empty", func(t *testing.T) {
		candles := []provider.OHLCV{{High: 110, Low: 90, Close: 100, Volume: 10}}
		if bins := VolumeProfile(candles, -3); len(bins) != 0 {
			t.Errorf("negative numBins should yield empty, got %d", len(bins))
		}
	})

	t.Run("non-finite candles are ignored, bin bounds stay finite", func(t *testing.T) {
		candles := []provider.OHLCV{
			{High: math.NaN(), Low: math.NaN(), Close: math.NaN(), Volume: 99},
			{High: 110, Low: 90, Close: 100, Volume: 10},
			{High: 100, Low: 80, Close: 90, Volume: 20},
			{High: math.Inf(1), Low: 0, Close: 50, Volume: 7},
		}
		bins := VolumeProfile(candles, 4)
		if len(bins) != 4 {
			t.Fatalf("want 4 bins, got %d", len(bins))
		}
		var total float64
		for i, b := range bins {
			if math.IsNaN(b.PriceMin) || math.IsInf(b.PriceMin, 0) ||
				math.IsNaN(b.PriceMax) || math.IsInf(b.PriceMax, 0) {
				t.Fatalf("bin %d has non-finite bounds: %v..%v", i, b.PriceMin, b.PriceMax)
			}
			total += b.Volume
		}
		// Only the two clean candles contribute.
		if math.Abs(total-30) > 1e-9 {
			t.Errorf("total volume should be 30, got %v", total)
		}
		if math.Abs(bins[0].PriceMin-80) > 1e-9 || math.Abs(bins[3].PriceMax-110) > 1e-9 {
			t.Errorf("range should span the finite candles only, got %v..%v",
				bins[0].PriceMin, bins[3].PriceMax)
		}
	})

	t.Run("all candles non-finite returns empty", func(t *testing.T) {
		candles := []provider.OHLCV{
			{High: math.NaN(), Low: math.NaN(), Close: math.NaN(), Volume: 1},
			{High: math.Inf(1), Low: math.Inf(-1), Close: 0, Volume: 1},
		}
		if bins := VolumeProfile(candles, 5); len(bins) != 0 {
			t.Errorf("want empty, got %d bins", len(bins))
		}
	})

	t.Run("POC ignores all-zero-volume bins", func(t *testing.T) {
		bins := []VolumeBin{{Volume: 0}, {Volume: 0}}
		idx, vol := POC(bins)
		if idx != -1 || vol != 0 {
			t.Errorf("got idx=%d vol=%v, want -1 0", idx, vol)
		}
	})
}

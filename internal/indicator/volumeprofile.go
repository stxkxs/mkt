package indicator

import "github.com/stxkxs/mkt/internal/provider"

// VolumeBin is one bucket of the volume profile.
type VolumeBin struct {
	PriceMin float64
	PriceMax float64
	Volume   float64
}

// VolumeProfile partitions the price range across candles into numBins
// equal-width buckets and accumulates each candle's volume into the
// bucket containing its typical price (H+L+C)/3. Returned bins are
// ordered low-to-high. Returns an empty slice for empty input, numBins
// <= 0, or a degenerate flat-price range.
func VolumeProfile(candles []provider.OHLCV, numBins int) []VolumeBin {
	if numBins <= 0 || len(candles) == 0 {
		return nil
	}

	minP, maxP := candles[0].Low, candles[0].High
	for _, c := range candles {
		if c.Low < minP {
			minP = c.Low
		}
		if c.High > maxP {
			maxP = c.High
		}
	}
	if maxP <= minP {
		return nil
	}

	binWidth := (maxP - minP) / float64(numBins)
	bins := make([]VolumeBin, numBins)
	for i := range bins {
		bins[i].PriceMin = minP + float64(i)*binWidth
		bins[i].PriceMax = bins[i].PriceMin + binWidth
	}

	for _, c := range candles {
		tp := (c.High + c.Low + c.Close) / 3
		idx := int((tp - minP) / binWidth)
		if idx >= numBins {
			idx = numBins - 1
		}
		if idx < 0 {
			idx = 0
		}
		bins[idx].Volume += c.Volume
	}
	return bins
}

// POC returns the index of the Point of Control — the bin with the
// highest volume — and its volume. Returns (-1, 0) for empty input.
func POC(bins []VolumeBin) (idx int, volume float64) {
	idx = -1
	for i, b := range bins {
		if b.Volume > volume {
			idx = i
			volume = b.Volume
		}
	}
	return idx, volume
}

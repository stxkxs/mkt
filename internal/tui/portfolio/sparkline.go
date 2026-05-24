package portfolio

import "strings"

var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// sparkline renders a tiny block-character sparkline for a slice of
// floats. width is the maximum number of cells; longer inputs are
// down-sampled by nearest-neighbor. Returns the empty string for
// fewer than two points.
func sparkline(values []float64, width int) string {
	if len(values) < 2 || width <= 0 {
		return ""
	}
	if len(values) > width {
		// nearest-neighbor downsample
		out := make([]float64, width)
		for i := range out {
			out[i] = values[i*len(values)/width]
		}
		values = out
	}
	minV, maxV := values[0], values[0]
	for _, v := range values {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	rng := maxV - minV
	var sb strings.Builder
	for _, v := range values {
		var idx int
		if rng > 0 {
			idx = int((v - minV) / rng * float64(len(sparkBlocks)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		sb.WriteRune(sparkBlocks[idx])
	}
	return sb.String()
}

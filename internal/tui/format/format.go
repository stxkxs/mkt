package format

import (
	"fmt"
	"strings"
)

// FormatPrice formats a price with appropriate precision.
func FormatPrice(price float64) string {
	switch {
	case price >= 100:
		return fmt.Sprintf("%.2f", price)
	case price >= 1:
		return fmt.Sprintf("%.4f", price)
	case price >= 0.01:
		return fmt.Sprintf("%.6f", price)
	default:
		return fmt.Sprintf("%.8f", price)
	}
}

// FormatAxisPrice formats a price for chart Y-axis labels (lower precision).
func FormatAxisPrice(price float64) string {
	switch {
	case price >= 10000:
		return fmt.Sprintf("%.0f", price)
	case price >= 100:
		return fmt.Sprintf("%.1f", price)
	case price >= 1:
		return fmt.Sprintf("%.2f", price)
	default:
		return fmt.Sprintf("%.4f", price)
	}
}

// FormatVolume formats volume with K/M/B suffixes.
func FormatVolume(vol float64) string {
	switch {
	case vol >= 1e9:
		return fmt.Sprintf("%.1fB", vol/1e9)
	case vol >= 1e6:
		return fmt.Sprintf("%.1fM", vol/1e6)
	case vol >= 1e3:
		return fmt.Sprintf("%.1fK", vol/1e3)
	default:
		return fmt.Sprintf("%.0f", vol)
	}
}

// DayRange renders a bar showing where price sits between low and high.
// Returns track string and marker index, so caller can style them independently.
// Uses ▓ for filled (left of marker), ░ for empty (right of marker), ● for position.
func DayRange(price, low, high float64, width int) (track string, markerIdx int) {
	if width <= 0 {
		return "", -1
	}
	if high == low || high == 0 || low == 0 {
		return fmt.Sprintf("%-*s", width, "—"), -1
	}
	pos := (price - low) / (high - low)
	if pos < 0 {
		pos = 0
	}
	if pos > 1 {
		pos = 1
	}
	idx := int(pos * float64(width-1))
	var sb strings.Builder
	for i := range width {
		if i == idx {
			sb.WriteRune('●')
		} else if i < idx {
			sb.WriteRune('▓')
		} else {
			sb.WriteRune('░')
		}
	}
	return sb.String(), idx
}

// Sparkline renders a mini sparkline from price data using Unicode block characters.
// The caller is responsible for padding the result if needed.
func Sparkline(prices []float64, width int) string {
	if len(prices) == 0 {
		return ""
	}

	if len(prices) > width {
		prices = prices[len(prices)-width:]
	}

	minP, maxP := prices[0], prices[0]
	for _, p := range prices {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}

	blocks := []rune("▁▂▃▄▅▆▇█")
	rng := maxP - minP
	if rng == 0 {
		rng = 1
	}

	var sb strings.Builder
	for _, p := range prices {
		idx := int((p - minP) / rng * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
}

// BrailleSparkline renders a sparkline using braille characters for higher resolution.
// Each output character encodes two data points (left/right column) with 4 vertical dot rows,
// producing an area-chart effect with 2x horizontal and 4x vertical resolution.
func BrailleSparkline(prices []float64, width int) string {
	if len(prices) == 0 || width <= 0 {
		return ""
	}

	// Resample prices to width*2 data points
	needed := width * 2
	sampled := resample(prices, needed)

	// Find min/max
	minP, maxP := sampled[0], sampled[0]
	for _, p := range sampled {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}

	rng := maxP - minP
	if rng == 0 {
		rng = 1
	}

	// Braille dot positions (bottom-up: row 3=bottom, row 0=top)
	// Left column dots:  row3=0x40, row2=0x04, row1=0x02, row0=0x01
	// Right column dots: row3=0x80, row2=0x20, row1=0x10, row0=0x08
	leftDots := [4]rune{0x40, 0x04, 0x02, 0x01}
	rightDots := [4]rune{0x80, 0x20, 0x10, 0x08}

	var sb strings.Builder
	for i := 0; i < needed; i += 2 {
		// Normalize to 0..3 range (row index, 0=top, 3=bottom)
		leftVal := int((sampled[i] - minP) / rng * 3.0)
		rightVal := int((sampled[i+1] - minP) / rng * 3.0)
		if leftVal > 3 {
			leftVal = 3
		}
		if rightVal > 3 {
			rightVal = 3
		}

		// Fill dots from bottom (row 3) up to the value level — area chart
		var code rune = 0x2800
		for row := 3; row >= 3-leftVal; row-- {
			code |= leftDots[row]
		}
		for row := 3; row >= 3-rightVal; row-- {
			code |= rightDots[row]
		}
		sb.WriteRune(code)
	}
	return sb.String()
}

// resample interpolates prices to exactly n data points.
func resample(prices []float64, n int) []float64 {
	if len(prices) == 0 || n <= 0 {
		return nil
	}
	if len(prices) == 1 {
		out := make([]float64, n)
		for i := range out {
			out[i] = prices[0]
		}
		return out
	}
	if len(prices) >= n {
		// Downsample: pick evenly spaced points
		out := make([]float64, n)
		for i := range n {
			idx := float64(i) * float64(len(prices)-1) / float64(n-1)
			lo := int(idx)
			hi := lo + 1
			if hi >= len(prices) {
				hi = len(prices) - 1
			}
			frac := idx - float64(lo)
			out[i] = prices[lo]*(1-frac) + prices[hi]*frac
		}
		return out
	}
	// Upsample: linear interpolation
	out := make([]float64, n)
	for i := range n {
		idx := float64(i) * float64(len(prices)-1) / float64(n-1)
		lo := int(idx)
		hi := lo + 1
		if hi >= len(prices) {
			hi = len(prices) - 1
		}
		frac := idx - float64(lo)
		out[i] = prices[lo]*(1-frac) + prices[hi]*frac
	}
	return out
}

// BrailleSpinner frames for animated loading indicators.
var BrailleSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerFrame returns the spinner character for a given tick.
func SpinnerFrame(tick int) string {
	return BrailleSpinner[tick%len(BrailleSpinner)]
}

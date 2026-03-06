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

package watchlist

import (
	"fmt"
	"strings"
)

// sparkline renders a mini sparkline from price data.
// Uses Unicode block characters: ▁▂▃▄▅▆▇█
func sparkline(prices []float64, width int) string {
	if len(prices) == 0 {
		return strings.Repeat(" ", width)
	}

	// Use last `width` prices
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

	// Pad if needed
	for sb.Len() < width {
		sb.WriteRune(' ')
	}
	return sb.String()
}

// formatPrice formats a price with appropriate precision.
func formatPrice(price float64) string {
	switch {
	case price >= 10000:
		return fmt.Sprintf("%.2f", price)
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

// formatVolume formats volume with K/M/B suffixes.
func formatVolume(vol float64) string {
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

package theme

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

const heatmapSteps = 9

var heatmapGradient [heatmapSteps]color.Color

// HeatmapColor returns the gradient color for a change-percentage value,
// interpolating between the current theme's red / neutral / green at roughly ±5%.
// Out-of-range inputs clamp to the gradient endpoints.
func HeatmapColor(pct float64) color.Color {
	// Map [-5, +5] onto [0, heatmapSteps-1].
	idx := int((pct + 5) / 10 * float64(heatmapSteps-1))
	if idx < 0 {
		idx = 0
	}
	if idx > heatmapSteps-1 {
		idx = heatmapSteps - 1
	}
	return heatmapGradient[idx]
}

func rebuildHeatmapGradient(redHex, neutralHex, greenHex string) {
	r := parseHex(redHex)
	n := parseHex(neutralHex)
	g := parseHex(greenHex)
	for i := 0; i < heatmapSteps; i++ {
		t := float64(i) / float64(heatmapSteps-1)
		var c rgb
		if t < 0.5 {
			c = lerp(r, n, t*2)
		} else {
			c = lerp(n, g, (t-0.5)*2)
		}
		heatmapGradient[i] = lipgloss.Color(c.hex())
	}
}

type rgb struct{ r, g, b int }

func parseHex(s string) rgb {
	var c rgb
	_, _ = fmt.Sscanf(s, "#%02x%02x%02x", &c.r, &c.g, &c.b)
	return c
}

func lerp(a, b rgb, t float64) rgb {
	return rgb{
		r: int(float64(a.r) + float64(b.r-a.r)*t),
		g: int(float64(a.g) + float64(b.g-a.g)*t),
		b: int(float64(a.b) + float64(b.b-a.b)*t),
	}
}

func (c rgb) hex() string {
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

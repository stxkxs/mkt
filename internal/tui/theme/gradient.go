package theme

import (
	"fmt"
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
)

const heatmapSteps = 9

var heatmapGradient [heatmapSteps]color.Color

// heatmapSpan is the change percentage that reaches either end of the
// gradient; anything further out clamps to the end.
const heatmapSpan = 5.0

// HeatmapColor returns the gradient color for a change-percentage value,
// interpolating between the current theme's red / neutral / green across
// ±heatmapSpan. Out-of-range inputs clamp to the gradient endpoints, and NaN
// reads as neutral.
//
// The step is chosen from the magnitude and then signed, so a move and its
// opposite land the same number of steps either side of neutral: moves
// smaller than 0.625% either way share the neutral tile.
func HeatmapColor(pct float64) color.Color {
	return heatmapGradient[(heatmapSteps-1)/2+heatmapStep(pct)]
}

// heatmapStep is how many gradient steps pct sits from neutral, negative
// for a fall.
func heatmapStep(pct float64) int {
	const mid = (heatmapSteps - 1) / 2
	if math.IsNaN(pct) {
		return 0
	}
	// Clamped before the conversion: int(+Inf) is not defined, and amd64
	// makes it the most negative int.
	step := int(math.Min(math.Round(math.Abs(pct)/heatmapSpan*mid), mid))
	if pct < 0 {
		return -step
	}
	return step
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

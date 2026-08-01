package chart

import (
	"image/color"

	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// drawOverlays paints the on-chart indicator series (moving averages,
// Bollinger bands, VWAP, pivot lines) directly onto the candlestick or
// line grid.
//
// Every series comes from the viewport, which holds values computed over
// the full fetched history and narrowed to the visible window — the
// overlay never recomputes anything from what happens to be on screen.
// Overlays are confined to chartW so they do not run into the
// volume-profile gutter, and they only paint blank cells so candles and
// pattern markers keep priority.
func (m Model) drawOverlays(p *panel, v viewport, scale vscale, chartW int) {
	limit := min(chartW, p.w)
	if limit <= 0 {
		return
	}
	plot := func(values []float64, clr color.Color) {
		plotSeries(p, values, v.step, scale, '─', clr, paintIfEmpty)
	}

	if m.indicators[IndSMA] {
		plot(v.ind.sma, theme.ColorCyan)
	}
	if m.indicators[IndEMA] {
		plot(v.ind.ema, theme.ColorYellow)
	}
	if m.indicators[IndBollinger] {
		plot(v.ind.bb.Upper, theme.ColorDim)
		plot(v.ind.bb.Middle, theme.ColorAccent)
		plot(v.ind.bb.Lower, theme.ColorDim)
	}
	if m.indicators[IndVWAP] {
		plot(v.ind.vwap, theme.ColorMagenta)
	}
	if m.indicators[IndPivots] && v.ind.hasPivots {
		piv := v.ind.pivots
		// A pivot outside the visible price range is not drawn at all,
		// rather than smeared across the top or bottom row.
		plotHLine := func(val float64, clr color.Color) {
			if val < scale.minV || val > scale.maxV {
				return
			}
			row, ok := scale.row(val)
			if !ok {
				return
			}
			for c := range limit {
				p.paint(row, c, refLine, clr, paintIfEmpty)
			}
		}
		plotHLine(piv.R3, theme.ColorGreen)
		plotHLine(piv.R2, theme.ColorGreen)
		plotHLine(piv.R1, theme.ColorGreen)
		plotHLine(piv.P, theme.ColorAccent)
		plotHLine(piv.S1, theme.ColorRed)
		plotHLine(piv.S2, theme.ColorRed)
		plotHLine(piv.S3, theme.ColorRed)
	}
}

// drawPatternMarkers paints a glyph above or below each candle whose
// pattern was detected. Bullish patterns mark below the low (▲ green),
// bearish above the high (▼ red), and doji above the high (◇ accent).
func drawPatternMarkers(p *panel, v viewport, scale vscale, chartW int) {
	for i, pat := range v.ind.patterns {
		if pat == indicator.PatternNone || i >= v.len() {
			continue
		}
		col := i * v.step
		if col >= chartW {
			break
		}
		var glyph rune
		var clr color.Color
		var row int
		var ok bool
		switch {
		case pat.IsBullish():
			glyph, clr = '▲', theme.ColorGreen
			row, ok = scale.row(v.candles[i].Low)
			row++
		case pat.IsBearish():
			glyph, clr = '▼', theme.ColorRed
			row, ok = scale.row(v.candles[i].High)
			row--
		default: // Doji
			glyph, clr = '◇', theme.ColorAccent
			row, ok = scale.row(v.candles[i].High)
			row--
		}
		if !ok {
			continue
		}
		p.paint(clampRow(row, p.h), col, glyph, clr, paintIfEmpty)
	}
}

// volumeBins computes the volume profile for the visible candles.
//
// Both the gutter histogram and the header's POC readout call this with
// the same arguments. They used to bin differently — the header used one
// bin per candle, which degenerates into "the typical price of the
// single highest-volume candle" — and so pointed at different prices.
func volumeBins(candles []provider.OHLCV, height int) []indicator.VolumeBin {
	if height < 1 {
		return nil
	}
	return indicator.VolumeProfile(candles, height)
}

// drawVolumeProfileGutter paints a horizontal volume histogram into the
// rightmost columns of the grid. Bins are computed at chart height
// resolution so each row maps to one bin (lowest price at the bottom row).
func drawVolumeProfileGutter(p *panel, candles []provider.OHLCV, chartW, totalW, height int) {
	bins := volumeBins(candles, height)
	if len(bins) == 0 {
		return
	}
	var maxVol float64
	for _, b := range bins {
		if b.Volume > maxVol {
			maxVol = b.Volume
		}
	}
	if maxVol <= 0 {
		return
	}
	pocIdx, _ := indicator.POC(bins)
	gutterW := totalW - chartW
	if gutterW <= 0 {
		return
	}
	for i, b := range bins {
		row := height - 1 - i
		if row < 0 || row >= height {
			continue
		}
		barLen := int(b.Volume / maxVol * float64(gutterW))
		if b.Volume > 0 && barLen < 1 {
			barLen = 1
		}
		clr := theme.ColorDim
		if i == pocIdx {
			clr = theme.ColorAccent
		}
		for c := chartW; c < chartW+barLen && c < totalW; c++ {
			p.paint(row, c, '▆', clr, paintOver)
		}
	}
}

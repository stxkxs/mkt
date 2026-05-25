package chart

import (
	"image/color"
	"math"

	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// drawOverlays paints the on-chart indicator series (moving averages,
// Bollinger bands, VWAP, pivot lines) directly onto the candlestick or
// line chart grid. Each overlay respects the chart area width and skips
// cells already occupied by candles or higher-priority glyphs.
func (m Model) drawOverlays(grid [][]rune, gridColor [][]color.Color, candles []provider.OHLCV, closes []float64, width, height int, minP, scale float64, step int) {
	plotLine := func(values []float64, clr color.Color) {
		for i, v := range values {
			if math.IsNaN(v) {
				continue
			}
			col := i * step
			if step == 1 && len(values) > width {
				col = i * width / len(values)
			} else if step > 1 {
				col = i * step
			}
			if col >= width {
				break
			}
			row := height - 1 - int((v-minP)*scale)
			row = clampRow(row, height)
			if grid[row][col] == ' ' {
				grid[row][col] = '─'
				gridColor[row][col] = clr
			}
		}
	}

	if m.indicators[IndSMA] {
		sma := indicator.SMA(closes, 20)
		plotLine(sma, theme.ColorCyan)
	}
	if m.indicators[IndEMA] {
		ema := indicator.EMA(closes, 20)
		plotLine(ema, theme.ColorYellow)
	}
	if m.indicators[IndBollinger] {
		bb := indicator.Bollinger(closes, 20, 2.0)
		plotLine(bb.Upper, theme.ColorDim)
		plotLine(bb.Middle, theme.ColorAccent)
		plotLine(bb.Lower, theme.ColorDim)
	}
	if m.indicators[IndVWAP] {
		highs := make([]float64, len(candles))
		lows := make([]float64, len(candles))
		vols := make([]float64, len(candles))
		for i, c := range candles {
			highs[i] = c.High
			lows[i] = c.Low
			vols[i] = c.Volume
		}
		vwap := indicator.VWAP(highs, lows, closes, vols)
		plotLine(vwap, theme.ColorMagenta)
	}
	if m.indicators[IndPivots] && len(candles) >= 2 {
		prev := candles[len(candles)-2]
		piv := indicator.PivotsClassic(prev.High, prev.Low, prev.Close)
		plotHLine := func(v float64, clr color.Color) {
			row := height - 1 - int((v-minP)*scale)
			if row < 0 || row >= height {
				return
			}
			for c := range width {
				if grid[row][c] == ' ' {
					grid[row][c] = '┄'
					gridColor[row][c] = clr
				}
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
func drawPatternMarkers(grid [][]rune, gridColor [][]color.Color, candles []provider.OHLCV, chartW, height int, minP, scale float64, candleWidth int) {
	pats := indicator.Patterns(candles)
	for i, p := range pats {
		if p == indicator.PatternNone {
			continue
		}
		col := i * candleWidth
		if col >= chartW {
			break
		}
		var glyph rune
		var clr color.Color
		var row int
		switch {
		case p.IsBullish():
			glyph = '▲'
			clr = theme.ColorGreen
			lowRow := height - 1 - int((candles[i].Low-minP)*scale)
			row = clampRow(lowRow+1, height)
		case p.IsBearish():
			glyph = '▼'
			clr = theme.ColorRed
			highRow := height - 1 - int((candles[i].High-minP)*scale)
			row = clampRow(highRow-1, height)
		default: // Doji
			glyph = '◇'
			clr = theme.ColorAccent
			highRow := height - 1 - int((candles[i].High-minP)*scale)
			row = clampRow(highRow-1, height)
		}
		if grid[row][col] == ' ' {
			grid[row][col] = glyph
			gridColor[row][col] = clr
		}
	}
}

// drawVolumeProfileGutter paints a horizontal volume histogram into the
// rightmost columns of the grid. Bins are computed at chart height
// resolution so each row maps to one bin (lowest price at the bottom row).
func drawVolumeProfileGutter(grid [][]rune, gridColor [][]color.Color, candles []provider.OHLCV, chartW, totalW, height int) {
	bins := indicator.VolumeProfile(candles, height)
	if len(bins) == 0 {
		return
	}
	var maxVol float64
	for _, b := range bins {
		if b.Volume > maxVol {
			maxVol = b.Volume
		}
	}
	if maxVol == 0 {
		return
	}
	pocIdx, _ := indicator.POC(bins)
	gutterW := totalW - chartW
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
			grid[row][c] = '▆'
			gridColor[row][c] = clr
		}
	}
}

// extractHL pulls high and low slices out of a candle series.
func extractHL(candles []provider.OHLCV) (highs, lows []float64) {
	highs = make([]float64, len(candles))
	lows = make([]float64, len(candles))
	for i, c := range candles {
		highs[i] = c.High
		lows[i] = c.Low
	}
	return highs, lows
}

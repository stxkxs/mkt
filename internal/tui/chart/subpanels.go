package chart

import (
	"math"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// Sub-panel renderers — each draws one indicator into a fresh panel the
// height of the supplied area. Returned strings include their own header
// line so the caller can stack them under the main chart.
//
// Every one of them reads its series off the viewport (computed over the
// full fetched history, narrowed to the visible window) and plots at the
// viewport's candle step, so the sub-panel describes exactly the bars
// drawn above it and lines up with them column for column.

// oscillatorScale is the fixed 0..100 scale shared by RSI, Stochastic
// and ADX.
func oscillatorScale(height int) vscale {
	return newPanelScale(0, 100, height)
}

// levelLabel builds a row-label function that prints a caption on the
// rows carrying the given reference levels.
func levelLabel(levels map[int]string) func(int) string {
	return func(r int) string {
		return levels[r]
	}
}

func renderRSI(v viewport, width, height int) string {
	if height < 3 || width <= 0 {
		return ""
	}
	scale := oscillatorScale(height)
	p := newPanel(width, height)

	// Reference lines at 30 and 70
	row30, _ := scale.row(30)
	row70, _ := scale.row(70)
	p.hline(row30, refLine, theme.ColorDim, paintOver)
	p.hline(row70, refLine, theme.ColorDim, paintOver)

	plotSeries(p, v.ind.rsi, v.step, scale, '●', theme.ColorMagenta, paintOver)

	out := panelTitle(lipgloss.NewStyle().Foreground(theme.ColorMagenta).Bold(true).Render("RSI(14)"))
	return out + p.render(levelLabel(map[int]string{row70: "70", row30: "30"}))
}

func renderMACD(v viewport, width, height int) string {
	if height < 3 || width <= 0 {
		return ""
	}
	macd := v.ind.macd

	// Range spans the MACD line and the histogram, always including zero.
	minV, maxV := 0.0, 0.0
	for _, series := range [][]float64{macd.MACD, macd.Histogram} {
		for _, val := range series {
			if math.IsNaN(val) {
				continue
			}
			if val < minV {
				minV = val
			}
			if val > maxV {
				maxV = val
			}
		}
	}
	scale := newPanelScale(minV, maxV, height)
	p := newPanel(width, height)

	// Zero line
	zeroRow, _ := scale.row(0)
	p.hline(zeroRow, refLine, theme.ColorDim, paintOver)

	// Histogram bars, drawn from the zero line out to the value.
	for i, val := range macd.Histogram {
		col := i * v.step
		if col >= width {
			break
		}
		row, ok := scale.row(val)
		if !ok {
			continue
		}
		clr := theme.ColorGreen
		if val < 0 {
			clr = theme.ColorRed
		}
		if row < zeroRow {
			for r := row; r < zeroRow; r++ {
				p.paint(r, col, '▮', clr, paintOver)
			}
		} else {
			for r := zeroRow + 1; r <= row; r++ {
				p.paint(r, col, '▮', clr, paintOver)
			}
		}
	}

	plotSeries(p, macd.MACD, v.step, scale, '●', theme.ColorAccent, paintOver)
	plotSeries(p, macd.Signal, v.step, scale, '○', theme.ColorYellow, paintIfEmpty)

	out := panelTitle(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("MACD(12,26,9)"))
	return out + p.render(nil)
}

func renderADX(v viewport, width, height int) string {
	if height < 3 || width <= 0 || v.len() == 0 {
		return ""
	}
	scale := oscillatorScale(height)
	p := newPanel(width, height)

	// Reference line at 25 (conventional trending threshold)
	row25, _ := scale.row(25)
	p.hline(row25, refLine, theme.ColorDim, paintOver)

	plotSeries(p, v.ind.plusDI, v.step, scale, '+', theme.ColorGreen, paintIfClear)
	plotSeries(p, v.ind.minusDI, v.step, scale, '-', theme.ColorRed, paintIfClear)
	plotSeries(p, v.ind.adx, v.step, scale, '●', theme.ColorAccent, paintIfClear)

	title := lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("ADX(14) ") +
		lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("+DI ") +
		lipgloss.NewStyle().Foreground(theme.ColorRed).Render("-DI")
	return panelTitle(title) + p.render(levelLabel(map[int]string{row25: "25"}))
}

func renderATR(v viewport, width, height int) string {
	if height < 3 || width <= 0 || v.len() == 0 {
		return ""
	}
	minV, maxV, ok := finiteRange(v.ind.atr)
	if !ok {
		return ""
	}
	scale := newPanelScale(minV, maxV, height)
	p := newPanel(width, height)
	plotSeries(p, v.ind.atr, v.step, scale, '●', theme.ColorAccent, paintOver)

	out := panelTitle(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("ATR(14)"))
	return out + p.render(nil)
}

func renderStoch(v viewport, width, height int) string {
	if height < 3 || width <= 0 || v.len() == 0 {
		return ""
	}
	scale := oscillatorScale(height)
	p := newPanel(width, height)

	// Reference lines at 20 and 80
	row20, _ := scale.row(20)
	row80, _ := scale.row(80)
	p.hline(row20, refLine, theme.ColorDim, paintOver)
	p.hline(row80, refLine, theme.ColorDim, paintOver)

	plotSeries(p, v.ind.stochK, v.step, scale, '●', theme.ColorAccent, paintIfClear)
	plotSeries(p, v.ind.stochD, v.step, scale, '○', theme.ColorYellow, paintIfClear)

	out := panelTitle(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("Stochastic"))
	return out + p.render(levelLabel(map[int]string{row80: "80", row20: "20"}))
}

func renderOBV(v viewport, width, height int) string {
	if height < 3 || width <= 0 || v.len() == 0 {
		return ""
	}
	minV, maxV, ok := finiteRange(v.ind.obv)
	if !ok {
		return ""
	}
	scale := newPanelScale(minV, maxV, height)
	p := newPanel(width, height)

	// Zero reference line if it falls inside the range
	if minV < 0 && maxV > 0 {
		if zeroRow, ok := scale.row(0); ok {
			p.hline(zeroRow, refLine, theme.ColorDim, paintOver)
		}
	}
	plotSeries(p, v.ind.obv, v.step, scale, '●', theme.ColorAccent, paintOver)

	out := panelTitle(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("OBV"))
	return out + p.render(nil)
}

// finiteRange is the min and max of the finite samples in a series.
// ok is false when the series carries no plottable value, in which case
// the sub-panel renders nothing rather than an empty axis.
func finiteRange(series []float64) (minV, maxV float64, ok bool) {
	minV, maxV = math.Inf(1), math.Inf(-1)
	for _, v := range series {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
		ok = true
	}
	if !ok {
		return 0, 0, false
	}
	return minV, maxV, true
}

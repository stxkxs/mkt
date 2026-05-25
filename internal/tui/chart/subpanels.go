package chart

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// Sub-panel renderers — each draws one indicator into a fresh grid the
// height of the supplied panel area. Returned strings include their
// own header line so the caller can stack them under the main chart.

func (m Model) renderRSI(closes []float64, width, height int) string {
	if height < 3 || width <= 0 {
		return ""
	}
	rsi := indicator.RSI(closes, 14)

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	// Reference lines at 30 and 70
	row30 := height - 1 - int(30.0/100.0*float64(height-1))
	row70 := height - 1 - int(70.0/100.0*float64(height-1))
	row30 = clampRow(row30, height)
	row70 = clampRow(row70, height)
	for c := range width {
		grid[row30][c] = '┄'
		gridColor[row30][c] = theme.ColorDim
		grid[row70][c] = '┄'
		gridColor[row70][c] = theme.ColorDim
	}

	// Plot RSI
	for i, v := range rsi {
		if math.IsNaN(v) {
			continue
		}
		col := i
		if len(rsi) > width {
			col = i * width / len(rsi)
		}
		if col >= width {
			break
		}
		row := height - 1 - int(v/100.0*float64(height-1))
		row = clampRow(row, height)
		grid[row][col] = '●'
		gridColor[row][col] = theme.ColorMagenta
	}

	// Render
	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorMagenta).Bold(true).Render("RSI(14)"))
	sb.WriteString("\n")
	for r := range height {
		if r == row70 {
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, "70")))
		} else if r == row30 {
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, "30")))
		} else {
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderMACD(closes []float64, width, height int) string {
	if height < 3 || width <= 0 {
		return ""
	}
	macd := indicator.MACD(closes, 12, 26, 9)

	// Find range
	minV, maxV := 0.0, 0.0
	for i := range closes {
		if !math.IsNaN(macd.MACD[i]) {
			if macd.MACD[i] < minV {
				minV = macd.MACD[i]
			}
			if macd.MACD[i] > maxV {
				maxV = macd.MACD[i]
			}
		}
		if !math.IsNaN(macd.Histogram[i]) {
			if macd.Histogram[i] < minV {
				minV = macd.Histogram[i]
			}
			if macd.Histogram[i] > maxV {
				maxV = macd.Histogram[i]
			}
		}
	}
	rng := maxV - minV
	if rng == 0 {
		rng = 1
	}

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	// Zero line
	zeroRow := height - 1 - int((0-minV)/rng*float64(height-1))
	zeroRow = clampRow(zeroRow, height)
	for c := range width {
		grid[zeroRow][c] = '┄'
		gridColor[zeroRow][c] = theme.ColorDim
	}

	// Histogram bars
	for i := range closes {
		if math.IsNaN(macd.Histogram[i]) {
			continue
		}
		col := i
		if len(closes) > width {
			col = i * width / len(closes)
		}
		if col >= width {
			break
		}
		v := macd.Histogram[i]
		row := height - 1 - int((v-minV)/rng*float64(height-1))
		row = clampRow(row, height)

		color := theme.ColorGreen
		if v < 0 {
			color = theme.ColorRed
		}

		if row < zeroRow {
			for r := row; r < zeroRow; r++ {
				grid[r][col] = '▮'
				gridColor[r][col] = color
			}
		} else {
			for r := zeroRow + 1; r <= row; r++ {
				grid[r][col] = '▮'
				gridColor[r][col] = color
			}
		}
	}

	// MACD line
	for i := range closes {
		if math.IsNaN(macd.MACD[i]) {
			continue
		}
		col := i
		if len(closes) > width {
			col = i * width / len(closes)
		}
		if col >= width {
			break
		}
		row := height - 1 - int((macd.MACD[i]-minV)/rng*float64(height-1))
		row = clampRow(row, height)
		grid[row][col] = '●'
		gridColor[row][col] = theme.ColorAccent
	}

	// Signal line
	for i := range closes {
		if math.IsNaN(macd.Signal[i]) {
			continue
		}
		col := i
		if len(closes) > width {
			col = i * width / len(closes)
		}
		if col >= width {
			break
		}
		row := height - 1 - int((macd.Signal[i]-minV)/rng*float64(height-1))
		row = clampRow(row, height)
		if grid[row][col] == ' ' {
			grid[row][col] = '○'
			gridColor[row][col] = theme.ColorYellow
		}
	}

	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("MACD(12,26,9)"))
	sb.WriteString("\n")
	for r := range height {
		sb.WriteString(strings.Repeat(" ", labelWidth+1))
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderADX(highs, lows, closes []float64, width, height int) string {
	if height < 3 || width <= 0 || len(closes) == 0 {
		return ""
	}
	adx, plusDI, minusDI := indicator.ADX(highs, lows, closes, 14)

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	// Reference line at 25 (conventional trending threshold)
	row25 := height - 1 - int(25.0/100.0*float64(height-1))
	row25 = clampRow(row25, height)
	for c := range width {
		grid[row25][c] = '┄'
		gridColor[row25][c] = theme.ColorDim
	}

	plotSeries := func(series []float64, clr color.Color, glyph rune) {
		for i, v := range series {
			if math.IsNaN(v) {
				continue
			}
			col := i
			if len(series) > width {
				col = i * width / len(series)
			}
			if col >= width {
				break
			}
			row := height - 1 - int(v/100.0*float64(height-1))
			row = clampRow(row, height)
			if grid[row][col] == ' ' || grid[row][col] == '┄' {
				grid[row][col] = glyph
				gridColor[row][col] = clr
			}
		}
	}
	plotSeries(plusDI, theme.ColorGreen, '+')
	plotSeries(minusDI, theme.ColorRed, '-')
	plotSeries(adx, theme.ColorAccent, '●')

	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("ADX(14) "))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorGreen).Render("+DI "))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorRed).Render("-DI"))
	sb.WriteString("\n")
	for r := range height {
		if r == row25 {
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, "25")))
		} else {
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderATR(highs, lows, closes []float64, width, height int) string {
	if height < 3 || width <= 0 || len(closes) == 0 {
		return ""
	}
	atr := indicator.ATR(highs, lows, closes, 14)

	// Range over non-NaN ATR values
	minV, maxV := math.Inf(1), math.Inf(-1)
	for _, v := range atr {
		if math.IsNaN(v) {
			continue
		}
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	if math.IsInf(minV, 1) {
		return ""
	}
	rng := maxV - minV
	if rng == 0 {
		rng = 1
	}

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	for i, v := range atr {
		if math.IsNaN(v) {
			continue
		}
		col := i
		if len(atr) > width {
			col = i * width / len(atr)
		}
		if col >= width {
			break
		}
		row := height - 1 - int((v-minV)/rng*float64(height-1))
		row = clampRow(row, height)
		grid[row][col] = '●'
		gridColor[row][col] = theme.ColorAccent
	}

	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("ATR(14)"))
	sb.WriteString("\n")
	for r := range height {
		sb.WriteString(strings.Repeat(" ", labelWidth+1))
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderStoch(highs, lows, closes []float64, width, height int) string {
	if height < 3 || width <= 0 || len(closes) == 0 {
		return ""
	}
	k, d := indicator.Stochastic(highs, lows, closes, 14, 3)

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	// Reference lines at 20 and 80
	row20 := height - 1 - int(20.0/100.0*float64(height-1))
	row80 := height - 1 - int(80.0/100.0*float64(height-1))
	row20 = clampRow(row20, height)
	row80 = clampRow(row80, height)
	for c := range width {
		grid[row20][c] = '┄'
		gridColor[row20][c] = theme.ColorDim
		grid[row80][c] = '┄'
		gridColor[row80][c] = theme.ColorDim
	}

	plotSeries := func(series []float64, clr color.Color, glyph rune) {
		for i, v := range series {
			if math.IsNaN(v) {
				continue
			}
			col := i
			if len(series) > width {
				col = i * width / len(series)
			}
			if col >= width {
				break
			}
			row := height - 1 - int(v/100.0*float64(height-1))
			row = clampRow(row, height)
			if grid[row][col] == ' ' || grid[row][col] == '┄' {
				grid[row][col] = glyph
				gridColor[row][col] = clr
			}
		}
	}
	plotSeries(k, theme.ColorAccent, '●')
	plotSeries(d, theme.ColorYellow, '○')

	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("Stochastic"))
	sb.WriteString("\n")
	for r := range height {
		switch r {
		case row80:
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, "80")))
		case row20:
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, "20")))
		default:
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderOBV(closes, volumes []float64, width, height int) string {
	if height < 3 || width <= 0 || len(closes) == 0 {
		return ""
	}
	obv := indicator.OBV(closes, volumes)

	// Find OBV range
	minV, maxV := obv[0], obv[0]
	for _, v := range obv {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	rng := maxV - minV
	if rng == 0 {
		rng = 1
	}

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for r := range height {
		grid[r] = make([]rune, width)
		gridColor[r] = make([]color.Color, width)
		for c := range width {
			grid[r][c] = ' '
		}
	}

	// Zero reference line if it falls inside the range
	if minV < 0 && maxV > 0 {
		zeroRow := height - 1 - int((0-minV)/rng*float64(height-1))
		zeroRow = clampRow(zeroRow, height)
		for c := range width {
			grid[zeroRow][c] = '┄'
			gridColor[zeroRow][c] = theme.ColorDim
		}
	}

	// Plot OBV line
	for i, v := range obv {
		col := i
		if len(obv) > width {
			col = i * width / len(obv)
		}
		if col >= width {
			break
		}
		row := height - 1 - int((v-minV)/rng*float64(height-1))
		row = clampRow(row, height)
		grid[row][col] = '●'
		gridColor[row][col] = theme.ColorAccent
	}

	var sb strings.Builder
	labelWidth := 10
	sb.WriteString(strings.Repeat(" ", labelWidth+1))
	sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("OBV"))
	sb.WriteString("\n")
	for r := range height {
		sb.WriteString(strings.Repeat(" ", labelWidth+1))
		for c := range grid[r] {
			ch := grid[r][c]
			clr := gridColor[r][c]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if clr != nil {
				sb.WriteString(lipgloss.NewStyle().Foreground(clr).Render(string(ch)))
			} else {
				sb.WriteRune(ch)
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

package chart

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/indicator"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleAxis  = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleInfo  = lipgloss.NewStyle().Foreground(theme.ColorCyan)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleAxis = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleInfo = lipgloss.NewStyle().Foreground(theme.ColorCyan)
}

// IndicatorType identifies a technical indicator.
type IndicatorType int

const (
	IndSMA IndicatorType = iota
	IndEMA
	IndBollinger
	IndRSI
	IndMACD
	indCount
)

var indicatorNames = []string{"SMA(20)", "EMA(20)", "Bollinger", "RSI(14)", "MACD"}

// ChartMode determines the chart type.
type ChartMode int

const (
	ModeCandlestick ChartMode = iota
	ModeLine
)

var intervals = []provider.Interval{
	provider.Interval1m,
	provider.Interval5m,
	provider.Interval15m,
	provider.Interval1h,
	provider.Interval4h,
	provider.Interval1d,
	provider.Interval1w,
}

// HistoryProvider is the interface for fetching history.
type HistoryProvider interface {
	History(ctx context.Context, params provider.HistoryParams) ([]provider.OHLCV, error)
}

// Model is the full-screen chart view.
type Model struct {
	symbol        string
	data          []provider.OHLCV
	mode          ChartMode
	intervalIdx   int
	zoom          int // number of candles to display
	width         int
	height        int
	active        bool
	histProvider  HistoryProvider
	loading       bool
	errMsg        string
	indicators    [indCount]bool // which indicators are active
	indicatorMenu bool           // showing indicator picker
}

// New creates a chart model.
func New(histProvider HistoryProvider) Model {
	return Model{
		mode:         ModeCandlestick,
		intervalIdx:  5, // 1d default
		zoom:         50,
		histProvider: histProvider,
	}
}

// SetSymbol sets the symbol and triggers data fetch.
func (m *Model) SetSymbol(sym string) tea.Cmd {
	m.symbol = sym
	m.active = true
	m.loading = true
	m.errMsg = ""
	return m.fetchHistory()
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Active returns whether the chart is showing.
func (m Model) Active() bool {
	return m.active
}

// historyLoadedMsg is sent when history data arrives.
type historyLoadedMsg struct {
	symbol string
	data   []provider.OHLCV
}

// historyErrorMsg is sent on history fetch failure.
type historyErrorMsg struct {
	err error
}

func (m *Model) fetchHistory() tea.Cmd {
	sym := m.symbol
	interval := intervals[m.intervalIdx]
	hp := m.histProvider
	if hp == nil {
		return nil
	}
	return func() tea.Msg {
		data, err := hp.History(context.Background(), provider.HistoryParams{
			Symbol:   sym,
			Interval: interval,
			Limit:    200,
		})
		if err != nil {
			return historyErrorMsg{err: err}
		}
		return historyLoadedMsg{symbol: sym, data: data}
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case historyLoadedMsg:
		if msg.symbol == m.symbol {
			m.data = msg.data
			m.loading = false
			m.errMsg = ""
		}
		return m, nil

	case historyErrorMsg:
		m.loading = false
		m.errMsg = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		// Indicator menu handling
		if m.indicatorMenu {
			switch msg.String() {
			case "i", "esc":
				m.indicatorMenu = false
			case "1":
				m.indicators[IndSMA] = !m.indicators[IndSMA]
			case "2":
				m.indicators[IndEMA] = !m.indicators[IndEMA]
			case "3":
				m.indicators[IndBollinger] = !m.indicators[IndBollinger]
			case "4":
				m.indicators[IndRSI] = !m.indicators[IndRSI]
			case "5":
				m.indicators[IndMACD] = !m.indicators[IndMACD]
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			m.active = false
			return m, nil
		case "m":
			if m.mode == ModeCandlestick {
				m.mode = ModeLine
			} else {
				m.mode = ModeCandlestick
			}
		case "+", "=":
			if m.zoom > 10 {
				m.zoom -= 10
			}
		case "-":
			if m.zoom < 200 {
				m.zoom += 10
			}
		case "[":
			if m.intervalIdx > 0 {
				m.intervalIdx--
				return m, m.fetchHistory()
			}
		case "]":
			if m.intervalIdx < len(intervals)-1 {
				m.intervalIdx++
				return m, m.fetchHistory()
			}
		case "i":
			m.indicatorMenu = true
		}
	}
	return m, nil
}

// View renders the full chart.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder

	// Title bar
	interval := intervals[m.intervalIdx]
	modeStr := "Candlestick"
	if m.mode == ModeLine {
		modeStr = "Line"
	}
	title := styleTitle.Render(fmt.Sprintf("  %s  %s  %s", m.symbol, interval, modeStr))
	help := styleAxis.Render("  [/]: interval  +/-: zoom  m: mode  i: indicators  esc: back")
	sb.WriteString(title + "  " + help + "\n")

	// Indicator menu overlay
	if m.indicatorMenu {
		sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("  Indicators: "))
		for i := range indCount {
			marker := "○"
			if m.indicators[i] {
				marker = "●"
			}
			sb.WriteString(fmt.Sprintf(" %d:%s%s", i+1, marker, indicatorNames[i]))
		}
		sb.WriteString(styleAxis.Render("  (press 1-5 to toggle, i/esc to close)"))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	if m.loading {
		sb.WriteString(styleAxis.Render("  Loading chart data..."))
		return sb.String()
	}

	if len(m.data) == 0 {
		if m.errMsg != "" {
			sb.WriteString(theme.StyleDown.Render(fmt.Sprintf("  Error loading chart: %s", m.errMsg)))
		} else {
			sb.WriteString(styleAxis.Render("  No data available"))
		}
		return sb.String()
	}

	// Get visible candles
	candles := m.data
	if len(candles) > m.zoom {
		candles = candles[len(candles)-m.zoom:]
	}

	// Extract closes for indicators
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	// Determine chart heights
	hasSubPanel := m.indicators[IndRSI] || m.indicators[IndMACD]
	headerLines := 4
	if m.indicatorMenu {
		headerLines = 5
	}
	totalChartH := m.height - headerLines
	if totalChartH < 5 {
		totalChartH = 5
	}

	mainH := totalChartH
	subH := 0
	if hasSubPanel {
		mainH = totalChartH * 65 / 100
		subH = totalChartH - mainH - 1 // -1 for separator
		if mainH < 5 {
			mainH = 5
		}
		if subH < 3 {
			subH = 3
		}
	}

	if m.mode == ModeCandlestick {
		sb.WriteString(m.renderCandlestickWithIndicators(candles, closes, m.width-12, mainH))
	} else {
		sb.WriteString(m.renderLineWithIndicators(candles, closes, m.width-12, mainH))
	}

	// Sub-panels (RSI / MACD)
	if hasSubPanel {
		sb.WriteString(strings.Repeat("─", m.width-2))
		sb.WriteString("\n")

		if m.indicators[IndRSI] {
			sb.WriteString(m.renderRSI(closes, m.width-12, subH))
		} else if m.indicators[IndMACD] {
			sb.WriteString(m.renderMACD(closes, m.width-12, subH))
		}
	}

	// Summary
	if len(candles) > 0 {
		last := candles[len(candles)-1]
		summary := fmt.Sprintf("\n  %s O:%.2f H:%.2f L:%.2f C:%.2f V:%.0f",
			styleInfo.Render(last.Time.Format("2006-01-02 15:04")),
			last.Open, last.High, last.Low, last.Close, last.Volume)

		// Append indicator values
		var indVals []string
		if m.indicators[IndSMA] {
			sma := indicator.SMA(closes, 20)
			if v := sma[len(sma)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("SMA:%.2f", v))
			}
		}
		if m.indicators[IndEMA] {
			ema := indicator.EMA(closes, 20)
			if v := ema[len(ema)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("EMA:%.2f", v))
			}
		}
		if m.indicators[IndRSI] {
			rsi := indicator.RSI(closes, 14)
			if v := rsi[len(rsi)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("RSI:%.1f", v))
			}
		}
		if len(indVals) > 0 {
			summary += "  " + lipgloss.NewStyle().Foreground(theme.ColorMagenta).Render(strings.Join(indVals, " "))
		}
		sb.WriteString(summary)
	}

	return sb.String()
}

// renderCandlestickWithIndicators draws candlestick chart with optional indicator overlays.
func (m Model) renderCandlestickWithIndicators(candles []provider.OHLCV, closes []float64, width, height int) string {
	if len(candles) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	// Find price range (include bollinger bands if active)
	minP, maxP := candles[0].Low, candles[0].High
	for _, c := range candles {
		if c.Low < minP {
			minP = c.Low
		}
		if c.High > maxP {
			maxP = c.High
		}
	}

	if m.indicators[IndBollinger] {
		bb := indicator.Bollinger(closes, 20, 2.0)
		for _, v := range bb.Upper {
			if !math.IsNaN(v) && v > maxP {
				maxP = v
			}
		}
		for _, v := range bb.Lower {
			if !math.IsNaN(v) && v < minP {
				minP = v
			}
		}
	}

	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}
	scale := float64(height) / priceRange

	candleWidth := 2
	maxCandles := width / candleWidth
	if len(candles) > maxCandles {
		candles = candles[len(candles)-maxCandles:]
		closes = closes[len(closes)-maxCandles:]
	}

	// Build grid
	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		gridColor[i] = make([]color.Color, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	// Draw candlesticks
	for i, c := range candles {
		col := i * candleWidth
		if col >= width {
			break
		}

		isUp := c.Close >= c.Open
		bodyTop := max(c.Open, c.Close)
		bodyBot := min(c.Open, c.Close)

		highRow := height - 1 - int((c.High-minP)*scale)
		lowRow := height - 1 - int((c.Low-minP)*scale)
		topRow := height - 1 - int((bodyTop-minP)*scale)
		botRow := height - 1 - int((bodyBot-minP)*scale)

		highRow = clampRow(highRow, height)
		lowRow = clampRow(lowRow, height)
		topRow = clampRow(topRow, height)
		botRow = clampRow(botRow, height)

		color := theme.ColorGreen
		if !isUp {
			color = theme.ColorRed
		}

		for r := highRow; r < topRow; r++ {
			grid[r][col] = '│'
			gridColor[r][col] = color
		}
		for r := topRow; r <= botRow; r++ {
			if isUp {
				grid[r][col] = '┃'
			} else {
				grid[r][col] = '█'
			}
			gridColor[r][col] = color
		}
		for r := botRow + 1; r <= lowRow; r++ {
			grid[r][col] = '│'
			gridColor[r][col] = color
		}
	}

	// Overlay indicators
	m.drawOverlays(grid, gridColor, closes, width, height, minP, scale, candleWidth)

	// Render
	return renderGrid(grid, gridColor, width, height, maxP, scale)
}

// renderLineWithIndicators draws line chart with optional indicator overlays.
func (m Model) renderLineWithIndicators(candles []provider.OHLCV, closes []float64, width, height int) string {
	if len(candles) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	prices := make([]float64, len(closes))
	copy(prices, closes)

	minP, maxP := prices[0], prices[0]
	for _, p := range prices {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}

	if m.indicators[IndBollinger] {
		bb := indicator.Bollinger(prices, 20, 2.0)
		for _, v := range bb.Upper {
			if !math.IsNaN(v) && v > maxP {
				maxP = v
			}
		}
		for _, v := range bb.Lower {
			if !math.IsNaN(v) && v < minP {
				minP = v
			}
		}
	}

	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}
	scale := float64(height) / priceRange

	if len(prices) > width {
		resampled := make([]float64, width)
		for i := range width {
			idx := i * len(prices) / width
			resampled[i] = prices[idx]
		}
		prices = resampled
	}

	grid := make([][]rune, height)
	gridColor := make([][]color.Color, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		gridColor[i] = make([]color.Color, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	blocks := []rune("▁▂▃▄▅▆▇█")
	isUp := len(prices) > 1 && prices[len(prices)-1] >= prices[0]
	lineColor := theme.ColorGreen
	if !isUp {
		lineColor = theme.ColorRed
	}

	for i, p := range prices {
		if i >= width {
			break
		}
		normalized := (p - minP) / priceRange
		row := height - 1 - int(normalized*float64(height-1))
		row = clampRow(row, height)
		blockIdx := int(math.Mod(normalized*float64(len(blocks)), float64(len(blocks))))
		if blockIdx >= len(blocks) {
			blockIdx = len(blocks) - 1
		}
		grid[row][i] = blocks[blockIdx]
		gridColor[row][i] = lineColor
	}

	// Overlay indicators (use original closes, not resampled)
	m.drawOverlays(grid, gridColor, closes, width, height, minP, scale, 1)

	return renderGrid(grid, gridColor, width, height, maxP, scale)
}

func (m Model) drawOverlays(grid [][]rune, gridColor [][]color.Color, closes []float64, width, height int, minP, scale float64, step int) {
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
}

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

func renderGrid(grid [][]rune, gridColor [][]color.Color, width, height int, maxP, scale float64) string {
	var sb strings.Builder
	labelWidth := 10
	for r := range height {
		if r%(height/5+1) == 0 {
			price := maxP - float64(r)/scale
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, format.FormatAxisPrice(price))))
		} else {
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}
		for col := range grid[r] {
			ch := grid[r][col]
			clr := gridColor[r][col]
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

func clampRow(r, height int) int {
	if r < 0 {
		return 0
	}
	if r >= height {
		return height - 1
	}
	return r
}

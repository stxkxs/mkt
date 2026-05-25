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
	IndVWAP
	IndOBV
	IndATR
	IndStoch
	IndADX
	IndPivots
	IndVolProfile
	IndPatterns
	indCount
)

var indicatorNames = []string{"SMA(20)", "EMA(20)", "Bollinger", "RSI(14)", "MACD", "VWAP", "OBV", "ATR(14)", "Stoch", "ADX(14)", "Pivots", "VolProfile", "Patterns"}

// indicatorKeys is the per-indicator menu key label. Letters take over
// after the digits run out.
var indicatorKeys = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "a", "p", "v", "k"}

// volumeProfileGutterW is the number of columns reserved on the right
// edge of the main chart for the volume-profile histogram when toggled.
const volumeProfileGutterW = 15

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
	symbol      string
	data        []provider.OHLCV
	mode        ChartMode
	intervalIdx int
	zoom        int // number of candles to display
	width       int
	height      int

	// Hover crosshair: hoverCol/hoverRow are terminal coordinates of
	// the last MouseMotionMsg seen; -1 means no hover. The renderer
	// translates them into grid coordinates and draws dashed crosshair
	// lines plus a readout for the candle under the cursor.
	hoverCol      int
	hoverRow      int
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
		hoverCol:     -1,
		hoverRow:     -1,
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
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
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
			case "6":
				m.indicators[IndVWAP] = !m.indicators[IndVWAP]
			case "7":
				m.indicators[IndOBV] = !m.indicators[IndOBV]
			case "8":
				m.indicators[IndATR] = !m.indicators[IndATR]
			case "9":
				m.indicators[IndStoch] = !m.indicators[IndStoch]
			case "a":
				m.indicators[IndADX] = !m.indicators[IndADX]
			case "p":
				m.indicators[IndPivots] = !m.indicators[IndPivots]
			case "v":
				m.indicators[IndVolProfile] = !m.indicators[IndVolProfile]
			case "k":
				m.indicators[IndPatterns] = !m.indicators[IndPatterns]
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

	case tea.MouseWheelMsg:
		// Wheel up = zoom in (fewer candles); wheel down = zoom out (more).
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.zoom > 10 {
				m.zoom -= 10
			}
		case tea.MouseWheelDown:
			if m.zoom < 200 {
				m.zoom += 10
			}
		}

	case tea.MouseMotionMsg:
		// Track the cursor in terminal coordinates so the renderer can
		// draw crosshair lines + a per-candle readout. The renderer
		// itself decides whether the position falls inside the candle
		// area; here we just store the raw coords.
		m.hoverCol = msg.X
		m.hoverRow = msg.Y
	}
	return m, nil
}

// ClearHover resets the hover state. Useful for tests or for the host
// when the mouse leaves the chart's drawing area.
func (m *Model) ClearHover() {
	m.hoverCol = -1
	m.hoverRow = -1
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
			sb.WriteString(fmt.Sprintf(" %s:%s%s", indicatorKeys[i], marker, indicatorNames[i]))
		}
		sb.WriteString(styleAxis.Render("  (toggle: 1-9, a, p, v, k; i/esc to close)"))
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
	hasSubPanel := m.indicators[IndRSI] || m.indicators[IndMACD] || m.indicators[IndOBV] || m.indicators[IndATR] || m.indicators[IndStoch] || m.indicators[IndADX]
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
		} else if m.indicators[IndOBV] {
			volumes := make([]float64, len(candles))
			for i, c := range candles {
				volumes[i] = c.Volume
			}
			sb.WriteString(m.renderOBV(closes, volumes, m.width-12, subH))
		} else if m.indicators[IndATR] {
			highs, lows := extractHL(candles)
			sb.WriteString(m.renderATR(highs, lows, closes, m.width-12, subH))
		} else if m.indicators[IndStoch] {
			highs, lows := extractHL(candles)
			sb.WriteString(m.renderStoch(highs, lows, closes, m.width-12, subH))
		} else if m.indicators[IndADX] {
			highs, lows := extractHL(candles)
			sb.WriteString(m.renderADX(highs, lows, closes, m.width-12, subH))
		}
	}

	// Summary — shows the hovered candle when the cursor is inside the
	// chart, otherwise the most recent one.
	if len(candles) > 0 {
		shown := candles[len(candles)-1]
		if idx := m.hoverCandleIdx(len(candles)); idx >= 0 {
			shown = candles[idx]
		}
		summary := fmt.Sprintf("\n  %s O:%.2f H:%.2f L:%.2f C:%.2f V:%.0f",
			styleInfo.Render(shown.Time.Format("2006-01-02 15:04")),
			shown.Open, shown.High, shown.Low, shown.Close, shown.Volume)

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
			if v := vwap[len(vwap)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("VWAP:%.2f", v))
			}
		}
		if m.indicators[IndOBV] {
			vols := make([]float64, len(candles))
			for i, c := range candles {
				vols[i] = c.Volume
			}
			obv := indicator.OBV(closes, vols)
			last := obv[len(obv)-1]
			sign := ""
			if last < 0 {
				sign = "-"
				last = -last
			}
			indVals = append(indVals, fmt.Sprintf("OBV:%s%s", sign, format.FormatVolume(last)))
		}
		if m.indicators[IndATR] {
			highs, lows := extractHL(candles)
			atr := indicator.ATR(highs, lows, closes, 14)
			if v := atr[len(atr)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("ATR:%.4f", v))
			}
		}
		if m.indicators[IndStoch] {
			highs, lows := extractHL(candles)
			k, d := indicator.Stochastic(highs, lows, closes, 14, 3)
			var parts []string
			if v := k[len(k)-1]; !math.IsNaN(v) {
				parts = append(parts, fmt.Sprintf("K:%.1f", v))
			}
			if v := d[len(d)-1]; !math.IsNaN(v) {
				parts = append(parts, fmt.Sprintf("D:%.1f", v))
			}
			if len(parts) > 0 {
				indVals = append(indVals, "Stoch:"+strings.Join(parts, "/"))
			}
		}
		if m.indicators[IndADX] {
			highs, lows := extractHL(candles)
			adx, _, _ := indicator.ADX(highs, lows, closes, 14)
			if v := adx[len(adx)-1]; !math.IsNaN(v) {
				indVals = append(indVals, fmt.Sprintf("ADX:%.1f", v))
			}
		}
		if m.indicators[IndPivots] && len(candles) >= 2 {
			prev := candles[len(candles)-2]
			piv := indicator.PivotsClassic(prev.High, prev.Low, prev.Close)
			indVals = append(indVals, fmt.Sprintf("P:%.2f", piv.P))
		}
		if m.indicators[IndVolProfile] {
			bins := indicator.VolumeProfile(candles, len(candles))
			if pocIdx, _ := indicator.POC(bins); pocIdx >= 0 {
				pocPrice := (bins[pocIdx].PriceMin + bins[pocIdx].PriceMax) / 2
				indVals = append(indVals, fmt.Sprintf("POC:%.2f", pocPrice))
			}
		}
		if m.indicators[IndPatterns] {
			pats := indicator.Patterns(candles)
			// Walk backwards for the most recent detected pattern
			for i := len(pats) - 1; i >= 0; i-- {
				if pats[i] != indicator.PatternNone {
					indVals = append(indVals, "Pattern:"+pats[i].Name())
					break
				}
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

	chartW := width
	if m.indicators[IndVolProfile] && width > volumeProfileGutterW+10 {
		chartW = width - volumeProfileGutterW
	}

	candleWidth := 2
	maxCandles := chartW / candleWidth
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
		if col >= chartW {
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

	// Overlay indicators (constrained to chart area, not gutter)
	m.drawOverlays(grid, gridColor, candles, closes, chartW, height, minP, scale, candleWidth)

	// Pattern markers (candlestick mode only — line mode has no candle cues)
	if m.indicators[IndPatterns] {
		drawPatternMarkers(grid, gridColor, candles, chartW, height, minP, scale, candleWidth)
	}

	// Volume profile gutter
	if m.indicators[IndVolProfile] && chartW < width {
		drawVolumeProfileGutter(grid, gridColor, candles, chartW, width, height)
	}

	// Hover crosshair (only when the cursor is inside the candle area).
	m.drawCrosshair(grid, gridColor, chartW, height)

	// Render
	return renderGrid(grid, gridColor, width, height, maxP, scale)
}

// gridLabelWidth is the column count of the price-axis label prefix
// printed by renderGrid; used to translate hover terminal coordinates
// to grid coordinates.
const gridLabelWidth = 10

// hoverHeaderRows is the number of header lines printed by View before
// the grid begins. Two rows by default (title + blank); three when the
// indicator menu is open.
func (m Model) hoverHeaderRows() int {
	if m.indicatorMenu {
		return 5
	}
	return 4
}

// hoverCandleIdx returns the index into the visible candles slice that
// sits under the cursor, or -1 when out of bounds. Assumes candleWidth=2
// (the value used by renderCandlestickWithIndicators).
func (m Model) hoverCandleIdx(visible int) int {
	if m.hoverCol < 0 {
		return -1
	}
	gx := m.hoverCol - (gridLabelWidth + 1)
	if gx < 0 {
		return -1
	}
	idx := gx / 2
	if idx < 0 || idx >= visible {
		return -1
	}
	return idx
}

// drawCrosshair overlays dashed vertical + horizontal lines on the grid
// at the hover position. No-op when hover is unset or out of bounds.
func (m Model) drawCrosshair(grid [][]rune, gridColor [][]color.Color, chartW, height int) {
	if m.hoverCol < 0 || m.hoverRow < 0 {
		return
	}
	gx := m.hoverCol - (gridLabelWidth + 1)
	gy := m.hoverRow - m.hoverHeaderRows()
	if gx < 0 || gx >= chartW || gy < 0 || gy >= height {
		return
	}
	for r := range height {
		if grid[r][gx] == ' ' {
			grid[r][gx] = '│'
			gridColor[r][gx] = theme.ColorDim
		}
	}
	for c := range chartW {
		if grid[gy][c] == ' ' {
			grid[gy][c] = '─'
			gridColor[gy][c] = theme.ColorDim
		}
	}
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

	chartW := width
	if m.indicators[IndVolProfile] && width > volumeProfileGutterW+10 {
		chartW = width - volumeProfileGutterW
	}

	if len(prices) > chartW {
		resampled := make([]float64, chartW)
		for i := range chartW {
			idx := i * len(prices) / chartW
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
		if i >= chartW {
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

	// Overlay indicators (use original closes, not resampled; constrained to chartW)
	m.drawOverlays(grid, gridColor, candles, closes, chartW, height, minP, scale, 1)

	// Volume profile gutter
	if m.indicators[IndVolProfile] && chartW < width {
		drawVolumeProfileGutter(grid, gridColor, candles, chartW, width, height)
	}

	return renderGrid(grid, gridColor, width, height, maxP, scale)
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

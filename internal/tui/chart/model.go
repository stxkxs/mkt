package chart

import (
	"context"
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleAxis  = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
	styleInfo  = lipgloss.NewStyle().Foreground(theme.ColorCyan)
)

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
	symbol       string
	data         []provider.OHLCV
	mode         ChartMode
	intervalIdx  int
	zoom         int // number of candles to display
	width        int
	height       int
	active       bool
	histProvider HistoryProvider
	loading      bool
	errMsg       string
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
	help := styleAxis.Render("  [/]: interval  +/-: zoom  m: mode  esc: back")
	sb.WriteString(title + "  " + help + "\n\n")

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

	chartHeight := m.height - 6
	if chartHeight < 5 {
		chartHeight = 5
	}

	if m.mode == ModeCandlestick {
		sb.WriteString(renderCandlestick(candles, m.width-12, chartHeight))
	} else {
		sb.WriteString(renderLine(candles, m.width-12, chartHeight))
	}

	// Summary
	if len(candles) > 0 {
		last := candles[len(candles)-1]
		sb.WriteString(fmt.Sprintf("\n  %s O:%.2f H:%.2f L:%.2f C:%.2f V:%.0f",
			styleInfo.Render(last.Time.Format("2006-01-02 15:04")),
			last.Open, last.High, last.Low, last.Close, last.Volume))
	}

	return sb.String()
}

// renderCandlestick draws a candlestick chart using Unicode characters.
func renderCandlestick(candles []provider.OHLCV, width, height int) string {
	if len(candles) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	// Find price range
	minP, maxP := candles[0].Low, candles[0].High
	for _, c := range candles {
		if c.Low < minP {
			minP = c.Low
		}
		if c.High > maxP {
			maxP = c.High
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}

	// Scale factor
	scale := float64(height) / priceRange

	// How many candles fit (each candle = 1 col width + 1 spacing)
	candleWidth := 2
	maxCandles := width / candleWidth
	if len(candles) > maxCandles {
		candles = candles[len(candles)-maxCandles:]
	}

	// Build grid
	grid := make([][]rune, height)
	colors := make([][]bool, height) // true = green, false = red
	for i := range grid {
		grid[i] = make([]rune, width)
		colors[i] = make([]bool, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

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

		// Clamp
		highRow = clampRow(highRow, height)
		lowRow = clampRow(lowRow, height)
		topRow = clampRow(topRow, height)
		botRow = clampRow(botRow, height)

		// Wick above body
		for r := highRow; r < topRow; r++ {
			grid[r][col] = '│'
			colors[r][col] = isUp
		}

		// Body
		for r := topRow; r <= botRow; r++ {
			if isUp {
				grid[r][col] = '┃'
			} else {
				grid[r][col] = '█'
			}
			colors[r][col] = isUp
		}

		// Wick below body
		for r := botRow + 1; r <= lowRow; r++ {
			grid[r][col] = '│'
			colors[r][col] = isUp
		}
	}

	// Render with colors and Y-axis labels
	var sb strings.Builder
	labelWidth := 10
	for r := range height {
		// Y-axis price label (every 5 rows)
		if r%(height/5+1) == 0 {
			price := maxP - float64(r)/scale
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, format.FormatAxisPrice(price))))
		} else {
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}

		// Chart row
		for col := range grid[r] {
			ch := grid[r][col]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if colors[r][col] {
				sb.WriteString(theme.StyleUp.Render(string(ch)))
			} else {
				sb.WriteString(theme.StyleDown.Render(string(ch)))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// renderLine draws a line chart using braille-like characters.
func renderLine(candles []provider.OHLCV, width, height int) string {
	if len(candles) == 0 || width <= 0 || height <= 0 {
		return ""
	}

	// Extract close prices
	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}

	// Find range
	minP, maxP := prices[0], prices[0]
	for _, p := range prices {
		if p < minP {
			minP = p
		}
		if p > maxP {
			maxP = p
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}

	// Resample if needed
	if len(prices) > width {
		resampled := make([]float64, width)
		for i := range width {
			idx := i * len(prices) / width
			resampled[i] = prices[idx]
		}
		prices = resampled
	}

	// Build grid
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	blocks := []rune("▁▂▃▄▅▆▇█")

	for i, p := range prices {
		if i >= width {
			break
		}
		normalized := (p - minP) / priceRange
		row := height - 1 - int(normalized*float64(height-1))
		if row < 0 {
			row = 0
		}
		if row >= height {
			row = height - 1
		}
		blockIdx := int(math.Mod(normalized*float64(len(blocks)), float64(len(blocks))))
		if blockIdx >= len(blocks) {
			blockIdx = len(blocks) - 1
		}
		grid[row][i] = blocks[blockIdx]
	}

	// Render
	var sb strings.Builder
	labelWidth := 10
	isUp := len(prices) > 1 && prices[len(prices)-1] >= prices[0]

	for r := range height {
		if r%(height/5+1) == 0 {
			price := maxP - float64(r)/float64(height)*priceRange
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, format.FormatAxisPrice(price))))
		} else {
			sb.WriteString(strings.Repeat(" ", labelWidth+1))
		}

		for col := range grid[r] {
			ch := grid[r][col]
			if ch == ' ' {
				sb.WriteRune(' ')
			} else if isUp {
				sb.WriteString(theme.StyleUp.Render(string(ch)))
			} else {
				sb.WriteString(theme.StyleDown.Render(string(ch)))
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

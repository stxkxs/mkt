package chart

import (
	"context"
	"fmt"
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

// candleWidth is the number of grid columns one candle occupies in
// candlestick mode: one for the body, one for the gap.
const candleWidth = 2

// Zoom bounds. zoomStep is how many candles one +/- press adds or
// removes; minZoom is the tightest window the chart allows.
const (
	zoomStep = 10
	minZoom  = 10
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

// defaultIntervalIdx is the index of the 1d interval in intervals.
const defaultIntervalIdx = 5

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
	width       int
	height      int

	// zoom is the number of candles the user asked to see. autoZoom
	// means "as many as the terminal can show": the chart fits itself to
	// the window and re-fits on resize, instead of pinning a fixed 50
	// candles and leaving half of a wide terminal blank. Pressing +/-
	// takes manual control and clamps to the data actually fetched.
	zoom     int
	autoZoom bool

	// dataInterval is the interval m.data was requested at and served
	// is the interval the provider actually delivered; they differ when
	// a provider maps an interval it cannot serve (Yahoo answers a 4h
	// request with 1h bars). The header is labelled from served.
	dataInterval provider.Interval
	served       provider.Interval

	// Hover crosshair: hoverCol/hoverRow are terminal coordinates of
	// the last MouseMotionMsg seen; -1 means no hover. The renderer
	// translates them into grid coordinates and draws dashed crosshair
	// lines plus a readout for the candle under the cursor.
	hoverCol int
	hoverRow int
	active   bool

	histProvider HistoryProvider
	cache        *historyCache

	// reqSeq tags every history request so a response that lands after
	// a newer request was issued can be discarded, and cancel aborts
	// the request currently in flight.
	reqSeq uint64
	cancel context.CancelFunc

	loading       bool
	errMsg        string
	indicators    [indCount]bool // which indicators are active
	indicatorMenu bool           // showing indicator picker
}

// New creates a chart model.
func New(histProvider HistoryProvider) Model {
	return Model{
		mode:         ModeCandlestick,
		intervalIdx:  defaultIntervalIdx,
		autoZoom:     true,
		histProvider: histProvider,
		cache:        newHistoryCache(),
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

// SetSize updates dimensions. While the zoom is auto-fitted the visible
// window follows the new width on the next render; a manual zoom is only
// clamped down when it no longer fits.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.autoZoom {
		if lim := m.zoomLimit(); m.zoom > lim {
			m.zoom = lim
		}
	}
}

// Active returns whether the chart is showing.
func (m Model) Active() bool {
	return m.active
}

// interval is the currently selected interval.
func (m Model) interval() provider.Interval {
	if m.intervalIdx < 0 || m.intervalIdx >= len(intervals) {
		return intervals[defaultIntervalIdx]
	}
	return intervals[m.intervalIdx]
}

// plotWidth is the number of grid columns available to the plot,
// leaving room for the axis gutter.
func (m Model) plotWidth() int {
	return m.width - (gridLabelWidth + 2)
}

// chartWidth is the plot width available to the price series, excluding
// the volume-profile gutter when it is shown.
func (m Model) chartWidth() int {
	w := m.plotWidth()
	if m.indicators[IndVolProfile] && w > volumeProfileGutterW+10 {
		w -= volumeProfileGutterW
	}
	return w
}

// candleStep is the number of grid columns each candle occupies.
func (m Model) candleStep() int {
	if m.mode == ModeLine {
		return 1
	}
	return candleWidth
}

// capacity is the number of candles that fit across the plot area.
func (m Model) capacity() int {
	w := m.chartWidth()
	if w < 1 {
		return 0
	}
	return w / m.candleStep()
}

// visibleCount is how many of the fetched candles are on screen: the
// zoom level clamped to what fits across the plot and to the history
// actually fetched.
func (m Model) visibleCount() int {
	n := len(m.data)
	if n == 0 {
		return 0
	}
	fit := m.capacity()
	if fit < 1 {
		return 0
	}
	want := m.zoom
	if m.autoZoom || want < 1 {
		want = fit
	}
	return min(want, fit, n)
}

// zoomLimit is the widest useful window: no more candles than fit across
// the plot, and no more than were fetched.
func (m Model) zoomLimit() int {
	lim := m.capacity()
	if n := len(m.data); n > 0 && n < lim {
		lim = n
	}
	if lim < minZoom {
		lim = minZoom
	}
	return lim
}

// zoomBy narrows (negative delta) or widens the visible window, taking
// manual control of the zoom. The result is clamped to minZoom and to
// the data actually available.
func (m *Model) zoomBy(delta int) {
	cur := m.visibleCount()
	if cur < 1 {
		cur = m.zoom
	}
	if cur < 1 {
		cur = m.zoomLimit()
	}
	m.autoZoom = false
	n := cur + delta
	if lim := m.zoomLimit(); n > lim {
		n = lim
	}
	if n < minZoom {
		n = minZoom
	}
	m.zoom = n
}

// historyLoadedMsg is sent when history data arrives. seq identifies the
// request it answers so a superseded response can be dropped.
type historyLoadedMsg struct {
	seq      uint64
	symbol   string
	interval provider.Interval
	served   provider.Interval
	data     []provider.OHLCV
}

// historyErrorMsg is sent on history fetch failure, tagged like
// historyLoadedMsg so a stale failure cannot clear a fresh chart.
type historyErrorMsg struct {
	seq    uint64
	symbol string
	err    error
}

// servedIntervalFor asks the provider which interval it will actually
// serve. Providers that serve every interval natively do not implement
// ServedIntervalProvider, in which case the request stands.
func (m Model) servedIntervalFor(sym string, req provider.Interval) provider.Interval {
	if p, ok := m.histProvider.(ServedIntervalProvider); ok {
		if got := p.ServedInterval(sym, req); got != "" {
			return got
		}
	}
	return req
}

// fetchHistory starts a history request for the current symbol and
// interval.
//
// Any request already in flight is cancelled and its answer discarded:
// pressing ] three times quickly used to leave whichever response landed
// last on screen, which could put 1h data under a "4h" label. Every
// request carries a sequence number and Update accepts only the newest.
// A fresh series is served straight from the cache so a burst of
// interval switches costs at most one request per interval.
func (m *Model) fetchHistory() tea.Cmd {
	hp := m.histProvider
	if hp == nil || m.symbol == "" {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}

	m.reqSeq++
	seq := m.reqSeq
	sym := m.symbol
	req := m.interval()
	served := m.servedIntervalFor(sym, req)

	if data, ok := m.cache.get(sym, req); ok {
		m.loading = false
		return func() tea.Msg {
			return historyLoadedMsg{seq: seq, symbol: sym, interval: req, served: served, data: data}
		}
	}

	m.loading = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	cache := m.cache
	return func() tea.Msg {
		defer cancel()
		data, err := hp.History(ctx, provider.HistoryParams{
			Symbol:   sym,
			Interval: req,
			Limit:    historyLimit,
		})
		if err != nil {
			return historyErrorMsg{seq: seq, symbol: sym, err: err}
		}
		cache.put(sym, req, data)
		return historyLoadedMsg{seq: seq, symbol: sym, interval: req, served: served, data: data}
	}
}

// accepts reports whether a tagged history response is still the one the
// chart is waiting for.
func (m Model) accepts(seq uint64, sym string) bool {
	return seq == m.reqSeq && sym == m.symbol
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case historyLoadedMsg:
		if !m.accepts(msg.seq, msg.symbol) {
			return m, nil
		}
		m.data = msg.data
		m.dataInterval = msg.interval
		m.served = msg.served
		m.loading = false
		m.errMsg = ""
		if !m.autoZoom {
			if lim := m.zoomLimit(); m.zoom > lim {
				m.zoom = lim
			}
		}
		return m, nil

	case historyErrorMsg:
		if !m.accepts(msg.seq, msg.symbol) {
			return m, nil
		}
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
			m.zoomBy(-zoomStep)
		case "-":
			m.zoomBy(zoomStep)
		case "f":
			// Refit the window to the terminal width.
			m.autoZoom = true
		case "[":
			if m.intervalIdx > 0 {
				m.intervalIdx--
				cmd := m.fetchHistory()
				return m, cmd
			}
		case "]":
			if m.intervalIdx < len(intervals)-1 {
				m.intervalIdx++
				cmd := m.fetchHistory()
				return m, cmd
			}
		case "i":
			m.indicatorMenu = true
		}

	case tea.MouseWheelMsg:
		// Wheel up = zoom in (fewer candles); wheel down = zoom out (more).
		switch msg.Button {
		case tea.MouseWheelUp:
			m.zoomBy(-zoomStep)
		case tea.MouseWheelDown:
			m.zoomBy(zoomStep)
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

// intervalLabel is the header's interval text. When the provider serves
// a coarser or finer resolution than the one requested, both are shown
// so the header never claims a resolution the chart is not drawing.
func (m Model) intervalLabel() string {
	req := m.interval()
	if m.dataInterval == req && m.served != "" && m.served != req {
		return fmt.Sprintf("%s (%s bars)", req, m.served)
	}
	return string(req)
}

// View renders the full chart.
func (m Model) View() string {
	if m.width < 1 || m.height < 1 {
		return ""
	}

	var sb strings.Builder

	// Title bar
	modeStr := "Candlestick"
	if m.mode == ModeLine {
		modeStr = "Line"
	}
	title := styleTitle.Render(fmt.Sprintf("  %s  %s  %s", m.symbol, m.intervalLabel(), modeStr))
	help := styleAxis.Render("  [/]: interval  +/-: zoom  f: fit  m: mode  i: indicators  esc: back")
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

	// One viewport per frame. Indicators are computed over the full
	// fetched history and only then narrowed to the visible window, so
	// their values do not move when the user zooms.
	set := computeIndicators(m.data, m.indicators)
	v := newViewport(m.data, set, m.visibleCount(), m.candleStep())
	if v.len() == 0 {
		sb.WriteString(styleAxis.Render("  Terminal too small for chart"))
		return sb.String()
	}

	// Determine chart heights
	hasSubPanel := m.indicators[IndRSI] || m.indicators[IndMACD] || m.indicators[IndOBV] || m.indicators[IndATR] || m.indicators[IndStoch] || m.indicators[IndADX]
	totalChartH := m.height - m.headerBudget()
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
		sb.WriteString(m.renderCandlestick(v, m.plotWidth(), mainH))
	} else {
		sb.WriteString(m.renderLine(v, m.plotWidth(), mainH))
	}

	// Sub-panels (RSI / MACD / OBV / ATR / Stoch / ADX). The sub-panel
	// shares the main chart's width and candle step so its x-axis lines
	// up with the candles above it.
	if hasSubPanel {
		sb.WriteString(repeat("─", m.width-2))
		sb.WriteString("\n")

		subW := m.chartWidth()
		switch {
		case m.indicators[IndRSI]:
			sb.WriteString(renderRSI(v, subW, subH))
		case m.indicators[IndMACD]:
			sb.WriteString(renderMACD(v, subW, subH))
		case m.indicators[IndOBV]:
			sb.WriteString(renderOBV(v, subW, subH))
		case m.indicators[IndATR]:
			sb.WriteString(renderATR(v, subW, subH))
		case m.indicators[IndStoch]:
			sb.WriteString(renderStoch(v, subW, subH))
		case m.indicators[IndADX]:
			sb.WriteString(renderADX(v, subW, subH))
		}
	}

	sb.WriteString(m.renderReadout(v, mainH))
	return sb.String()
}

// headerBudget is the number of terminal rows View reserves outside the
// grid: the header lines plus the trailing readout.
func (m Model) headerBudget() int {
	if m.indicatorMenu {
		return 5
	}
	return 4
}

// renderReadout is the summary line under the chart. It describes the
// hovered candle when the cursor is inside the chart, otherwise the most
// recent one, and reports every active indicator at that same bar — the
// same viewport the grid above was drawn from.
func (m Model) renderReadout(v viewport, mainH int) string {
	idx := m.hoverCandleIdx(v.len(), v.step)
	if idx < 0 {
		idx = v.len() - 1
	}
	if idx < 0 {
		return ""
	}
	shown := v.candles[idx]

	summary := fmt.Sprintf("\n  %s O:%.2f H:%.2f L:%.2f C:%.2f V:%.0f",
		styleInfo.Render(shown.Time.Format("2006-01-02 15:04")),
		shown.Open, shown.High, shown.Low, shown.Close, shown.Volume)

	var indVals []string
	if m.indicators[IndSMA] {
		if val, ok := valueAt(v.ind.sma, idx); ok {
			indVals = append(indVals, fmt.Sprintf("SMA:%.2f", val))
		}
	}
	if m.indicators[IndEMA] {
		if val, ok := valueAt(v.ind.ema, idx); ok {
			indVals = append(indVals, fmt.Sprintf("EMA:%.2f", val))
		}
	}
	if m.indicators[IndRSI] {
		if val, ok := valueAt(v.ind.rsi, idx); ok {
			indVals = append(indVals, fmt.Sprintf("RSI:%.1f", val))
		}
	}
	if m.indicators[IndVWAP] {
		if val, ok := valueAt(v.ind.vwap, idx); ok {
			indVals = append(indVals, fmt.Sprintf("VWAP:%.2f", val))
		}
	}
	if m.indicators[IndOBV] {
		if val, ok := valueAt(v.ind.obv, idx); ok {
			sign := ""
			if val < 0 {
				sign = "-"
				val = -val
			}
			indVals = append(indVals, fmt.Sprintf("OBV:%s%s", sign, format.FormatVolume(val)))
		}
	}
	if m.indicators[IndATR] {
		if val, ok := valueAt(v.ind.atr, idx); ok {
			indVals = append(indVals, fmt.Sprintf("ATR:%.4f", val))
		}
	}
	if m.indicators[IndStoch] {
		var parts []string
		if val, ok := valueAt(v.ind.stochK, idx); ok {
			parts = append(parts, fmt.Sprintf("K:%.1f", val))
		}
		if val, ok := valueAt(v.ind.stochD, idx); ok {
			parts = append(parts, fmt.Sprintf("D:%.1f", val))
		}
		if len(parts) > 0 {
			indVals = append(indVals, "Stoch:"+strings.Join(parts, "/"))
		}
	}
	if m.indicators[IndADX] {
		if val, ok := valueAt(v.ind.adx, idx); ok {
			indVals = append(indVals, fmt.Sprintf("ADX:%.1f", val))
		}
	}
	if m.indicators[IndPivots] && v.ind.hasPivots {
		indVals = append(indVals, fmt.Sprintf("P:%.2f", v.ind.pivots.P))
	}
	if m.indicators[IndVolProfile] {
		// Same bins as the gutter histogram, so the two agree on where
		// the point of control sits.
		bins := volumeBins(v.candles, mainH)
		if pocIdx, _ := indicator.POC(bins); pocIdx >= 0 {
			pocPrice := (bins[pocIdx].PriceMin + bins[pocIdx].PriceMax) / 2
			indVals = append(indVals, fmt.Sprintf("POC:%.2f", pocPrice))
		}
	}
	if m.indicators[IndPatterns] {
		// Most recent pattern at or before the bar being described.
		for i := min(idx, len(v.ind.patterns)-1); i >= 0; i-- {
			if v.ind.patterns[i] != indicator.PatternNone {
				indVals = append(indVals, "Pattern:"+v.ind.patterns[i].Name())
				break
			}
		}
	}
	if len(indVals) > 0 {
		summary += "  " + lipgloss.NewStyle().Foreground(theme.ColorMagenta).Render(strings.Join(indVals, " "))
	}
	return summary
}

// priceScale measures the visible price range, widening it to contain
// the Bollinger bands when they are drawn. It is derived from exactly
// the candles the grid will draw, so the axis, the candles and the
// readout agree.
func (m Model) priceScale(v viewport, height int, lows, highs []float64) vscale {
	if len(lows) == 0 || len(highs) == 0 {
		return newPriceScale(0, 1, height)
	}
	minP, maxP := lows[0], highs[0]
	for _, p := range lows {
		if p < minP {
			minP = p
		}
	}
	for _, p := range highs {
		if p > maxP {
			maxP = p
		}
	}
	if m.indicators[IndBollinger] {
		for _, val := range v.ind.bb.Upper {
			if !math.IsNaN(val) && val > maxP {
				maxP = val
			}
		}
		for _, val := range v.ind.bb.Lower {
			if !math.IsNaN(val) && val < minP {
				minP = val
			}
		}
	}
	return newPriceScale(minP, maxP, height)
}

// renderCandlestick draws the candlestick chart with its indicator
// overlays. width is the full plot width including the volume-profile
// gutter; the candles themselves are confined to chartWidth.
func (m Model) renderCandlestick(v viewport, width, height int) string {
	if v.len() == 0 || width <= 0 || height <= 0 {
		return ""
	}
	scale := m.priceScale(v, height, v.lows, v.highs)
	chartW := m.chartWidth()
	if chartW > width {
		chartW = width
	}

	p := newPanel(width, height)

	for i, c := range v.candles {
		col := i * v.step
		if col >= chartW {
			break
		}

		isUp := c.Close >= c.Open
		bodyTop := max(c.Open, c.Close)
		bodyBot := min(c.Open, c.Close)

		highRow, okH := scale.row(c.High)
		lowRow, okL := scale.row(c.Low)
		topRow, okT := scale.row(bodyTop)
		botRow, okB := scale.row(bodyBot)
		if !okH || !okL || !okT || !okB {
			continue
		}

		clr := theme.ColorGreen
		if !isUp {
			clr = theme.ColorRed
		}

		for r := highRow; r < topRow; r++ {
			p.paint(r, col, '│', clr, paintOver)
		}
		for r := topRow; r <= botRow; r++ {
			if isUp {
				p.paint(r, col, '┃', clr, paintOver)
			} else {
				p.paint(r, col, '█', clr, paintOver)
			}
		}
		for r := botRow + 1; r <= lowRow; r++ {
			p.paint(r, col, '│', clr, paintOver)
		}
	}

	// Overlay indicators (constrained to chart area, not gutter)
	m.drawOverlays(p, v, scale, chartW)

	// Pattern markers (candlestick mode only — line mode has no candle cues)
	if m.indicators[IndPatterns] {
		drawPatternMarkers(p, v, scale, chartW)
	}

	// Volume profile gutter
	if m.indicators[IndVolProfile] && chartW < width {
		drawVolumeProfileGutter(p, v.candles, chartW, width, height)
	}

	// Hover crosshair (only when the cursor is inside the candle area).
	m.drawCrosshair(p, chartW)

	return p.render(priceAxis(scale, height))
}

// renderLine draws the line chart with its indicator overlays.
func (m Model) renderLine(v viewport, width, height int) string {
	if v.len() == 0 || width <= 0 || height <= 0 {
		return ""
	}
	scale := m.priceScale(v, height, v.closes, v.closes)
	chartW := m.chartWidth()
	if chartW > width {
		chartW = width
	}

	p := newPanel(width, height)

	blocks := []rune("▁▂▃▄▅▆▇█")
	isUp := v.len() > 1 && v.closes[v.len()-1] >= v.closes[0]
	lineColor := theme.ColorGreen
	if !isUp {
		lineColor = theme.ColorRed
	}

	span := scale.maxV - scale.minV
	if !(span > 0) {
		span = 1
	}
	for i, price := range v.closes {
		col := i * v.step
		if col >= chartW {
			break
		}
		row, ok := scale.row(price)
		if !ok {
			continue
		}
		normalized := (price - scale.minV) / span
		blockIdx := int(math.Mod(normalized*float64(len(blocks)), float64(len(blocks))))
		if blockIdx >= len(blocks) {
			blockIdx = len(blocks) - 1
		}
		if blockIdx < 0 {
			blockIdx = 0
		}
		p.paint(row, col, blocks[blockIdx], lineColor, paintOver)
	}

	m.drawOverlays(p, v, scale, chartW)

	if m.indicators[IndVolProfile] && chartW < width {
		drawVolumeProfileGutter(p, v.candles, chartW, width, height)
	}

	m.drawCrosshair(p, chartW)

	return p.render(priceAxis(scale, height))
}

// priceAxis builds the row-label function for the price grid: a price
// every few rows, blank in between.
func priceAxis(scale vscale, height int) func(int) string {
	every := height/5 + 1
	if every < 1 {
		every = 1
	}
	return func(r int) string {
		if r%every != 0 {
			return ""
		}
		return format.FormatAxisPrice(scale.value(r))
	}
}

// hoverHeaderRows is the number of terminal rows View prints before the
// first grid row: the title line plus a blank line, and the indicator
// menu line when it is open.
func (m Model) hoverHeaderRows() int {
	if m.indicatorMenu {
		return 3
	}
	return 2
}

// hoverCandleIdx returns the index into the visible candles that sits
// under the cursor, or -1 when out of bounds. step is the candle width
// in columns, which differs between candlestick and line mode.
func (m Model) hoverCandleIdx(visible, step int) int {
	if m.hoverCol < 0 || visible <= 0 {
		return -1
	}
	if step < 1 {
		step = 1
	}
	gx := m.hoverCol - (gridLabelWidth + 1)
	if gx < 0 {
		return -1
	}
	idx := gx / step
	if idx >= visible {
		return -1
	}
	return idx
}

// drawCrosshair overlays dashed vertical + horizontal lines on the grid
// at the hover position. No-op when hover is unset or out of bounds.
func (m Model) drawCrosshair(p *panel, chartW int) {
	if m.hoverCol < 0 || m.hoverRow < 0 {
		return
	}
	gx := m.hoverCol - (gridLabelWidth + 1)
	gy := m.hoverRow - m.hoverHeaderRows()
	if gx < 0 || gx >= chartW || gy < 0 || gy >= p.h {
		return
	}
	p.vline(gx, '│', theme.ColorDim, paintIfEmpty)
	for c := range min(chartW, p.w) {
		p.paint(gy, c, '─', theme.ColorDim, paintIfEmpty)
	}
}

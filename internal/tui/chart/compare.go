package chart

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// maxCompareSymbols is how many series the comparison chart holds. The
// palette is sized to match so no two series share a color.
const maxCompareSymbols = 3

// compareColorList is the comparison palette, one entry per comparison
// slot.
func compareColorList() []color.Color {
	return []color.Color{theme.ColorCyan, theme.ColorYellow, theme.ColorMagenta}
}

// compareColorFor returns the plot color for sym, derived from its
// position in the comparison set.
//
// Both the legend and the plotted series call this, so they cannot
// disagree. They used to: the legend colored by position in the symbol
// list while the series colored by position in the fetched-entry list,
// and the entries were appended in whatever order the concurrent
// fetches happened to finish. The legend was routinely wrong.
func compareColorFor(symbols []string, sym string) color.Color {
	colors := compareColorList()
	for i, s := range symbols {
		if s == sym {
			return colors[i%len(colors)]
		}
	}
	return theme.ColorDim
}

// CompareEntry holds data for one comparison symbol.
type CompareEntry struct {
	Symbol string
	Data   []provider.OHLCV
}

// compareLoadedMsg is sent when comparison data arrives. seq identifies
// the request it answers so a superseded batch can be dropped.
type compareLoadedMsg struct {
	seq     uint64
	entries []CompareEntry
}

// CompareModel is the multi-symbol comparison chart.
type CompareModel struct {
	entries     []CompareEntry
	symbols     []string // symbols to compare (up to maxCompareSymbols)
	zoom        int
	autoZoom    bool
	intervalIdx int
	width       int
	height      int
	active      bool

	histProvider HistoryProvider
	cache        *historyCache

	// reqSeq tags every batch so a response for an interval the user has
	// already moved on from is discarded instead of overwriting the
	// current one; cancel aborts the batch in flight.
	reqSeq uint64
	cancel context.CancelFunc

	loading bool
}

// NewCompare creates a comparison model.
func NewCompare(hp HistoryProvider) CompareModel {
	return CompareModel{
		autoZoom:     true,
		intervalIdx:  defaultIntervalIdx,
		histProvider: hp,
		cache:        newHistoryCache(),
	}
}

// Active returns whether comparison chart is showing.
func (m CompareModel) Active() bool {
	return m.active
}

// AddSymbol adds a symbol to the comparison set (max 3, one per palette
// color). Duplicates and overflow are ignored.
func (m *CompareModel) AddSymbol(sym string) {
	for _, s := range m.symbols {
		if s == sym {
			return
		}
	}
	if len(m.symbols) >= maxCompareSymbols {
		return
	}
	m.symbols = append(m.symbols, sym)
}

// Symbols returns the current comparison symbols.
func (m CompareModel) Symbols() []string {
	return m.symbols
}

// Open activates the comparison chart and fetches data.
func (m *CompareModel) Open() tea.Cmd {
	if len(m.symbols) == 0 {
		return nil
	}
	m.active = true
	m.loading = true
	return m.fetchAll()
}

// SetSize updates dimensions.
func (m *CompareModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	if !m.autoZoom {
		if lim := m.zoomLimit(); m.zoom > lim {
			m.zoom = lim
		}
	}
}

// interval is the currently selected interval.
func (m CompareModel) interval() provider.Interval {
	if m.intervalIdx < 0 || m.intervalIdx >= len(intervals) {
		return intervals[defaultIntervalIdx]
	}
	return intervals[m.intervalIdx]
}

// plotWidth is the number of columns available to the comparison plot.
func (m CompareModel) plotWidth() int {
	return m.width - (gridLabelWidth + 2)
}

// zoomLimit is the widest useful window: no more points than fit across
// the plot, and no more than the shortest series carries.
func (m CompareModel) zoomLimit() int {
	lim := m.plotWidth()
	if n := m.shortestSeries(); n > 0 && n < lim {
		lim = n
	}
	if lim < minZoom {
		lim = minZoom
	}
	return lim
}

// shortestSeries is the length of the shortest loaded series, which
// bounds the window every series can share.
func (m CompareModel) shortestSeries() int {
	shortest := 0
	for _, e := range m.entries {
		if len(e.Data) == 0 {
			continue
		}
		if shortest == 0 || len(e.Data) < shortest {
			shortest = len(e.Data)
		}
	}
	return shortest
}

// visibleCount is how many points of each series are on screen.
func (m CompareModel) visibleCount() int {
	n := m.shortestSeries()
	if n == 0 {
		return 0
	}
	fit := m.plotWidth()
	if fit < 1 {
		return 0
	}
	want := m.zoom
	if m.autoZoom || want < 1 {
		want = fit
	}
	return min(want, fit, n)
}

// zoomBy narrows (negative delta) or widens the window, taking manual
// control of the zoom.
func (m *CompareModel) zoomBy(delta int) {
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

// fetchAll loads every comparison symbol at the current interval.
//
// Entries come back in the same order as m.symbols regardless of which
// fetch finishes first, and a batch superseded by a later interval
// change is cancelled and its answer discarded.
func (m *CompareModel) fetchAll() tea.Cmd {
	hp := m.histProvider
	if hp == nil || len(m.symbols) == 0 {
		return nil
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}

	m.reqSeq++
	seq := m.reqSeq
	req := m.interval()
	syms := make([]string, len(m.symbols))
	copy(syms, m.symbols)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.loading = true
	cache := m.cache

	return func() tea.Msg {
		defer cancel()

		// Indexed writes, so completion order cannot reorder the set.
		loaded := make([][]provider.OHLCV, len(syms))
		var wg sync.WaitGroup
		for i, sym := range syms {
			if data, ok := cache.get(sym, req); ok {
				loaded[i] = data
				continue
			}
			wg.Add(1)
			go func(i int, s string) {
				defer wg.Done()
				data, err := hp.History(ctx, provider.HistoryParams{
					Symbol:   s,
					Interval: req,
					Limit:    historyLimit,
				})
				if err != nil {
					return
				}
				cache.put(s, req, data)
				loaded[i] = data
			}(i, sym)
		}
		wg.Wait()

		entries := make([]CompareEntry, 0, len(syms))
		for i, sym := range syms {
			if len(loaded[i]) == 0 {
				continue
			}
			entries = append(entries, CompareEntry{Symbol: sym, Data: loaded[i]})
		}
		return compareLoadedMsg{seq: seq, entries: entries}
	}
}

// Update handles messages.
func (m CompareModel) Update(msg tea.Msg) (CompareModel, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case compareLoadedMsg:
		if msg.seq != m.reqSeq {
			return m, nil
		}
		m.entries = msg.entries
		m.loading = false
		if !m.autoZoom {
			if lim := m.zoomLimit(); m.zoom > lim {
				m.zoom = lim
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.active = false
			return m, nil
		case "+", "=":
			m.zoomBy(-zoomStep)
		case "-":
			m.zoomBy(zoomStep)
		case "f":
			m.autoZoom = true
		case "[":
			if m.intervalIdx > 0 {
				m.intervalIdx--
				cmd := m.fetchAll()
				return m, cmd
			}
		case "]":
			if m.intervalIdx < len(intervals)-1 {
				m.intervalIdx++
				cmd := m.fetchAll()
				return m, cmd
			}
		case "x":
			if len(m.symbols) > 0 {
				dropped := m.symbols[len(m.symbols)-1]
				m.symbols = m.symbols[:len(m.symbols)-1]
				m.entries = withoutSymbol(m.entries, dropped)
				if len(m.symbols) == 0 {
					m.active = false
					return m, nil
				}
				cmd := m.fetchAll()
				return m, cmd
			}
		}

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.zoomBy(-zoomStep)
		case tea.MouseWheelDown:
			m.zoomBy(zoomStep)
		}
	}
	return m, nil
}

// withoutSymbol drops one entry, keeping the order of the rest so the
// remaining series keep their colors.
func withoutSymbol(entries []CompareEntry, sym string) []CompareEntry {
	out := entries[:0:0]
	for _, e := range entries {
		if e.Symbol != sym {
			out = append(out, e)
		}
	}
	return out
}

// compareSeries is one normalized comparison line.
type compareSeries struct {
	symbol string
	pcts   []float64
	color  color.Color
}

// buildSeries normalizes every loaded entry to percent change over a
// common window — the last count bars of each — so the series share a
// start date and the percentages are actually comparable. Colors come
// from compareColorFor, the same source the legend uses.
func (m CompareModel) buildSeries(count int) []compareSeries {
	if count <= 0 {
		return nil
	}
	out := make([]compareSeries, 0, len(m.entries))
	for _, entry := range m.entries {
		data := entry.Data
		if len(data) < count {
			continue
		}
		data = data[len(data)-count:]
		base := data[0].Close
		if base == 0 || math.IsNaN(base) {
			continue
		}
		pcts := make([]float64, len(data))
		for j, d := range data {
			pcts[j] = (d.Close - base) / base * 100
		}
		out = append(out, compareSeries{
			symbol: entry.Symbol,
			pcts:   pcts,
			color:  compareColorFor(m.symbols, entry.Symbol),
		})
	}
	return out
}

// View renders the comparison chart.
func (m CompareModel) View() string {
	if m.width < 1 || m.height < 1 {
		return ""
	}

	var sb strings.Builder

	// Title with legend. Legend colors come from compareColorFor, the
	// same function the plotted series use.
	title := styleTitle.Render(fmt.Sprintf("  Compare  %s", m.interval()))
	var legend []string
	for _, sym := range m.symbols {
		clr := compareColorFor(m.symbols, sym)
		legend = append(legend, lipgloss.NewStyle().Foreground(clr).Render("● "+sym))
	}
	sb.WriteString(title + "  " + strings.Join(legend, "  "))
	sb.WriteString("\n")
	help := styleAxis.Render("  [/]: interval  +/-: zoom  f: fit  x: remove  esc: back")
	sb.WriteString(help + "\n\n")

	if m.loading {
		sb.WriteString(styleAxis.Render("  Loading..."))
		return sb.String()
	}

	if len(m.entries) == 0 {
		sb.WriteString(styleAxis.Render("  No data"))
		return sb.String()
	}

	chartHeight := m.height - 6
	chartWidth := m.plotWidth()
	if chartHeight < 5 {
		chartHeight = 5
	}
	if chartWidth < 1 {
		sb.WriteString(styleAxis.Render("  Terminal too small for chart"))
		return sb.String()
	}

	allSeries := m.buildSeries(m.visibleCount())
	if len(allSeries) == 0 {
		sb.WriteString(styleAxis.Render("  No valid data"))
		return sb.String()
	}

	// Global min/max across all series
	minP, maxP := math.Inf(1), math.Inf(-1)
	for _, s := range allSeries {
		for _, p := range s.pcts {
			if math.IsNaN(p) || math.IsInf(p, 0) {
				continue
			}
			if p < minP {
				minP = p
			}
			if p > maxP {
				maxP = p
			}
		}
	}
	if math.IsInf(minP, 1) {
		sb.WriteString(styleAxis.Render("  No valid data"))
		return sb.String()
	}
	scale := newPriceScale(minP, maxP, chartHeight)

	p := newPanel(chartWidth, chartHeight)

	// Zero line
	if zeroRow, ok := scale.row(0); ok {
		p.hline(zeroRow, refLine, theme.ColorDim, paintIfEmpty)
	}

	// Plot each series. All series share a length, so one column per
	// point with no stretching.
	for _, s := range allSeries {
		plotSeries(p, s.pcts, 1, scale, '●', s.color, paintOver)
	}

	every := chartHeight/5 + 1
	sb.WriteString(p.render(func(r int) string {
		if r%every != 0 {
			return ""
		}
		return fmt.Sprintf("%+.1f%%", scale.value(r))
	}))

	return sb.String()
}

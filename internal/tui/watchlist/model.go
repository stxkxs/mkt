package watchlist

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleVol = lipgloss.NewStyle().Foreground(theme.ColorYellow)

	styleSparkUp    = lipgloss.NewStyle().Foreground(theme.ColorGreen)
	styleSparkDown  = lipgloss.NewStyle().Foreground(theme.ColorRed)
	styleRangeTrack = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleRangeMark  = lipgloss.NewStyle().Foreground(theme.ColorAccent)
	styleSearch     = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleVol = lipgloss.NewStyle().Foreground(theme.ColorYellow)
	styleSparkUp = lipgloss.NewStyle().Foreground(theme.ColorGreen)
	styleSparkDown = lipgloss.NewStyle().Foreground(theme.ColorRed)
	styleRangeTrack = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleRangeMark = lipgloss.NewStyle().Foreground(theme.ColorAccent)
	styleSearch = lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true)
}

// Group is one named watchlist subset.
type Group struct {
	Name    string
	Symbols []string
}

// sortMode selects the row ordering. Quotes update live, so non-config
// orders are recomputed every render and rows can move as prices change.
type sortMode int

const (
	sortConfig sortMode = iota // config order
	sortChange                 // change% descending
	sortVolume                 // volume descending
	sortPrice                  // price descending
)

func (s sortMode) String() string {
	switch s {
	case sortChange:
		return "change"
	case sortVolume:
		return "volume"
	case sortPrice:
		return "price"
	}
	return "config"
}

// Model is the watchlist view.
type Model struct {
	groups    []Group
	activeIdx int
	symbols   []string // active group's symbols; mirrored from groups[activeIdx]
	quotes    map[string]provider.Quote
	cache     *market.Cache
	cursor    int // position in display order (see order())
	sortMode  sortMode
	width     int
	height    int

	// Search state
	searching   bool
	searchQuery string
	filtered    []int // indices into symbols matching query
	filterCur   int   // cursor within filtered
	preCursor   int   // cursor before search started (for restore on esc)
}

// New creates a watchlist model from one or more groups. The first group
// is active by default. A nil/empty groups slice falls back to a single
// "Default" group with no symbols.
func New(groups []Group, cache *market.Cache) Model {
	if len(groups) == 0 {
		groups = []Group{{Name: "Default"}}
	}
	return Model{
		groups:  groups,
		symbols: groups[0].Symbols,
		quotes:  make(map[string]provider.Quote),
		cache:   cache,
	}
}

// ActiveGroupName returns the name of the currently active group.
func (m Model) ActiveGroupName() string {
	if m.activeIdx < len(m.groups) {
		return m.groups[m.activeIdx].Name
	}
	return ""
}

// switchGroup advances to the next/prev group with wraparound and
// resyncs the cursor + cached symbols slice.
func (m *Model) switchGroup(delta int) {
	if len(m.groups) <= 1 {
		return
	}
	m.activeIdx = (m.activeIdx + delta + len(m.groups)) % len(m.groups)
	m.symbols = m.groups[m.activeIdx].Symbols
	m.cursor = 0
}

// Symbols returns the current symbol list.
func (m Model) Symbols() []string {
	return m.symbols
}

// order returns indices into m.symbols in display order. Config order is
// the identity; other modes sort descending by the quote field, with
// unquoted symbols last (stable, so they keep their config order).
func (m Model) order() []int {
	idx := make([]int, len(m.symbols))
	for i := range idx {
		idx[i] = i
	}
	if m.sortMode == sortConfig {
		return idx
	}
	key := func(i int) (float64, bool) {
		q, ok := m.quotes[m.symbols[i]]
		if !ok {
			return 0, false
		}
		switch m.sortMode {
		case sortVolume:
			return q.Volume, true
		case sortPrice:
			return q.Price, true
		default:
			return q.ChangePct, true
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		va, oka := key(idx[a])
		vb, okb := key(idx[b])
		if oka != okb {
			return oka
		}
		return va > vb
	})
	return idx
}

// posOf returns the display position of a symbols-slice index.
func (m Model) posOf(symIdx int) int {
	for pos, si := range m.order() {
		if si == symIdx {
			return pos
		}
	}
	return 0
}

// SelectedSymbol returns the currently selected symbol.
func (m Model) SelectedSymbol() string {
	ord := m.order()
	if m.cursor < len(ord) {
		return m.symbols[ord[m.cursor]]
	}
	return ""
}

// CurrentPrice returns the current price for a symbol.
func (m Model) CurrentPrice(sym string) float64 {
	if q, ok := m.quotes[sym]; ok {
		return q.Price
	}
	return 0
}

// Searching returns whether the watchlist is in search mode.
func (m Model) Searching() bool {
	return m.searching
}

// SearchQuery returns the current search query (empty if not searching).
func (m Model) SearchQuery() string {
	if m.searching {
		return m.searchQuery
	}
	return ""
}

// SetSize updates the viewport dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles messages for the watchlist.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case tea.KeyPressMsg:
		if m.searching {
			return m.updateSearch(msg)
		}
		switch msg.String() {
		case "/":
			m.searching = true
			m.searchQuery = ""
			m.preCursor = m.cursor
			m.filtered = m.computeFiltered("")
			m.filterCur = 0
			return m, nil
		case "j", "down":
			if m.cursor < len(m.symbols)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.symbols) > 0 {
				m.cursor = len(m.symbols) - 1
			}
		case "s":
			// Keep the selection on the same symbol across the re-sort.
			sym := m.SelectedSymbol()
			m.sortMode = (m.sortMode + 1) % 4
			if sym != "" {
				for pos, si := range m.order() {
					if m.symbols[si] == sym {
						m.cursor = pos
						break
					}
				}
			}
		case "[":
			m.switchGroup(-1)
		case "]":
			m.switchGroup(1)
		}
	case tea.MouseClickMsg:
		// The hit-test maps a mouse row straight to a data-row index, so it
		// only holds while rows are on screen. A frame too narrow for the
		// table draws none.
		if m.layout() == nil {
			return m, nil
		}
		row := msg.Y - m.headerLines()
		if row < 0 {
			return m, nil
		}
		startIdx := m.viewportStart()
		idx := startIdx + row
		if idx >= 0 && idx < len(m.symbols) {
			m.cursor = idx
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.symbols)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

func (m Model) updateSearch(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "enter":
		if len(m.filtered) > 0 && m.filterCur < len(m.filtered) {
			m.cursor = m.posOf(m.filtered[m.filterCur])
		}
		m.searching = false
		m.searchQuery = ""
		m.filtered = nil
		return m, nil
	case "esc":
		m.cursor = m.preCursor
		m.searching = false
		m.searchQuery = ""
		m.filtered = nil
		return m, nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.filtered = m.computeFiltered(m.searchQuery)
			m.filterCur = 0
		}
		return m, nil
	case "j", "down", "ctrl+n":
		if len(m.filtered) > 0 && m.filterCur < len(m.filtered)-1 {
			m.filterCur++
		}
		return m, nil
	case "k", "up", "ctrl+p":
		if m.filterCur > 0 {
			m.filterCur--
		}
		return m, nil
	default:
		// Append printable characters
		for _, r := range key {
			if unicode.IsPrint(r) && !unicode.IsControl(r) {
				m.searchQuery += string(r)
			}
		}
		m.filtered = m.computeFiltered(m.searchQuery)
		m.filterCur = 0
		return m, nil
	}
}

func (m Model) computeFiltered(query string) []int {
	if query == "" {
		result := make([]int, len(m.symbols))
		for i := range m.symbols {
			result[i] = i
		}
		return result
	}
	var result []int
	q := strings.ToLower(query)
	for i, sym := range m.symbols {
		if fuzzyMatch(strings.ToLower(sym), q) {
			result = append(result, i)
		}
	}
	return result
}

// fuzzyMatch checks if all chars in query appear in target in order.
func fuzzyMatch(target, query string) bool {
	ti := 0
	for _, qc := range query {
		found := false
		for ti < len(target) {
			tc := rune(target[ti])
			ti++
			if tc == qc {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	m.quotes[q.Symbol] = q
}

// headerLines counts the fixed rows above the first symbol row: column
// header + separator, plus the group/sort hint line when shown.
func (m Model) headerLines() int {
	if m.showHintLine() {
		return 3
	}
	return 2
}

// showHintLine reports whether the group-switcher / sort hint row renders.
func (m Model) showHintLine() bool {
	return len(m.groups) > 1 || m.sortMode != sortConfig
}

// visibleRows is how many symbol rows fit below the fixed header rows.
// Clamped to at least one: a content area shorter than the header rows
// yields a negative row budget, and treating that as "unlimited" made
// the watchlist paint its entire symbol list straight through the
// bottom of the frame.
func (m Model) visibleRows() int {
	return format.VisibleRows(m.height, m.headerLines(), len(m.symbols))
}

func (m Model) viewportStart() int {
	return format.ViewportStart(m.cursor, len(m.symbols), m.visibleRows())
}

// Column keys. The header, every row and the search view compose by
// switching on these, so the table's geometry is described once.
const (
	colSymbol = "symbol"
	colPrice  = "price"
	colChange = "change"
	colVol    = "vol"
	colRange  = "range"
	colTrend  = "trend"
)

// rowGutter is the cursor column that every row and the header lead with.
const rowGutter = 2

// columnPlan describes the table to format.Fit. Prio is the shedding
// order: TREND goes first, then RANGE, VOL and CHANGE, leaving SYMBOL and
// PRICE — the row identifier and the value being watched — for the
// narrowest frames that still render a table. VOL and RANGE carry no Min
// and so are dropped whole rather than squeezed: a volume figure and a
// glyph bar at half width are not smaller versions of themselves. TREND
// flexes, so the sparkline takes whatever the other columns leave.
func columnPlan() []format.Col {
	return []format.Col{
		{Key: colSymbol, Width: 12, Min: 6, Prio: 6},
		{Key: colPrice, Width: 12, Min: 8, Prio: 5},
		{Key: colChange, Width: 10, Min: 8, Prio: 4},
		{Key: colVol, Width: 8, Prio: 3},
		{Key: colRange, Width: 8, Prio: 2},
		{Key: colTrend, Width: 20, Min: 8, Prio: 1, Flex: true},
	}
}

// layout fits the plan to the frame. A nil Layout means not even the
// symbol column fits, and the view prints its too-narrow line instead of
// a partial table.
func (m Model) layout() format.Layout {
	return format.Fit(columnPlan(), m.width, rowGutter)
}

// cell pads s to exactly w display cells, truncating first so a column
// width is a ceiling as well as a floor. Padding happens before styling:
// fmt's width verbs count runes rather than cells, and count an ANSI
// escape as runes, which silently grows a styled cell past its column.
func cell(s string, w int, alignRight bool) string {
	s = format.Truncate(format.CellText(s), w)
	pad := format.Spaces(w - lipgloss.Width(s))
	if alignRight {
		return pad + s
	}
	return s + pad
}

// headerRow renders the column header from the same Layout the rows use.
func headerRow(lay format.Layout) string {
	cells := make([]string, 0, len(lay))
	for _, c := range lay {
		switch c.Key {
		case colSymbol:
			cells = append(cells, cell("SYMBOL", c.Width, false))
		case colPrice:
			cells = append(cells, cell("PRICE", c.Width, true))
		case colChange:
			cells = append(cells, cell("CHANGE", c.Width, true))
		case colVol:
			cells = append(cells, cell("VOL", c.Width, true))
		case colRange:
			cells = append(cells, cell("RANGE", c.Width, true))
		case colTrend:
			cells = append(cells, cell("TREND", c.Width, false))
		}
	}
	return theme.StyleHeader.Render(format.Spaces(rowGutter) + strings.Join(cells, " "))
}

// View renders the watchlist.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	lay := m.layout()
	if lay == nil {
		return theme.StyleDim.Render(format.Truncate("  Terminal too small for the watchlist.", m.width))
	}

	var sb strings.Builder

	// Search mode: show filtered results
	if m.searching {
		return m.viewSearch(lay)
	}

	// Hint line: group switcher and/or active sort mode.
	if m.showHintLine() {
		sb.WriteString(m.hintLine())
		sb.WriteString("\n")
	}

	// Header
	sb.WriteString(headerRow(lay))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
	sb.WriteString("\n")

	// Compute visible window below the fixed header rows.
	maxRows := m.visibleRows()
	startIdx := m.viewportStart()
	endIdx := startIdx + maxRows
	if endIdx > len(m.symbols) {
		endIdx = len(m.symbols)
	}

	// Rows, in display order
	ord := m.order()
	for pos := startIdx; pos < endIdx; pos++ {
		m.renderRow(&sb, ord[pos], pos == m.cursor, lay)
	}

	return sb.String()
}

// hintLine renders the group switcher and the active sort mode within the
// frame. Chrome is budgeted like a row: the content panel word-wraps a
// line it cannot fit, and a wrapped hint costs a symbol row and shifts
// every mouse row against the data-row index it maps to.
func (m Model) hintLine() string {
	var sb strings.Builder
	sb.WriteString(format.Spaces(rowGutter))
	budget := m.width - rowGutter
	write := func(text string, style func(string) string) {
		if budget <= 0 {
			return
		}
		t := format.Truncate(text, budget)
		budget -= lipgloss.Width(t)
		sb.WriteString(style(t))
	}

	dim := func(s string) string { return theme.StyleDim.Render(s) }

	if len(m.groups) > 1 {
		write(m.ActiveGroupName(), theme.StyleAccentText)
		write(fmt.Sprintf("  [/]: switch  (%d/%d)", m.activeIdx+1, len(m.groups)), dim)
	}
	if m.sortMode != sortConfig {
		write("  sort: ", dim)
		write(m.sortMode.String()+" ↓", theme.StyleAccentText)
		write("  s: cycle", dim)
	}
	return sb.String()
}

func (m Model) viewSearch(lay format.Layout) string {
	var sb strings.Builder

	// Search prompt, clipped so the caret still lands inside the frame.
	prompt := format.Truncate("  / "+m.searchQuery, m.width-1)
	sb.WriteString(styleSearch.Render(prompt))
	sb.WriteString(theme.StyleDim.Render("_"))
	sb.WriteString("\n")

	// Header
	sb.WriteString(headerRow(lay))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
	sb.WriteString("\n")

	if len(m.filtered) == 0 {
		sb.WriteString(theme.StyleDim.Render(format.Truncate("  No matches", m.width)))
		sb.WriteString("\n")
		return sb.String()
	}

	// Show filtered results below the prompt + header + separator rows.
	maxRows := format.VisibleRows(m.height, 3, len(m.filtered))
	startIdx := format.ViewportStart(m.filterCur, len(m.filtered), maxRows)
	endIdx := startIdx + maxRows
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
	}

	for fi := startIdx; fi < endIdx; fi++ {
		idx := m.filtered[fi]
		m.renderRow(&sb, idx, fi == m.filterCur, lay)
	}

	return sb.String()
}

func (m Model) renderRow(sb *strings.Builder, i int, selected bool, lay format.Layout) {
	sym := m.symbols[i]
	q, hasQuote := m.quotes[sym]

	// Cursor indicator
	cursor := format.Spaces(rowGutter)
	if selected {
		cursor = theme.StyleCursorGutter.Render("▎") + " "
	}

	cells := make([]string, 0, len(lay))
	for _, c := range lay {
		switch c.Key {
		case colSymbol:
			cells = append(cells, theme.StyleSymbol.Render(cell(sym, c.Width, false)))
		case colPrice:
			price := "—"
			if hasQuote {
				price = format.FormatPrice(q.Price)
			}
			cells = append(cells, cell(price, c.Width, true))
		case colChange:
			cells = append(cells, changeCell(q, hasQuote, c.Width))
		case colVol:
			if hasQuote && q.Volume > 0 {
				cells = append(cells, styleVol.Render(cell(format.FormatVolume(q.Volume), c.Width, true)))
			} else {
				cells = append(cells, theme.StyleNeutral.Render(cell("—", c.Width, true)))
			}
		case colRange:
			cells = append(cells, rangeCell(q, hasQuote, c.Width))
		case colTrend:
			cells = append(cells, m.trendCell(sym, q, hasQuote, c.Width))
		}
	}

	row := cursor + strings.Join(cells, " ")
	if selected {
		row = theme.StyleCursorRow.Bold(true).Render(row)
	}
	sb.WriteString(row)
	sb.WriteString("\n")
}

// changeCell renders the percentage move, padded to the column before the
// direction style is applied so one shape serves every path.
func changeCell(q provider.Quote, hasQuote bool, w int) string {
	if !hasQuote {
		return theme.StyleNeutral.Render(cell("—", w, true))
	}
	sign := "+"
	if q.ChangePct < 0 {
		sign = ""
	}
	style := theme.StyleNeutral
	if q.ChangePct > 0 {
		style = theme.StyleUp
	} else if q.ChangePct < 0 {
		style = theme.StyleDown
	}
	return style.Render(cell(fmt.Sprintf("%s%.2f%%", sign, q.ChangePct), w, true))
}

// rangeCell draws where the price sits between the day's low and high.
// The track is generated at the column's width, so a squeezed or widened
// column gets a bar built for it rather than one sliced from another size.
func rangeCell(q provider.Quote, hasQuote bool, w int) string {
	if !hasQuote || q.High24h <= 0 || q.Low24h <= 0 {
		return theme.StyleNeutral.Render(cell("—", w, true))
	}
	track, markerIdx := format.DayRange(q.Price, q.Low24h, q.High24h, w)
	runes := []rune(track)
	if markerIdx < 0 || markerIdx >= len(runes) {
		return styleRangeTrack.Render(cell(track, w, false))
	}
	return styleRangeTrack.Render(string(runes[:markerIdx])) +
		styleRangeMark.Render(string(runes[markerIdx])) +
		styleRangeTrack.Render(string(runes[markerIdx+1:]))
}

// trendCell renders the sparkline at the column's width. A braille rune is
// three bytes and one cell, so the cell is measured and padded in cells:
// format.BrailleSparkline already returns exactly w of them for a
// non-empty series, and an empty series pads to keep the row's geometry.
func (m Model) trendCell(sym string, q provider.Quote, hasQuote bool, w int) string {
	spark := cell(format.BrailleSparkline(m.cache.Prices(sym), w), w, false)
	if hasQuote && q.ChangePct >= 0 {
		return styleSparkUp.Render(spark)
	}
	return styleSparkDown.Render(spark)
}

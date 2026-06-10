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

func (m Model) viewportStart() int {
	return format.ViewportStart(m.cursor, len(m.symbols), m.height-m.headerLines())
}

const rangeWidth = 8

// View renders the watchlist.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	sparkWidth := 20
	var sb strings.Builder

	// Search mode: show filtered results
	if m.searching {
		return m.viewSearch(sparkWidth)
	}

	// Hint line: group switcher and/or active sort mode.
	if m.showHintLine() {
		sb.WriteString("  ")
		if len(m.groups) > 1 {
			sb.WriteString(theme.StyleAccentText(m.ActiveGroupName()))
			sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  [/]: switch  (%d/%d)", m.activeIdx+1, len(m.groups))))
		}
		if m.sortMode != sortConfig {
			sb.WriteString(theme.StyleDim.Render("  sort: "))
			sb.WriteString(theme.StyleAccentText(m.sortMode.String() + " ↓"))
			sb.WriteString(theme.StyleDim.Render("  s: cycle"))
		}
		sb.WriteString("\n")
	}

	// Header
	header := fmt.Sprintf("  %-12s %12s %10s %8s %*s  %-*s",
		"SYMBOL", "PRICE", "CHANGE", "VOL", rangeWidth, "RANGE", sparkWidth, "TREND")
	sb.WriteString(theme.StyleHeader.Render(header))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(strings.Repeat("─", m.width)))
	sb.WriteString("\n")

	// Compute visible window below the fixed header rows.
	maxRows := m.height - m.headerLines()
	if maxRows < 1 || maxRows >= len(m.symbols) {
		maxRows = len(m.symbols)
	}
	startIdx := m.viewportStart()
	endIdx := startIdx + maxRows
	if endIdx > len(m.symbols) {
		endIdx = len(m.symbols)
	}

	// Rows, in display order
	ord := m.order()
	for pos := startIdx; pos < endIdx; pos++ {
		m.renderRow(&sb, ord[pos], pos == m.cursor, sparkWidth)
	}

	return sb.String()
}

func (m Model) viewSearch(sparkWidth int) string {
	var sb strings.Builder

	// Search prompt
	sb.WriteString(styleSearch.Render(fmt.Sprintf("  / %s", m.searchQuery)))
	sb.WriteString(theme.StyleDim.Render("_"))
	sb.WriteString("\n")

	// Header
	header := fmt.Sprintf("  %-12s %12s %10s %8s %*s  %-*s",
		"SYMBOL", "PRICE", "CHANGE", "VOL", rangeWidth, "RANGE", sparkWidth, "TREND")
	sb.WriteString(theme.StyleHeader.Render(header))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(strings.Repeat("─", m.width)))
	sb.WriteString("\n")

	if len(m.filtered) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No matches"))
		sb.WriteString("\n")
		return sb.String()
	}

	// Show filtered results (max visible rows minus 3 for prompt + header + separator)
	maxRows := m.height - 3
	if maxRows < 1 {
		maxRows = 1
	}
	startIdx := format.ViewportStart(m.filterCur, len(m.filtered), maxRows)
	endIdx := startIdx + maxRows
	if endIdx > len(m.filtered) {
		endIdx = len(m.filtered)
	}

	for fi := startIdx; fi < endIdx; fi++ {
		idx := m.filtered[fi]
		m.renderRow(&sb, idx, fi == m.filterCur, sparkWidth)
	}

	return sb.String()
}

func (m Model) renderRow(sb *strings.Builder, i int, selected bool, sparkWidth int) {
	sym := m.symbols[i]
	q, hasQuote := m.quotes[sym]

	// Cursor indicator
	cursor := "  "
	if selected {
		cursor = theme.StyleCursorGutter.Render("▎") + " "
	}

	// Symbol
	symStr := theme.StyleSymbol.Render(fmt.Sprintf("%-12s", sym))

	// Price
	var priceStr, changeStr string
	var changeStyle lipgloss.Style
	if hasQuote {
		priceStr = fmt.Sprintf("%12s", format.FormatPrice(q.Price))
		sign := "+"
		if q.ChangePct < 0 {
			sign = ""
		}
		changeStr = fmt.Sprintf("%s%.2f%%", sign, q.ChangePct)
		if q.ChangePct > 0 {
			changeStyle = theme.StyleUp
		} else if q.ChangePct < 0 {
			changeStyle = theme.StyleDown
		} else {
			changeStyle = theme.StyleNeutral
		}
	} else {
		priceStr = fmt.Sprintf("%12s", "—")
		changeStr = fmt.Sprintf("%10s", "—")
		changeStyle = theme.StyleNeutral
	}

	// Volume
	var volStr string
	if hasQuote && q.Volume > 0 {
		volStr = styleVol.Render(fmt.Sprintf("%8s", format.FormatVolume(q.Volume)))
	} else {
		volStr = theme.StyleNeutral.Render(fmt.Sprintf("%8s", "—"))
	}

	// Day Range
	var rangeStr string
	if hasQuote && q.High24h > 0 && q.Low24h > 0 {
		track, markerIdx := format.DayRange(q.Price, q.Low24h, q.High24h, rangeWidth)
		if markerIdx >= 0 {
			runes := []rune(track)
			before := string(runes[:markerIdx])
			marker := string(runes[markerIdx : markerIdx+1])
			after := string(runes[markerIdx+1:])
			rangeStr = styleRangeTrack.Render(before) + styleRangeMark.Render(marker) + styleRangeTrack.Render(after)
		} else {
			rangeStr = styleRangeTrack.Render(track)
		}
	} else {
		rangeStr = theme.StyleNeutral.Render(fmt.Sprintf("%*s", rangeWidth, "—"))
	}

	// Sparkline (braille for higher resolution)
	prices := m.cache.Prices(sym)
	spark := format.BrailleSparkline(prices, sparkWidth)
	// Pad if needed
	for len(spark) < sparkWidth {
		spark += " "
	}
	var sparkStyled string
	if hasQuote && q.ChangePct >= 0 {
		sparkStyled = styleSparkUp.Render(spark)
	} else {
		sparkStyled = styleSparkDown.Render(spark)
	}

	row := fmt.Sprintf("%s%s %s %s %s %s  %s",
		cursor, symStr, priceStr,
		changeStyle.Render(fmt.Sprintf("%10s", changeStr)),
		volStr, rangeStr, sparkStyled)

	if selected {
		sb.WriteString(theme.StyleCursorRow.Bold(true).Render(row))
	} else {
		sb.WriteString(row)
	}
	sb.WriteString("\n")
}

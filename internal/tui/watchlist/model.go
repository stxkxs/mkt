package watchlist

import (
	"fmt"
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

	styleSparkUp   = lipgloss.NewStyle().Foreground(theme.ColorGreen)
	styleSparkDown = lipgloss.NewStyle().Foreground(theme.ColorRed)
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

// Model is the watchlist view.
type Model struct {
	symbols []string
	quotes  map[string]provider.Quote
	cache   *market.Cache
	cursor  int
	width   int
	height  int

	// Search state
	searching   bool
	searchQuery string
	filtered    []int // indices into symbols matching query
	filterCur   int   // cursor within filtered
	preCursor   int   // cursor before search started (for restore on esc)
}

// New creates a watchlist model.
func New(symbols []string, cache *market.Cache) Model {
	return Model{
		symbols: symbols,
		quotes:  make(map[string]provider.Quote),
		cache:   cache,
	}
}

// Symbols returns the current symbol list.
func (m Model) Symbols() []string {
	return m.symbols
}

// SelectedSymbol returns the currently selected symbol.
func (m Model) SelectedSymbol() string {
	if m.cursor < len(m.symbols) {
		return m.symbols[m.cursor]
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
		}
	case tea.MouseClickMsg:
		row := msg.Y - 1 // -1 for header
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
			m.cursor = m.filtered[m.filterCur]
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

func (m Model) viewportStart() int {
	maxRows := m.height - 1
	if maxRows < 1 || maxRows >= len(m.symbols) {
		return 0
	}
	startIdx := m.cursor - maxRows + 1
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+maxRows > len(m.symbols) {
		startIdx = len(m.symbols) - maxRows
	}
	return startIdx
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

	// Header
	header := fmt.Sprintf("  %-12s %12s %10s %8s %*s  %-*s",
		"SYMBOL", "PRICE", "CHANGE", "VOL", rangeWidth, "RANGE", sparkWidth, "TREND")
	sb.WriteString(theme.StyleHeader.Render(header))
	sb.WriteString("\n")

	// Compute visible window (1 row for header)
	maxRows := m.height - 1
	if maxRows < 1 || maxRows >= len(m.symbols) {
		maxRows = len(m.symbols)
	}
	startIdx := m.viewportStart()
	endIdx := startIdx + maxRows
	if endIdx > len(m.symbols) {
		endIdx = len(m.symbols)
	}

	// Rows
	for i := startIdx; i < endIdx; i++ {
		m.renderRow(&sb, i, i == m.cursor, sparkWidth)
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

	if len(m.filtered) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No matches"))
		sb.WriteString("\n")
		return sb.String()
	}

	// Show filtered results (max visible rows minus 2 for prompt + header)
	maxRows := m.height - 2
	if maxRows < 1 {
		maxRows = 1
	}
	startIdx := 0
	if len(m.filtered) > maxRows {
		startIdx = m.filterCur - maxRows + 1
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxRows > len(m.filtered) {
			startIdx = len(m.filtered) - maxRows
		}
	}
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
		cursor = theme.StyleCursor.Render("> ")
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

	// Sparkline
	prices := m.cache.Prices(sym)
	spark := format.Sparkline(prices, sparkWidth)
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
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(row))
	} else {
		sb.WriteString(row)
	}
	sb.WriteString("\n")
}

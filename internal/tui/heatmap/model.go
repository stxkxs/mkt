package heatmap

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// Color gradient: 9 discrete steps from red -> neutral -> green.
var gradientColors []color.Color

func initGradient() {
	gradientColors = []color.Color{
		lipgloss.Color("#ff0000"), // -5%
		lipgloss.Color("#cc3333"),
		lipgloss.Color("#994444"), // -2.5%
		lipgloss.Color("#775555"),
		lipgloss.Color("#555555"), // 0%
		lipgloss.Color("#557755"),
		lipgloss.Color("#449944"), // +2.5%
		lipgloss.Color("#33cc33"),
		lipgloss.Color("#00ff00"), // +5%
	}
}

func init() {
	initGradient()
}

func changeColor(pct float64) color.Color {
	if len(gradientColors) == 0 {
		initGradient()
	}
	idx := int((pct + 5) / 10 * 8)
	if idx < 0 {
		idx = 0
	}
	if idx > 8 {
		idx = 8
	}
	return gradientColors[idx]
}

// Model is the sector heatmap tab with drill-down.
type Model struct {
	sectors    []Sector
	quotes     map[string]provider.Quote
	cursor     int // sector cursor (overview) or stock cursor (drilldown)
	sectorIdx  int // which sector we're drilled into (-1 = overview)
	width      int
	height     int
}

// New creates a heatmap model.
func New() Model {
	return Model{
		sectors:   DefaultSectors,
		quotes:    make(map[string]provider.Quote),
		sectorIdx: -1,
	}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	m.quotes[q.Symbol] = q
}

// RebuildStyles is a no-op since heatmap uses dynamic gradient colors.
func RebuildStyles() {}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.sectorIdx >= 0 {
			return m.updateDrilldown(msg)
		}
		return m.updateOverview(msg)
	}
	return m, nil
}

func (m Model) updateOverview(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(m.sectors)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "l":
		m.cursor += 3
		if m.cursor >= len(m.sectors) {
			m.cursor = len(m.sectors) - 1
		}
	case "h":
		m.cursor -= 3
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "enter":
		if m.cursor < len(m.sectors) {
			m.sectorIdx = m.cursor
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) updateDrilldown(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	sect := m.sectors[m.sectorIdx]
	cols := m.tileCols()
	switch msg.String() {
	case "esc":
		m.cursor = m.sectorIdx
		m.sectorIdx = -1
	case "j", "down":
		m.cursor += cols
		if m.cursor >= len(sect.Symbols) {
			m.cursor = len(sect.Symbols) - 1
		}
	case "k", "up":
		m.cursor -= cols
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "l":
		if m.cursor < len(sect.Symbols)-1 {
			m.cursor++
		}
	case "h":
		if m.cursor > 0 {
			m.cursor--
		}
	}
	return m, nil
}

func (m Model) tileCols() int {
	tileW := 14
	cols := (m.width - 4) / tileW
	if cols < 1 {
		cols = 1
	}
	return cols
}

// sectorChange computes the average change% for a sector.
func (m Model) sectorChange(s Sector) float64 {
	var sum float64
	var count int
	for _, sym := range s.Symbols {
		if q, ok := m.quotes[sym]; ok && q.Price > 0 {
			sum += q.ChangePct
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// View renders the heatmap.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.sectorIdx >= 0 {
		return m.viewDrilldown()
	}
	return m.viewOverview()
}

// ── Overview: sector treemap ─────────────────────────────────────────

func (m Model) viewOverview() string {
	var sb strings.Builder
	sb.WriteString(theme.SectionHeader("Sector Heatmap", m.width))
	sb.WriteString(theme.StyleDim.Render("  j/k/h/l:nav  enter:drill down"))
	sb.WriteString("\n\n")

	if len(m.quotes) == 0 {
		sb.WriteString(theme.StyleDim.Render("  Waiting for market data..."))
		return sb.String()
	}

	chartH := m.height - 4
	chartW := m.width - 2
	if chartH < 3 || chartW < 10 {
		sb.WriteString(theme.StyleDim.Render("  Terminal too small"))
		return sb.String()
	}

	rects := layoutTreemap(len(m.sectors), chartW, chartH)

	grid := make([][]rune, chartH)
	gridColors := make([][]color.Color, chartH)
	gridBold := make([][]bool, chartH)
	for r := range chartH {
		grid[r] = make([]rune, chartW)
		gridColors[r] = make([]color.Color, chartW)
		gridBold[r] = make([]bool, chartW)
		for c := range chartW {
			grid[r][c] = ' '
		}
	}

	for i, s := range m.sectors {
		if i >= len(rects) {
			break
		}
		rect := rects[i]
		chg := m.sectorChange(s)
		clr := changeColor(chg)
		isSelected := i == m.cursor

		// Fill
		for r := rect.Y; r < rect.Y+rect.H && r < chartH; r++ {
			for c := rect.X; c < rect.X+rect.W && c < chartW; c++ {
				grid[r][c] = '░'
				gridColors[r][c] = clr
			}
		}

		// Border for selected
		if isSelected {
			bc := theme.ColorAccent
			for c := rect.X; c < rect.X+rect.W && c < chartW; c++ {
				if rect.Y < chartH {
					grid[rect.Y][c] = '─'
					gridColors[rect.Y][c] = bc
				}
				if botR := rect.Y + rect.H - 1; botR >= 0 && botR < chartH {
					grid[botR][c] = '─'
					gridColors[botR][c] = bc
				}
			}
			for r := rect.Y; r < rect.Y+rect.H && r < chartH; r++ {
				if rect.X < chartW {
					grid[r][rect.X] = '│'
					gridColors[r][rect.X] = bc
				}
				if rc := rect.X + rect.W - 1; rc >= 0 && rc < chartW {
					grid[r][rc] = '│'
					gridColors[r][rc] = bc
				}
			}
		}

		// Label
		nameRow := rect.Y
		if isSelected {
			nameRow = rect.Y + 1
		}
		if nameRow < chartH && rect.W >= 3 {
			label := fmt.Sprintf("%s %.1f%%", s.Name, chg)
			if len(label) > rect.W-2 {
				label = s.Name
				if len(label) > rect.W-2 {
					label = label[:rect.W-2]
				}
			}
			startC := rect.X + (rect.W-len(label))/2
			for ci, ch := range label {
				c := startC + ci
				if c >= rect.X && c < rect.X+rect.W && c < chartW {
					grid[nameRow][c] = ch
					gridColors[nameRow][c] = clr
					gridBold[nameRow][c] = true
				}
			}
		}

		// Tickers
		tickerRow := nameRow + 1
		if tickerRow < rect.Y+rect.H && tickerRow < chartH && rect.W >= 5 {
			var tickers []string
			for _, sym := range s.Symbols {
				if q, ok := m.quotes[sym]; ok && q.Price > 0 {
					tickers = append(tickers, sym)
				}
			}
			tickerLine := strings.Join(tickers, " ")
			if len(tickerLine) > rect.W-2 {
				tickerLine = tickerLine[:rect.W-2]
			}
			startC := rect.X + 1
			for ci, ch := range tickerLine {
				c := startC + ci
				if c < rect.X+rect.W && c < chartW {
					grid[tickerRow][c] = ch
					gridColors[tickerRow][c] = clr
				}
			}
		}
	}

	sb.WriteString(" ")
	for r := range chartH {
		for c := range chartW {
			ch := grid[r][c]
			clr := gridColors[r][c]
			if clr == nil {
				sb.WriteRune(ch)
			} else {
				s := lipgloss.NewStyle().Foreground(clr)
				if gridBold[r][c] {
					s = s.Bold(true)
				}
				sb.WriteString(s.Render(string(ch)))
			}
		}
		sb.WriteString("\n ")
	}
	return sb.String()
}

// ── Drilldown: individual stock tiles within a sector ────────────────

func (m Model) viewDrilldown() string {
	sect := m.sectors[m.sectorIdx]
	sectorChg := m.sectorChange(sect)

	var sb strings.Builder

	// Header: sector name + change + breadcrumb
	sectorColor := changeColor(sectorChg)
	title := lipgloss.NewStyle().Foreground(sectorColor).Bold(true).
		Render(fmt.Sprintf("  %s", sect.Name))
	chgStr := fmt.Sprintf(" %.2f%%", sectorChg)
	chgStyle := theme.StyleUp
	if sectorChg < 0 {
		chgStyle = theme.StyleDown
	}
	sb.WriteString(title)
	sb.WriteString(chgStyle.Render(chgStr))
	sb.WriteString(theme.StyleDim.Render("  esc:back  j/k/h/l:nav"))
	sb.WriteString("\n")

	// Separator
	sb.WriteString(theme.StyleDim.Render("  " + strings.Repeat("─", m.width-4)))
	sb.WriteString("\n\n")

	if len(m.quotes) == 0 {
		sb.WriteString(theme.StyleDim.Render("  Waiting for data..."))
		return sb.String()
	}

	// Sort symbols by change% descending
	type stockEntry struct {
		sym   string
		quote provider.Quote
		has   bool
	}
	entries := make([]stockEntry, len(sect.Symbols))
	for i, sym := range sect.Symbols {
		q, ok := m.quotes[sym]
		entries[i] = stockEntry{sym: sym, quote: q, has: ok}
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].has {
			return false
		}
		if !entries[j].has {
			return true
		}
		return entries[i].quote.ChangePct > entries[j].quote.ChangePct
	})

	// Tile layout: each tile is a fixed-width card
	tileW := 22
	cols := (m.width - 4) / tileW
	if cols < 1 {
		cols = 1
	}

	tileH := 4 // rows per tile (symbol+price, change, bar, blank)
	maxRows := (m.height - 5) / tileH
	if maxRows < 1 {
		maxRows = 1
	}
	maxVisible := maxRows * cols
	if maxVisible > len(entries) {
		maxVisible = len(entries)
	}

	// Scroll offset
	cursorRow := m.cursor / cols
	viewStart := 0
	if cursorRow >= maxRows {
		viewStart = (cursorRow - maxRows + 1) * cols
	}
	viewEnd := viewStart + maxVisible
	if viewEnd > len(entries) {
		viewEnd = len(entries)
	}

	// Render tiles row by row
	row := 0
	for idx := viewStart; idx < viewEnd; idx += cols {
		rowEnd := idx + cols
		if rowEnd > viewEnd {
			rowEnd = viewEnd
		}
		chunk := entries[idx:rowEnd]

		// Line 1: symbol + price
		var line1 []string
		for ci, e := range chunk {
			globalIdx := idx + ci
			sel := globalIdx == m.cursor

			symStyle := lipgloss.NewStyle().Foreground(theme.ColorCyan).Bold(true)
			if sel {
				symStyle = symStyle.Background(theme.ColorAccent).Foreground(theme.ColorBg)
			}

			var priceStr string
			if e.has && e.quote.Price > 0 {
				priceStr = format.FormatPrice(e.quote.Price)
			} else {
				priceStr = "—"
			}

			cell := fmt.Sprintf(" %s %s",
				symStyle.Render(fmt.Sprintf("%-6s", e.sym)),
				lipgloss.NewStyle().Foreground(theme.ColorFg).Render(fmt.Sprintf("%9s", priceStr)),
			)
			padding := tileW - lipgloss.Width(cell)
			if padding > 0 {
				cell += strings.Repeat(" ", padding)
			}
			line1 = append(line1, cell)
		}
		sb.WriteString("  " + strings.Join(line1, " "))
		sb.WriteString("\n")

		// Line 2: change% with colored bar
		var line2 []string
		for _, e := range chunk {
			if !e.has || e.quote.Price == 0 {
				line2 = append(line2, strings.Repeat(" ", tileW))
				continue
			}
			pct := e.quote.ChangePct
			clr := changeColor(pct)
			sign := "+"
			if pct < 0 {
				sign = ""
			}

			chg := fmt.Sprintf("%s%.2f%%", sign, pct)
			chgRendered := lipgloss.NewStyle().Foreground(clr).Render(fmt.Sprintf(" %-8s", chg))

			// Mini bar: fill proportional to |pct| out of 5%, max 10 chars
			barLen := int(abs(pct) / 5.0 * 10)
			if barLen > 10 {
				barLen = 10
			}
			if barLen < 1 && pct != 0 {
				barLen = 1
			}
			bar := lipgloss.NewStyle().Foreground(clr).Render(strings.Repeat("█", barLen))

			cell := chgRendered + bar
			w := lipgloss.Width(cell)
			if w < tileW {
				cell += strings.Repeat(" ", tileW-w)
			}
			line2 = append(line2, cell)
		}
		sb.WriteString("  " + strings.Join(line2, " "))
		sb.WriteString("\n")

		// Line 3: volume
		var line3 []string
		for _, e := range chunk {
			if !e.has || e.quote.Volume == 0 {
				line3 = append(line3, strings.Repeat(" ", tileW))
				continue
			}
			vol := format.FormatVolume(e.quote.Volume)
			cell := theme.StyleDim.Render(fmt.Sprintf(" Vol: %-12s", vol))
			w := lipgloss.Width(cell)
			if w < tileW {
				cell += strings.Repeat(" ", tileW-w)
			}
			line3 = append(line3, cell)
		}
		sb.WriteString("  " + strings.Join(line3, " "))
		sb.WriteString("\n\n")

		row++
		if row >= maxRows {
			break
		}
	}

	// Scrollbar hint
	total := len(entries)
	if total > maxVisible {
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  showing %d-%d of %d", viewStart+1, viewEnd, total)))
		sb.WriteString("\n")
	}

	return sb.String()
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

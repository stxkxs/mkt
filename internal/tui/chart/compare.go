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

func compareColorList() []color.Color {
	return []color.Color{theme.ColorCyan, theme.ColorYellow, theme.ColorMagenta}
}

// CompareEntry holds data for one comparison symbol.
type CompareEntry struct {
	Symbol string
	Data   []provider.OHLCV
}

// compareLoadedMsg is sent when comparison data arrives.
type compareLoadedMsg struct {
	entries []CompareEntry
}

// CompareModel is the multi-symbol comparison chart.
type CompareModel struct {
	entries      []CompareEntry
	symbols      []string // symbols to compare (up to 3)
	zoom         int
	intervalIdx  int
	width        int
	height       int
	active       bool
	histProvider HistoryProvider
	loading      bool
}

// NewCompare creates a comparison model.
func NewCompare(hp HistoryProvider) CompareModel {
	return CompareModel{
		zoom:         50,
		intervalIdx:  5,
		histProvider: hp,
	}
}

// Active returns whether comparison chart is showing.
func (m CompareModel) Active() bool {
	return m.active
}

// AddSymbol adds a symbol to the comparison set (max 3).
func (m *CompareModel) AddSymbol(sym string) {
	for _, s := range m.symbols {
		if s == sym {
			return
		}
	}
	if len(m.symbols) >= 3 {
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

func (m *CompareModel) fetchAll() tea.Cmd {
	syms := make([]string, len(m.symbols))
	copy(syms, m.symbols)
	interval := intervals[m.intervalIdx]
	hp := m.histProvider
	if hp == nil {
		return nil
	}

	return func() tea.Msg {
		var mu sync.Mutex
		var entries []CompareEntry
		var wg sync.WaitGroup

		for _, sym := range syms {
			wg.Add(1)
			go func(s string) {
				defer wg.Done()
				data, err := hp.History(context.Background(), provider.HistoryParams{
					Symbol:   s,
					Interval: interval,
					Limit:    200,
				})
				if err != nil {
					return
				}
				mu.Lock()
				entries = append(entries, CompareEntry{Symbol: s, Data: data})
				mu.Unlock()
			}(sym)
		}
		wg.Wait()
		return compareLoadedMsg{entries: entries}
	}
}

// SetSize updates dimensions.
func (m *CompareModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Update handles messages.
func (m CompareModel) Update(msg tea.Msg) (CompareModel, tea.Cmd) {
	switch msg := msg.(type) {
	case compareLoadedMsg:
		m.entries = msg.entries
		m.loading = false
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.active = false
			return m, nil
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
				return m, m.fetchAll()
			}
		case "]":
			if m.intervalIdx < len(intervals)-1 {
				m.intervalIdx++
				return m, m.fetchAll()
			}
		case "x":
			if len(m.symbols) > 0 {
				m.symbols = m.symbols[:len(m.symbols)-1]
				if len(m.symbols) == 0 {
					m.active = false
				}
				return m, m.fetchAll()
			}
		}
	}
	return m, nil
}

// View renders the comparison chart.
func (m CompareModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder
	colors := compareColorList()

	// Title with legend
	interval := intervals[m.intervalIdx]
	title := styleTitle.Render(fmt.Sprintf("  Compare  %s", interval))
	var legend []string
	for i, sym := range m.symbols {
		clr := colors[i%len(colors)]
		legend = append(legend, lipgloss.NewStyle().Foreground(clr).Render("● "+sym))
	}
	sb.WriteString(title + "  " + strings.Join(legend, "  "))
	sb.WriteString("\n")
	help := styleAxis.Render("  [/]: interval  +/-: zoom  x: remove  esc: back")
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
	chartWidth := m.width - 14
	if chartHeight < 5 {
		chartHeight = 5
	}
	if chartWidth < 10 {
		chartWidth = 10
	}

	// Normalize all series to % change from first visible candle
	type series struct {
		symbol string
		pcts   []float64
		color  color.Color
	}

	var allSeries []series
	for i, entry := range m.entries {
		data := entry.Data
		if len(data) > m.zoom {
			data = data[len(data)-m.zoom:]
		}
		if len(data) == 0 {
			continue
		}
		base := data[0].Close
		if base == 0 {
			continue
		}
		pcts := make([]float64, len(data))
		for j, d := range data {
			pcts[j] = (d.Close - base) / base * 100
		}
		allSeries = append(allSeries, series{
			symbol: entry.Symbol,
			pcts:   pcts,
			color:  colors[i%len(colors)],
		})
	}

	if len(allSeries) == 0 {
		sb.WriteString(styleAxis.Render("  No valid data"))
		return sb.String()
	}

	// Find global min/max across all series
	minP := math.MaxFloat64
	maxP := -math.MaxFloat64
	maxLen := 0
	for _, s := range allSeries {
		if len(s.pcts) > maxLen {
			maxLen = len(s.pcts)
		}
		for _, p := range s.pcts {
			if p < minP {
				minP = p
			}
			if p > maxP {
				maxP = p
			}
		}
	}
	priceRange := maxP - minP
	if priceRange == 0 {
		priceRange = 1
	}

	// Build grid
	grid := make([][]rune, chartHeight)
	gridColor := make([][]color.Color, chartHeight)
	for r := range chartHeight {
		grid[r] = make([]rune, chartWidth)
		gridColor[r] = make([]color.Color, chartWidth)
		for c := range chartWidth {
			grid[r][c] = ' '
		}
	}

	// Draw zero line
	zeroRow := chartHeight - 1 - int((0-minP)/priceRange*float64(chartHeight-1))
	zeroRow = clampRow(zeroRow, chartHeight)
	for c := range chartWidth {
		if grid[zeroRow][c] == ' ' {
			grid[zeroRow][c] = '┄'
			gridColor[zeroRow][c] = theme.ColorDim
		}
	}

	// Plot each series
	for _, s := range allSeries {
		for i, p := range s.pcts {
			col := i * chartWidth / maxLen
			if col >= chartWidth {
				break
			}
			row := chartHeight - 1 - int((p-minP)/priceRange*float64(chartHeight-1))
			row = clampRow(row, chartHeight)
			grid[row][col] = '●'
			gridColor[row][col] = s.color
		}
	}

	// Render
	labelWidth := 10
	for r := range chartHeight {
		if r%(chartHeight/5+1) == 0 {
			pct := maxP - float64(r)/float64(chartHeight)*priceRange
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", labelWidth, fmt.Sprintf("%+.1f%%", pct))))
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

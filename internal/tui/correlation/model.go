// Package correlation renders a Pearson correlation matrix between the
// watchlist symbols using each symbol's recent prices from the market
// cache.
package correlation

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// Model is the correlation-matrix tab.
type Model struct {
	symbols []string
	cache   *market.Cache
	width   int
	height  int
}

// New constructs a Model with the watchlist symbols + price cache.
func New(symbols []string, cache *market.Cache) Model {
	return Model{symbols: symbols, cache: cache}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// RebuildStyles refreshes any cached styles. No-op here; theme broadcast
// compatibility.
func RebuildStyles() {}

// Update handles messages. The view is read-only so non-theme messages
// are ignored.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
	}
	return m, nil
}

// View renders the correlation matrix as a colored grid. Top-left
// corner is empty; first row + first column are symbol headers.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.SectionHeader("Correlation Matrix (watchlist)", m.width))
	sb.WriteString("\n\n")

	if len(m.symbols) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No symbols in watchlist."))
		return sb.String()
	}

	// Cap the rendered grid to keep rows on screen.
	cols := (m.width - 8) / 7
	if cols < 2 {
		cols = 2
	}
	syms := m.symbols
	if len(syms) > cols {
		syms = syms[:cols]
	}
	rows := m.height - 5
	if rows < 2 {
		rows = 2
	}
	if len(syms) > rows {
		syms = syms[:rows]
	}

	prices := make([][]float64, len(syms))
	for i, s := range syms {
		prices[i] = m.cache.Prices(s)
	}
	matrix := portfolio.CorrelationMatrix(syms, prices)

	// Column header
	sb.WriteString("        ")
	for _, s := range syms {
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("%6s ", trimSym(s))))
	}
	sb.WriteString("\n")
	for i, row := range syms {
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("%-7s ", trimSym(row))))
		for j := range syms {
			c := matrix[i][j]
			cell := "  —  "
			if !math.IsNaN(c) {
				cell = fmt.Sprintf("%+.2f", c)
			}
			sb.WriteString(colorize(c).Render(fmt.Sprintf("%6s ", cell)))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	if len(syms) < len(m.symbols) {
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf("  showing %d of %d symbols (enlarge terminal for more)", len(syms), len(m.symbols))))
		sb.WriteString("\n")
	}
	sb.WriteString(theme.StyleDim.Render("  Positive = green; negative = red; intensity by magnitude."))
	return sb.String()
}

func trimSym(s string) string {
	if len(s) > 6 {
		return s[:6]
	}
	return s
}

// colorize maps a correlation value to a styled cell.
func colorize(c float64) lipgloss.Style {
	if math.IsNaN(c) {
		return theme.StyleDim
	}
	abs := math.Abs(c)
	switch {
	case abs >= 0.7 && c > 0:
		return theme.StyleUp.Bold(true)
	case abs >= 0.7 && c < 0:
		return theme.StyleDown.Bold(true)
	case abs >= 0.3 && c > 0:
		return theme.StyleUp
	case abs >= 0.3 && c < 0:
		return theme.StyleDown
	}
	return theme.StyleDim
}

// Package correlation renders a correlation matrix between the watchlist
// symbols. It correlates each symbol's LOG RETURNS resampled onto a
// shared time grid, not the raw price levels the tick ring stores —
// levels make any two symbols that merely drifted the same way read as
// near-perfectly correlated, and index-aligning a 5-second crypto stream
// against a 15-second stock poll compares different spans of time.
package correlation

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// buckets are the resampling intervals the tab cycles through with `b`.
// Each must be at least as coarse as the slowest feed (stocks poll every
// 15s) or the slower series carries forward across whole buckets and its
// returns read as zero.
var buckets = []time.Duration{
	30 * time.Second,
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// Grid geometry: the row-label gutter and the cells each column needs.
const (
	labelWidth = 8
	cellWidth  = 7
	symWidth   = 6
	// chrome is the section header, the blank row under it, the column
	// header, and the two footer rows.
	chrome = 5
)

// Model is the correlation-matrix tab.
type Model struct {
	symbols []string
	cache   *market.Cache
	offset  int // first symbol in the visible window
	bucketI int
	width   int
	height  int
}

// New constructs a Model with the watchlist symbols + price cache.
func New(symbols []string, cache *market.Cache) Model {
	return Model{symbols: symbols, cache: cache}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetSymbols replaces the symbol universe and rewinds the window.
func (m *Model) SetSymbols(symbols []string) {
	m.symbols = symbols
	m.offset = 0
}

// Bucket returns the resampling interval currently used to align series.
func (m Model) Bucket() time.Duration { return buckets[m.bucketI%len(buckets)] }

// visible returns how many symbols fit on screen. The matrix is square,
// so the same count bounds both axes: whichever of width or height runs
// out first decides. Zero means not even one row and column fit, which
// the view reports rather than painting a grid past its own frame.
func (m Model) visible() int {
	cols := (m.width - labelWidth) / cellWidth
	rows := m.height - chrome
	if cols < 1 || rows < 1 {
		return 0
	}
	return format.VisibleRows(min(cols, rows), 0, len(m.symbols))
}

// window returns the symbols currently on screen and the index they
// start at. The window scrolls, so symbols that do not fit stay
// reachable instead of being silently dropped off the end of the list.
func (m Model) window() ([]string, int) {
	n := m.visible()
	if n == 0 {
		return nil, 0
	}
	start := m.clamp(m.offset)
	return m.symbols[start : start+n], start
}

// Update handles messages. The matrix itself is read-only; the keys move
// the visible window over the symbol list and change the resampling
// bucket. The tab reads theme colors at render time, so a theme change
// needs no cached-style rebuild.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down", "l", "]":
			m.offset = m.clamp(m.offset + 1)
		case "k", "up", "h", "[":
			m.offset = m.clamp(m.offset - 1)
		case "pgdown":
			m.offset = m.clamp(m.offset + m.visible())
		case "pgup":
			m.offset = m.clamp(m.offset - m.visible())
		case "g", "home":
			m.offset = 0
		case "G", "end":
			m.offset = m.clamp(len(m.symbols))
		case "b":
			m.bucketI = (m.bucketI + 1) % len(buckets)
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.offset = m.clamp(m.offset - 1)
		case tea.MouseWheelDown:
			m.offset = m.clamp(m.offset + 1)
		}
	}
	return m, nil
}

// clamp keeps the window offset inside the symbol list.
func (m Model) clamp(v int) int {
	maxOff := len(m.symbols) - m.visible()
	if maxOff < 0 {
		maxOff = 0
	}
	if v > maxOff {
		v = maxOff
	}
	if v < 0 {
		v = 0
	}
	return v
}

// samples reads each symbol's timestamped tick history out of the cache
// in the form the alignment helpers want.
func (m Model) samples(syms []string) [][]portfolio.Sample {
	out := make([][]portfolio.Sample, len(syms))
	for i, s := range syms {
		prices, times := m.cache.Series(s)
		out[i] = portfolio.SamplesFrom(prices, times)
	}
	return out
}

// matrixFor computes the correlation matrix for a set of symbols: log
// returns of each series after resampling them onto a shared bucket grid.
func (m Model) matrixFor(syms []string) [][]float64 {
	return portfolio.CorrelationMatrixSeries(syms, m.samples(syms), m.Bucket())
}

// View renders the correlation matrix as a colored grid. Top-left
// corner is empty; first row + first column are symbol headers.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(theme.SectionHeaderHint("Correlation Matrix (watchlist)",
		fmt.Sprintf("log returns @ %s  h/l:scroll  b:bucket", m.Bucket()), m.width))
	sb.WriteString("\n\n")

	if len(m.symbols) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No symbols in watchlist."))
		return sb.String()
	}

	syms, start := m.window()
	if len(syms) == 0 {
		sb.WriteString(theme.StyleDim.Render("  Terminal too small for the matrix."))
		return sb.String()
	}

	matrix := m.matrixFor(syms)

	// Column header
	sb.WriteString(format.Spaces(labelWidth))
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
		sb.WriteString(theme.StyleDim.Render(fmt.Sprintf(
			"  showing %d-%d of %d symbols  ·  h/l or [/] to scroll, g/G for ends",
			start+1, start+len(syms), len(m.symbols))))
		sb.WriteString("\n")
	}
	sb.WriteString(theme.StyleDim.Render("  Positive = green; negative = red; intensity by magnitude."))
	return sb.String()
}

// trimSym shortens a symbol to the header cell width without splitting a
// multibyte character.
func trimSym(s string) string {
	r := []rune(s)
	if len(r) > symWidth {
		return string(r[:symWidth])
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

package chart

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// gridLabelWidth is the column count of the axis-label gutter printed to
// the left of every grid row. Labels are right-aligned inside it and one
// space separates the gutter from the plot, so a rendered row spans
// gridLabelWidth+1+width columns. It is also what translates a hover
// column in terminal coordinates back into a plot column.
//
// This is the single source of truth for the gutter width; nothing in
// the package may redeclare it locally.
const gridLabelWidth = 10

// refLine is the glyph used for reference levels (RSI 30/70, MACD zero,
// pivots). Series plotted with paintIfClear are allowed to draw over it
// because a level line carries less information than the series itself.
const refLine = '┄'

// repeat is strings.Repeat with a negative-count guard. Panel and
// gutter widths are derived from terminal dimensions that routinely go
// negative on a narrow window, and strings.Repeat panics on a negative
// count, so every dimension-derived repeat goes through here.
func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// clampRow pins a row index inside a grid of the given height.
func clampRow(r, height int) int {
	if r < 0 {
		return 0
	}
	if r >= height {
		return height - 1
	}
	return r
}

// paintMode selects how a plotted cell interacts with what is already
// on the grid.
type paintMode int

const (
	// paintOver overwrites whatever occupies the cell.
	paintOver paintMode = iota
	// paintIfEmpty leaves any existing glyph alone.
	paintIfEmpty
	// paintIfClear overwrites blanks and reference lines only.
	paintIfClear
)

// panel is a fixed-size character grid with a per-cell foreground color
// and an axis gutter. Every accessor is bounds-checked: rows and columns
// are computed from terminal dimensions and indicator values, and neither
// is trustworthy enough to index a slice with directly.
type panel struct {
	cells  [][]rune
	colors [][]color.Color
	w, h   int
}

// newPanel allocates a blank w×h panel. Negative dimensions collapse to
// zero, yielding a panel that renders as the empty string.
func newPanel(w, h int) *panel {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	p := &panel{
		cells:  make([][]rune, h),
		colors: make([][]color.Color, h),
		w:      w,
		h:      h,
	}
	for r := range p.cells {
		p.cells[r] = make([]rune, w)
		p.colors[r] = make([]color.Color, w)
		for c := range p.cells[r] {
			p.cells[r][c] = ' '
		}
	}
	return p
}

// inBounds reports whether the cell exists.
func (p *panel) inBounds(row, col int) bool {
	return row >= 0 && row < p.h && col >= 0 && col < p.w
}

// paint writes a glyph subject to the given mode. Out-of-bounds writes
// are dropped.
func (p *panel) paint(row, col int, ch rune, clr color.Color, mode paintMode) {
	if !p.inBounds(row, col) {
		return
	}
	switch mode {
	case paintIfEmpty:
		if p.cells[row][col] != ' ' {
			return
		}
	case paintIfClear:
		if cur := p.cells[row][col]; cur != ' ' && cur != refLine {
			return
		}
	case paintOver:
	}
	p.cells[row][col] = ch
	p.colors[row][col] = clr
}

// hline fills an entire row.
func (p *panel) hline(row int, ch rune, clr color.Color, mode paintMode) {
	for c := range p.w {
		p.paint(row, c, ch, clr, mode)
	}
}

// vline fills an entire column.
func (p *panel) vline(col int, ch rune, clr color.Color, mode paintMode) {
	for r := range p.h {
		p.paint(r, col, ch, clr, mode)
	}
}

// render draws the panel one row at a time. label is called once per row
// and returns the axis text for that row, or "" for a blank gutter.
//
// This is the only place in the package that emits a grid; the sub-panel
// and main-chart renderers all funnel through it so they cannot drift
// apart in gutter width or color handling.
func (p *panel) render(label func(row int) string) string {
	if p.w < 1 || p.h < 1 {
		return ""
	}
	var sb strings.Builder
	blank := repeat(" ", gridLabelWidth+1)
	styles := make(map[colorKey]lipgloss.Style, 8)

	for r := range p.h {
		var txt string
		if label != nil {
			txt = label(r)
		}
		if txt == "" {
			sb.WriteString(blank)
		} else {
			sb.WriteString(styleAxis.Render(fmt.Sprintf("%*s ", gridLabelWidth, txt)))
		}

		// Cells are emitted in runs of a single color rather than one
		// styled Render per cell: a full-screen grid holds thousands of
		// colored cells and the chart redraws on every quote, so the
		// per-cell style build used to dominate the frame.
		var buf []rune
		var curColor color.Color
		var curKey colorKey
		flush := func() {
			if len(buf) == 0 {
				return
			}
			if curColor == nil {
				sb.WriteString(string(buf))
			} else {
				st, ok := styles[curKey]
				if !ok {
					st = lipgloss.NewStyle().Foreground(curColor)
					styles[curKey] = st
				}
				sb.WriteString(st.Render(string(buf)))
			}
			buf = buf[:0]
		}
		for c := range p.w {
			ch := p.cells[r][c]
			clr := p.colors[r][c]
			if ch == ' ' {
				clr = nil
			}
			k := keyOf(clr)
			if len(buf) > 0 && k != curKey {
				flush()
			}
			if len(buf) == 0 {
				curKey, curColor = k, clr
			}
			buf = append(buf, ch)
		}
		flush()
		sb.WriteString("\n")
	}
	return sb.String()
}

// colorKey identifies a color by its resolved RGBA components. Used
// instead of the color.Color interface itself because an arbitrary
// implementation need not be comparable, and comparing one would panic.
type colorKey struct{ r, g, b, a uint32 }

// keyOf resolves a color to its cache key. The zero key means "no
// color", which no real color can collide with: every color.Color
// reports a non-zero alpha.
func keyOf(c color.Color) colorKey {
	if c == nil {
		return colorKey{}
	}
	r, g, b, a := c.RGBA()
	return colorKey{r, g, b, a}
}

// panelTitle renders a sub-panel heading aligned with the plot area.
func panelTitle(s string) string {
	return repeat(" ", gridLabelWidth+1) + s + "\n"
}

// vscale maps values onto grid rows with the largest value at the top.
//
// Two constructors exist because the price chart and the bounded
// oscillator panels use different denominators: the price chart spreads
// its range over height rows, the panels over height-1 so that the top
// and bottom rows land exactly on max and min. Both behaviours predate
// this type and are preserved exactly.
type vscale struct {
	minV, maxV float64
	span       float64 // rows per unit of value
	height     int
}

// newPriceScale builds the scale used by the main price grid.
func newPriceScale(minV, maxV float64, height int) vscale {
	return vscale{minV: minV, maxV: maxV, span: scaleSpan(minV, maxV, height), height: height}
}

// newPanelScale builds the scale used by the sub-panels, where the
// bottom row is minV and the top row is maxV.
func newPanelScale(minV, maxV float64, height int) vscale {
	return vscale{minV: minV, maxV: maxV, span: scaleSpan(minV, maxV, height-1), height: height}
}

// scaleSpan is rows-per-unit for the given row count, with the
// degenerate and non-finite ranges folded onto a unit range the way the
// chart has always handled a flat series.
func scaleSpan(minV, maxV float64, rows int) float64 {
	if rows < 0 {
		rows = 0
	}
	rng := maxV - minV
	if !(rng > 0) || math.IsInf(rng, 0) || math.IsNaN(rng) {
		rng = 1
	}
	return float64(rows) / rng
}

// row returns the grid row for v, clamped into the panel. ok is false
// when the panel has no rows or v cannot be placed (non-finite input, or
// an offset that would overflow the conversion to int), in which case
// the caller must skip the sample rather than plot a garbage row.
func (s vscale) row(v float64) (int, bool) {
	if s.height < 1 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	off := (v - s.minV) * s.span
	if math.IsNaN(off) {
		return 0, false
	}
	if off >= float64(s.height) {
		return 0, true
	}
	if off <= -float64(s.height) {
		return s.height - 1, true
	}
	return clampRow(s.height-1-int(off), s.height), true
}

// value is the inverse of row: the value at the top edge of a row, used
// for axis labels.
func (s vscale) value(row int) float64 {
	if s.span == 0 {
		return s.maxV
	}
	return s.maxV - float64(row)/s.span
}

// plotSeries paints one value series across the panel, one sample every
// step columns. Samples the scale cannot place, and samples that fall
// past the right edge, are skipped.
func plotSeries(p *panel, values []float64, step int, s vscale, glyph rune, clr color.Color, mode paintMode) {
	if step < 1 {
		step = 1
	}
	for i, v := range values {
		col := i * step
		if col >= p.w {
			break
		}
		row, ok := s.row(v)
		if !ok {
			continue
		}
		p.paint(row, col, glyph, clr, mode)
	}
}

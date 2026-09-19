// Package options renders the Yahoo options chain (calls + puts) for a
// chosen symbol as a strike-aligned grid with a max-pain header.
package options

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// ChainSource fetches an options chain for a given symbol. The yahoo
// provider implements this; tests can stub it.
type ChainSource interface {
	FetchOptionsChain(ctx context.Context, symbol string) (yahoo.OptionsChain, error)
}

// Model is the options-tab Bubbletea model.
type Model struct {
	source ChainSource

	symbol  string
	chain   yahoo.OptionsChain
	cursor  int // selected strike row
	loading bool
	errMsg  string
	width   int
	height  int
}

// New constructs an empty Options model.
func New(source ChainSource) Model {
	return Model{source: source}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// LoadSymbol triggers an async fetch for the given symbol and returns
// the tea.Cmd to run. Callers typically pair this with switching to
// the Options tab.
func (m *Model) LoadSymbol(sym string) tea.Cmd {
	m.symbol = sym
	m.loading = true
	m.errMsg = ""
	m.cursor = 0
	src := m.source
	return func() tea.Msg {
		c, err := src.FetchOptionsChain(context.Background(), sym)
		if err != nil {
			return errorMsg{err: err}
		}
		return loadedMsg{chain: c}
	}
}

type loadedMsg struct{ chain yahoo.OptionsChain }
type errorMsg struct{ err error }

// Update handles messages. The chain is rendered from the theme's
// exported colors at render time, so a theme change needs no
// cached-style rebuild.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		return m, nil
	case loadedMsg:
		m.chain = msg.chain
		m.loading = false
		return m, nil
	case errorMsg:
		m.loading = false
		m.errMsg = msg.err.Error()
		return m, nil
	case tea.KeyPressMsg:
		rows := uniqueStrikes(m.chain)
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(rows)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		}
	case tea.MouseWheelMsg:
		rows := uniqueStrikes(m.chain)
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(rows)-1 {
				m.cursor++
			}
		}
	}
	return m, nil
}

const (
	// chainGutter is the cursor column, spent before the first cell.
	chainGutter = 2
	// chainSepExtra is what the two " | " frames around Strike cost over
	// and above the single cell Fit charges between adjacent columns:
	// 3 cells each, so 4 beyond what the fit accounts for. The tab
	// reserves them up front — Fit(cols, width-chainSepExtra, chainGutter)
	// — and composes the frames itself.
	chainSepExtra = 4
)

var (
	chainCallKeys = []string{"call_bid", "call_last", "call_iv"}
	chainPutKeys  = []string{"put_bid", "put_last", "put_iv"}

	// chainMate binds each column to the one it must shed with.
	chainMate = map[string]string{
		"call_bid":  "put_bid",
		"call_last": "put_last",
		"call_iv":   "put_iv",
		"put_bid":   "call_bid",
		"put_last":  "call_last",
		"put_iv":    "call_iv",
	}

	// chainLabels are the header cells, laid out through the same
	// geometry as a data row. A label is a value like any other: "PUT IV%"
	// is 7 cells against a floor of 6, and a label wider than its column
	// overflows the header row exactly as an over-long quote overflows a
	// data row.
	chainLabels = map[string]string{
		"call_bid":  "CALL Bid",
		"call_last": "Last",
		"call_iv":   "IV%",
		"strike":    "Strike",
		"put_bid":   "Bid",
		"put_last":  "Last",
		"put_iv":    "PUT IV%",
	}
)

// chainCols is the column plan in table order, and the only place the
// chain's geometry is written down. A call column and its put
// counterpart carry the same Prio because the chain is read across
// Strike: a call quote with no put beside it compares nothing.
func chainCols() []format.Col {
	return []format.Col{
		{Key: "call_bid", Width: 12, Min: 8, Prio: 6},
		{Key: "call_last", Width: 8, Min: 6, Prio: 4},
		{Key: "call_iv", Width: 8, Min: 6, Prio: 2},
		{Key: "strike", Width: 9, Min: 7, Prio: 7}, // the row identifier: highest Prio, so it sheds last
		{Key: "put_bid", Width: 8, Min: 6, Prio: 6},
		{Key: "put_last", Width: 8, Min: 6, Prio: 4},
		{Key: "put_iv", Width: 12, Min: 6, Prio: 2},
	}
}

// chainLayout fits the plan to a frame and then enforces the pairing
// that equal Prio only makes likely: Fit breaks a Prio tie on position,
// so it sheds one side of a pair a step before the other. Dropping a
// column only frees cells, so the refit sheds nothing further and the
// surviving set is the same either way.
//
// A nil layout means the frame holds no table at all.
func chainLayout(width int) format.Layout {
	cols := chainCols()
	fitted := format.Fit(cols, width-chainSepExtra, chainGutter)
	if fitted == nil {
		return nil
	}

	kept := make([]format.Col, 0, len(cols))
	for _, c := range cols {
		if !fitted.Has(c.Key) {
			continue
		}
		if mate, paired := chainMate[c.Key]; paired && !fitted.Has(mate) {
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == len(fitted) {
		return fitted
	}
	return format.Fit(kept, width-chainSepExtra, chainGutter)
}

// chainCell renders one value right-aligned in w display cells. A column
// width is a maximum as much as a minimum: fmt's %Ns pads a short value
// and writes a long one straight through the column, and nothing
// downstream clips it. The padding is measured in cells and applied to
// the raw value, before a style wraps it in escapes a rune count would
// then charge for.
func chainCell(s string, w int) string {
	s = format.Truncate(s, w)
	return format.Spaces(w-lipgloss.Width(s)) + s
}

// chainRow lays one row out around Strike, taking each surviving cell
// from value. lead is the cursor gutter, already styled; it escapes the
// cell arithmetic because it is never padded to a width.
func chainRow(l format.Layout, lead string, value func(key string) string) string {
	var b strings.Builder
	b.WriteString(lead)
	if calls := chainSide(l, chainCallKeys, value); calls != "" {
		b.WriteString(calls)
		b.WriteString(" | ")
	}
	b.WriteString(chainCell(value("strike"), l.Cells("strike")))
	if puts := chainSide(l, chainPutKeys, value); puts != "" {
		b.WriteString(" | ")
		b.WriteString(puts)
	}
	return b.String()
}

// chainSide joins the surviving columns of one block with the single
// cell Fit charges between adjacent columns.
func chainSide(l format.Layout, keys []string, value func(key string) string) string {
	cells := make([]string, 0, len(keys))
	for _, k := range keys {
		if !l.Has(k) {
			continue
		}
		cells = append(cells, chainCell(value(k), l.Cells(k)))
	}
	return strings.Join(cells, " ")
}

// View renders the options chain.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	var sb strings.Builder

	// Every chrome line is truncated to the frame for the same reason a
	// row is: the content panel word-wraps what it cannot fit, and a
	// wrapped line spends a second row of a height budget that assumes
	// one display line per row.
	dim := func(s string) string { return theme.StyleDim.Render(format.Truncate(s, m.width)) }

	if m.symbol == "" {
		sb.WriteString(theme.SectionHeader("Options", m.width))
		sb.WriteString("\n\n")
		sb.WriteString(dim("  Select a symbol on the Watchlist tab and press 'O' to load its options chain."))
		return sb.String()
	}

	header := fmt.Sprintf("Options: %s", m.symbol)
	if !m.chain.Expiration.IsZero() {
		header += "  " + m.chain.Expiration.Format("2006-01-02")
	}
	if mp := MaxPain(m.chain); !math.IsNaN(mp) {
		header += fmt.Sprintf("   Max Pain: $%.2f", mp)
	}
	sb.WriteString(theme.SectionHeader(header, m.width))
	sb.WriteString("\n\n")

	if m.loading {
		sb.WriteString(dim("  Loading…"))
		return sb.String()
	}
	if m.errMsg != "" {
		sb.WriteString(theme.StyleDown.Render(format.Truncate("  "+m.errMsg, m.width)))
		return sb.String()
	}
	if len(m.chain.Calls) == 0 && len(m.chain.Puts) == 0 {
		sb.WriteString(dim("  No options data available."))
		return sb.String()
	}

	lay := chainLayout(m.width)
	if lay == nil {
		sb.WriteString(dim("  Terminal too small for the options chain."))
		return sb.String()
	}

	strikes := uniqueStrikes(m.chain)
	sort.Float64s(strikes)
	callsByStrike := indexByStrike(m.chain.Calls)
	putsByStrike := indexByStrike(m.chain.Puts)

	// Column header
	colHdr := chainRow(lay, format.Spaces(chainGutter), func(k string) string { return chainLabels[k] })
	sb.WriteString(theme.StyleHeader.Render(colHdr))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(format.Repeat("─", m.width)))
	sb.WriteString("\n")

	// Section header, blank, column header, separator above; the tab's
	// own chrome below.
	maxRows := format.VisibleRows(m.height, 6, len(strikes))
	start := format.ViewportStart(m.cursor, len(strikes), maxRows)
	end := start + maxRows
	if end > len(strikes) {
		end = len(strikes)
	}
	for i := start; i < end; i++ {
		s := strikes[i]
		c := callsByStrike[s]
		p := putsByStrike[s]
		lead := format.Spaces(chainGutter)
		if i == m.cursor {
			lead = theme.StyleCursorGutter.Render("▎") + " "
		}
		row := chainRow(lay, lead, func(k string) string {
			switch k {
			case "call_bid":
				return fmtMoney(c.Bid)
			case "call_last":
				return fmtMoney(c.Last)
			case "call_iv":
				return fmtPct(c.IV)
			case "strike":
				return fmt.Sprintf("$%.2f", s)
			case "put_bid":
				return fmtMoney(p.Bid)
			case "put_last":
				return fmtMoney(p.Last)
			case "put_iv":
				return fmtPct(p.IV)
			}
			return ""
		})
		sb.WriteString(theme.StyleVal.Render(row))
		sb.WriteString("\n")
	}
	return sb.String()
}

func indexByStrike(opts []yahoo.Option) map[float64]yahoo.Option {
	out := make(map[float64]yahoo.Option, len(opts))
	for _, o := range opts {
		out[o.Strike] = o
	}
	return out
}

func fmtMoney(v float64) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", v)
}

func fmtPct(v float64) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v*100)
}

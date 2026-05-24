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
	"github.com/stxkxs/mkt/internal/provider/yahoo"
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

// RebuildStyles is a no-op placeholder for theme broadcast compatibility.
func RebuildStyles() {}

type loadedMsg struct{ chain yahoo.OptionsChain }
type errorMsg struct{ err error }

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
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

// View renders the options chain.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	var sb strings.Builder

	if m.symbol == "" {
		sb.WriteString(theme.SectionHeader("Options", m.width))
		sb.WriteString("\n\n")
		sb.WriteString(theme.StyleDim.Render("  Select a symbol on the Watchlist tab and press 'O' to load its options chain."))
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
		sb.WriteString(theme.StyleDim.Render("  Loading…"))
		return sb.String()
	}
	if m.errMsg != "" {
		sb.WriteString(theme.StyleDown.Render("  " + m.errMsg))
		return sb.String()
	}
	if len(m.chain.Calls) == 0 && len(m.chain.Puts) == 0 {
		sb.WriteString(theme.StyleDim.Render("  No options data available."))
		return sb.String()
	}

	strikes := uniqueStrikes(m.chain)
	sort.Float64s(strikes)
	callsByStrike := indexByStrike(m.chain.Calls)
	putsByStrike := indexByStrike(m.chain.Puts)

	// Column header
	colHdr := fmt.Sprintf("  %12s %8s %8s | %9s | %8s %8s %12s",
		"CALL Bid", "Last", "IV%", "Strike", "Bid", "Last", "PUT IV%")
	sb.WriteString(theme.StyleHeader.Render(colHdr))
	sb.WriteString("\n")
	sb.WriteString(theme.StyleBorderChar.Render(strings.Repeat("─", m.width)))
	sb.WriteString("\n")

	maxRows := m.height - 6
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows >= len(strikes) {
		maxRows = len(strikes)
	}
	start := m.cursor - maxRows + 1
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(strikes) {
		start = len(strikes) - maxRows
	}
	end := start + maxRows
	if end > len(strikes) {
		end = len(strikes)
	}
	for i := start; i < end; i++ {
		s := strikes[i]
		c := callsByStrike[s]
		p := putsByStrike[s]
		cursor := "  "
		if i == m.cursor {
			cursor = theme.StyleCursorGutter.Render("▎") + " "
		}
		row := fmt.Sprintf("%s%12s %8s %8s | %9s | %8s %8s %12s",
			cursor,
			fmtMoney(c.Bid), fmtMoney(c.Last), fmtPct(c.IV),
			fmt.Sprintf("$%.2f", s),
			fmtMoney(p.Bid), fmtMoney(p.Last), fmtPct(p.IV),
		)
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

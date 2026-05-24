// Package palette is a tiny command prompt for jump-to-tab, theme
// switching, and quit. Opened with `:` from any tab.
package palette

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// Action is what the palette wants the host to do.
type Action int

const (
	ActionNone Action = iota
	ActionJumpTab
	ActionSetTheme
	ActionQuit
	ActionCancel
)

// Result is the payload of an Action.
type Result struct {
	Action Action
	Arg    string // tab name or theme name; empty for Quit/Cancel
}

// Model is the palette state.
type Model struct {
	active   bool
	query    string
	tabNames []string
}

// New constructs the palette with the known tab names for tab-jump.
func New(tabNames []string) Model {
	return Model{tabNames: tabNames}
}

// Active reports whether the palette is open.
func (m Model) Active() bool { return m.active }

// Open activates the prompt and clears the query.
func (m *Model) Open() {
	m.active = true
	m.query = ""
}

// Update consumes a key while the palette is active. Returns the Model
// and a Result describing what the host should do; ActionNone means
// keep typing.
func (m Model) Update(msg tea.KeyPressMsg) (Model, Result) {
	if !m.active {
		return m, Result{Action: ActionNone}
	}
	key := msg.String()
	switch key {
	case "esc":
		m.active = false
		m.query = ""
		return m, Result{Action: ActionCancel}
	case "enter":
		res := m.parse()
		m.active = false
		m.query = ""
		return m, res
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
		}
		return m, Result{Action: ActionNone}
	}
	for _, r := range key {
		if unicode.IsPrint(r) && !unicode.IsControl(r) {
			m.query += string(r)
		}
	}
	return m, Result{Action: ActionNone}
}

// parse turns the typed query into an Action.
//
// Commands:
//
//	<tabname>           jump to tab whose name starts with <tabname> (case-insensitive)
//	theme <name>        switch theme
//	q | quit            quit
func (m Model) parse() Result {
	q := strings.TrimSpace(strings.ToLower(m.query))
	if q == "" {
		return Result{Action: ActionCancel}
	}
	if q == "q" || q == "quit" {
		return Result{Action: ActionQuit}
	}
	if strings.HasPrefix(q, "theme ") {
		return Result{Action: ActionSetTheme, Arg: strings.TrimSpace(q[len("theme "):])}
	}
	for _, n := range m.tabNames {
		if strings.HasPrefix(strings.ToLower(n), q) {
			return Result{Action: ActionJumpTab, Arg: n}
		}
	}
	// Unknown — treat as cancel; UX could surface an error later.
	return Result{Action: ActionCancel}
}

// View renders the prompt as a single bottom line. Returns empty when
// inactive.
func (m Model) View(width int) string {
	if !m.active {
		return ""
	}
	prompt := lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render(": ")
	q := lipgloss.NewStyle().Foreground(theme.ColorFg).Render(m.query)
	hint := theme.StyleDim.Render("  (tab name, 'theme <name>', or q)")
	line := prompt + q + "█" + hint
	return lipgloss.NewStyle().Width(width).Render(line)
}

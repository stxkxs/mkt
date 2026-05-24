package news

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	inews "github.com/stxkxs/mkt/internal/news"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleSource = lipgloss.NewStyle().Foreground(theme.ColorCyan)
	styleTime   = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleTitle  = lipgloss.NewStyle().Foreground(theme.ColorFg)
)

// Filter selects which subset of headlines is shown.
type Filter int

const (
	FilterAll     Filter = iota // every headline
	FilterNews                  // only items without a Category (RSS news)
	FilterFilings               // only items with a Category (SEC filings)
)

func (f Filter) String() string {
	switch f {
	case FilterNews:
		return "News"
	case FilterFilings:
		return "Filings"
	}
	return "All"
}

// Model is the news feed tab.
type Model struct {
	headlines []inews.Headline
	cursor    int
	filter    Filter
	width     int
	height    int
}

// New creates a news model.
func New() Model {
	return Model{}
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// UpdateHeadlines replaces the headline list.
func (m *Model) UpdateHeadlines(headlines []inews.Headline) {
	m.headlines = headlines
}

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleSource = lipgloss.NewStyle().Foreground(theme.ColorCyan)
	styleTime = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleTitle = lipgloss.NewStyle().Foreground(theme.ColorFg)
}

// visible returns the headline subset matching the current filter.
func (m Model) visible() []inews.Headline {
	if m.filter == FilterAll {
		return m.headlines
	}
	out := make([]inews.Headline, 0, len(m.headlines))
	for _, h := range m.headlines {
		switch m.filter {
		case FilterNews:
			if h.Category == "" {
				out = append(out, h)
			}
		case FilterFilings:
			if h.Category != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case tea.KeyPressMsg:
		vis := m.visible()
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(vis)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(vis) > 0 {
				m.cursor = len(vis) - 1
			}
		case "f":
			m.filter = (m.filter + 1) % 3
			m.cursor = 0
		case "enter":
			if m.cursor < len(vis) {
				h := vis[m.cursor]
				if h.Link != "" {
					_ = inews.OpenURL(h.Link)
				}
			}
		}
	case tea.MouseWheelMsg:
		vis := m.visible()
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(vis)-1 {
				m.cursor++
			}
		}
	case tea.MouseClickMsg:
		vis := m.visible()
		if len(vis) == 0 {
			return m, nil
		}
		// Local header: section header + hint + blank = 3 rows. Each
		// headline occupies 2 rows.
		row := msg.Y - 3
		if row < 0 {
			return m, nil
		}
		start := newsViewportStart(m.cursor, len(vis), m.height)
		idx := start + row/2
		if idx >= 0 && idx < len(vis) {
			m.cursor = idx
		}
	}
	return m, nil
}

// newsViewportStart mirrors the offset calculation in View so the click
// handler agrees with what's actually rendered.
func newsViewportStart(cursor, total, height int) int {
	maxItems := (height - 3) / 2
	if maxItems < 1 {
		maxItems = 1
	}
	if maxItems >= total {
		return 0
	}
	start := cursor - maxItems + 1
	if start < 0 {
		start = 0
	}
	if start+maxItems > total {
		start = total - maxItems
	}
	return start
}

// View renders the news feed.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	vis := m.visible()
	var sb strings.Builder
	sb.WriteString(theme.SectionHeader("News Feed ["+m.filter.String()+"]", m.width))
	sb.WriteString(theme.StyleDim.Render("  j/k:nav  enter:open  g/G:top/bottom  f:filter"))
	sb.WriteString("\n\n")

	if len(vis) == 0 {
		if len(m.headlines) == 0 {
			sb.WriteString(theme.StyleDim.Render("  Loading news..."))
		} else {
			sb.WriteString(theme.StyleDim.Render("  No items match the current filter."))
		}
		return sb.String()
	}

	// Each headline = 2 lines, so maxItems = (height - 3) / 2
	maxItems := (m.height - 3) / 2
	if maxItems < 1 {
		maxItems = 1
	}
	if maxItems > len(vis) {
		maxItems = len(vis)
	}

	startIdx := 0
	if len(vis) > maxItems {
		startIdx = m.cursor - maxItems + 1
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxItems > len(vis) {
			startIdx = len(vis) - maxItems
		}
	}
	endIdx := startIdx + maxItems

	for i := startIdx; i < endIdx; i++ {
		h := vis[i]

		cursor := "  "
		if i == m.cursor {
			cursor = theme.StyleCursorGutter.Render("▎") + " "
		}

		// Line 1: source + time
		meta := fmt.Sprintf("%s%s  %s",
			cursor,
			styleSource.Render(h.Source),
			styleTime.Render(inews.TimeAgo(h.PubTime)),
		)
		sb.WriteString(meta)
		sb.WriteString("\n")

		// Line 2: title (indented)
		title := h.Title
		maxTitleWidth := m.width - 6
		if maxTitleWidth > 0 && len(title) > maxTitleWidth {
			title = title[:maxTitleWidth-1] + "…"
		}
		titleStyle := styleTitle
		if i == m.cursor {
			titleStyle = titleStyle.Bold(true)
		}
		sb.WriteString("    " + titleStyle.Render(title))
		sb.WriteString("\n")
	}

	return sb.String()
}

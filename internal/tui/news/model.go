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

// Model is the news feed tab.
type Model struct {
	headlines []inews.Headline
	cursor    int
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

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.headlines)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.headlines) > 0 {
				m.cursor = len(m.headlines) - 1
			}
		case "enter":
			if m.cursor < len(m.headlines) {
				h := m.headlines[m.cursor]
				if h.Link != "" {
					_ = inews.OpenURL(h.Link)
				}
			}
		}
	}
	return m, nil
}

// View renders the news feed.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(theme.StyleHeader.Render("  NEWS FEED"))
	sb.WriteString(theme.StyleDim.Render("  j/k:nav  enter:open  g/G:top/bottom"))
	sb.WriteString("\n\n")

	if len(m.headlines) == 0 {
		sb.WriteString(theme.StyleDim.Render("  Loading news..."))
		return sb.String()
	}

	// Each headline = 2 lines, so maxItems = (height - 3) / 2
	maxItems := (m.height - 3) / 2
	if maxItems < 1 {
		maxItems = 1
	}
	if maxItems > len(m.headlines) {
		maxItems = len(m.headlines)
	}

	startIdx := 0
	if len(m.headlines) > maxItems {
		startIdx = m.cursor - maxItems + 1
		if startIdx < 0 {
			startIdx = 0
		}
		if startIdx+maxItems > len(m.headlines) {
			startIdx = len(m.headlines) - maxItems
		}
	}
	endIdx := startIdx + maxItems

	for i := startIdx; i < endIdx; i++ {
		h := m.headlines[i]

		cursor := "  "
		if i == m.cursor {
			cursor = theme.StyleCursor.Render("> ")
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

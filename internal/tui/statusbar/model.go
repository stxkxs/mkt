package statusbar

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleBar = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorDim)

	styleConnected = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorGreen)

	styleDisconnected = lipgloss.NewStyle().
				Background(theme.ColorTabBg).
				Foreground(theme.ColorRed)

	styleAlertCount = lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorYellow).
			Bold(true)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleBar = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorDim)
	styleConnected = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorGreen)
	styleDisconnected = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorRed)
	styleAlertCount = lipgloss.NewStyle().
		Background(theme.ColorTabBg).
		Foreground(theme.ColorYellow).
		Bold(true)
}

type providerEntry struct {
	Name      string
	Connected bool
}

// Model is the status bar component.
type Model struct {
	providers   []providerEntry
	lastUpdate  time.Time
	alertCount  int
	themeName   string
	searchQuery string
	width       int
}

// New creates a new status bar.
func New() Model {
	return Model{}
}

// SetWidth updates the status bar width.
func (m *Model) SetWidth(w int) {
	m.width = w
}

// SetProviderStatus updates the connection status of a provider.
func (m *Model) SetProviderStatus(name string, connected bool) {
	for i, p := range m.providers {
		if p.Name == name {
			m.providers[i].Connected = connected
			return
		}
	}
	m.providers = append(m.providers, providerEntry{Name: name, Connected: connected})
}

// SetLastUpdate records the last quote update time.
func (m *Model) SetLastUpdate(t time.Time) {
	m.lastUpdate = t
}

// SetAlertCount updates the active alert count.
func (m *Model) SetAlertCount(n int) {
	m.alertCount = n
}

// SetThemeName updates the displayed theme name.
func (m *Model) SetThemeName(name string) {
	m.themeName = name
}

// SetSearchQuery updates the search query displayed in the status bar.
func (m *Model) SetSearchQuery(q string) {
	m.searchQuery = q
}

// View renders the status bar.
func (m Model) View() string {
	var parts []string

	// Provider status
	for _, p := range m.providers {
		if p.Connected {
			parts = append(parts, styleConnected.Render("● "+p.Name))
		} else {
			parts = append(parts, styleDisconnected.Render("○ "+p.Name))
		}
	}

	// Last update
	if !m.lastUpdate.IsZero() {
		elapsed := time.Since(m.lastUpdate).Truncate(time.Second)
		parts = append(parts, styleBar.Render(fmt.Sprintf("updated %s ago", elapsed)))
	}

	// Search query
	if m.searchQuery != "" {
		parts = append(parts, styleAlertCount.Render(fmt.Sprintf("/ %s", m.searchQuery)))
	}

	left := strings.Join(parts, styleBar.Render("  "))

	// Right side: alerts + help
	var right string
	if m.alertCount > 0 {
		right = styleAlertCount.Render(fmt.Sprintf("🔔 %d alerts", m.alertCount))
	}
	if m.themeName != "" {
		right += styleBar.Render("  T:" + m.themeName)
	}
	right += styleBar.Render("  q:quit  tab:switch  j/k:nav  enter:detail")

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}

	return styleBar.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
}

package statusbar

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var (
	colorDim    = lipgloss.Color("#565f89")
	colorGreen  = lipgloss.Color("#9ece6a")
	colorRed    = lipgloss.Color("#f7768e")
	colorTabBg  = lipgloss.Color("#24283b")
	colorYellow = lipgloss.Color("#e0af68")

	styleBar = lipgloss.NewStyle().
			Background(colorTabBg).
			Foreground(colorDim)

	styleConnected = lipgloss.NewStyle().
			Background(colorTabBg).
			Foreground(colorGreen)

	styleDisconnected = lipgloss.NewStyle().
				Background(colorTabBg).
				Foreground(colorRed)

	styleAlertCount = lipgloss.NewStyle().
			Background(colorTabBg).
			Foreground(colorYellow).
			Bold(true)
)

// Model is the status bar component.
type Model struct {
	providers  map[string]bool
	lastUpdate time.Time
	alertCount int
	width      int
}

// New creates a new status bar.
func New() Model {
	return Model{
		providers: make(map[string]bool),
	}
}

// SetWidth updates the status bar width.
func (m *Model) SetWidth(w int) {
	m.width = w
}

// SetProviderStatus updates the connection status of a provider.
func (m *Model) SetProviderStatus(name string, connected bool) {
	m.providers[name] = connected
}

// SetLastUpdate records the last quote update time.
func (m *Model) SetLastUpdate(t time.Time) {
	m.lastUpdate = t
}

// SetAlertCount updates the active alert count.
func (m *Model) SetAlertCount(n int) {
	m.alertCount = n
}

// View renders the status bar.
func (m Model) View() string {
	var parts []string

	// Provider status
	for name, connected := range m.providers {
		if connected {
			parts = append(parts, styleConnected.Render("● "+name))
		} else {
			parts = append(parts, styleDisconnected.Render("○ "+name))
		}
	}

	// Last update
	if !m.lastUpdate.IsZero() {
		elapsed := time.Since(m.lastUpdate).Truncate(time.Second)
		parts = append(parts, styleBar.Render(fmt.Sprintf("updated %s ago", elapsed)))
	}

	left := strings.Join(parts, styleBar.Render("  "))

	// Right side: alerts + help
	var right string
	if m.alertCount > 0 {
		right = styleAlertCount.Render(fmt.Sprintf("🔔 %d alerts", m.alertCount))
	}
	right += styleBar.Render("  q:quit  tab:switch  j/k:nav  enter:detail")

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}

	return styleBar.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
}

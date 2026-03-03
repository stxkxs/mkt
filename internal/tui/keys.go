package tui

import tea "charm.land/bubbletea/v2"

// Tab represents a TUI tab.
type Tab int

const (
	TabWatchlist Tab = iota
	TabPortfolio
	TabAlerts
	TabChart
)

var tabNames = []string{"Watchlist", "Portfolio", "Alerts", "Chart"}

func (t Tab) String() string {
	if int(t) < len(tabNames) {
		return tabNames[t]
	}
	return "Unknown"
}

// isQuit returns true for quit key combos.
func isQuit(msg tea.KeyPressMsg) bool {
	return msg.String() == "q" || msg.String() == "ctrl+c"
}

// isTabSwitch returns the target tab if the key is a tab switch, or -1.
func isTabSwitch(msg tea.KeyPressMsg) Tab {
	switch msg.String() {
	case "1":
		return TabWatchlist
	case "2":
		return TabPortfolio
	case "3":
		return TabAlerts
	case "4":
		return TabChart
	default:
		return -1
	}
}

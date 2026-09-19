package tui

import tea "charm.land/bubbletea/v2"

// Tab represents a TUI tab.
type Tab int

const (
	TabWatchlist Tab = iota
	TabPortfolio
	TabAlerts
	TabChart
	TabMacro
	TabNews
	TabHeatmap
	TabOptions
	TabCorrel
)

var tabNames = []string{"Watch", "Portfolio", "Alerts", "Chart", "Macro", "News", "Heatmap", "Options", "Correl"}

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
// The digits select by position — "1" is the leftmost tab — so the
// binding is derived from the tab set rather than being a second list of
// it that has to be kept in step. Tabs past the ninth have no digit.
func isTabSwitch(msg tea.KeyPressMsg) Tab {
	s := msg.String()
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return -1
	}
	if i := int(s[0] - '1'); i < len(tabNames) {
		return Tab(i)
	}
	return -1
}

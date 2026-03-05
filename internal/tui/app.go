package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	alertsview "github.com/stxkxs/mkt/internal/tui/alerts"
	"github.com/stxkxs/mkt/internal/tui/chart"
	"github.com/stxkxs/mkt/internal/tui/detail"
	portfolioview "github.com/stxkxs/mkt/internal/tui/portfolio"
	"github.com/stxkxs/mkt/internal/tui/statusbar"
	"github.com/stxkxs/mkt/internal/tui/theme"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

// App is the root TUI model.
type App struct {
	activeTab Tab
	width     int
	height    int
	ready     bool

	watchlist watchlist.Model
	detail    detail.Model
	chart     chart.Model
	portfolio portfolioview.Model
	alerts    alertsview.Model
	statusbar statusbar.Model
	cache     *market.Cache
}

// NewApp creates the root TUI model.
func NewApp(symbols []string, cache *market.Cache, histProvider chart.HistoryProvider, portfolios []portfolio.Portfolio, alertEngine *alert.Engine) *App {
	return &App{
		activeTab: TabWatchlist,
		watchlist: watchlist.New(symbols, cache),
		detail:    detail.New(cache),
		chart:     chart.New(histProvider),
		portfolio: portfolioview.New(portfolios),
		alerts:    alertsview.New(alertEngine),
		statusbar: statusbar.New(),
		cache:     cache,
	}
}

func (a *App) Init() tea.Cmd {
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.watchlist.SetSize(msg.Width, msg.Height-2)
		a.statusbar.SetWidth(msg.Width)
		a.detail.SetSize(msg.Width, msg.Height-2)
		a.chart.SetSize(msg.Width, msg.Height-2)
		return a, nil

	case tea.KeyPressMsg:
		if isQuit(msg) {
			return a, tea.Quit
		}

		// If detail panel is active, route to it
		if a.detail.Active() {
			var cmd tea.Cmd
			a.detail, cmd = a.detail.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

		// If chart is active, route to it
		if a.chart.Active() {
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

		// Tab switching
		if tab := isTabSwitch(msg); tab >= 0 {
			a.activeTab = tab
			return a, nil
		}
		switch msg.String() {
		case "tab", "right":
			a.activeTab = (a.activeTab + 1) % Tab(len(tabNames))
			return a, nil
		case "shift+tab", "left":
			a.activeTab = (a.activeTab - 1 + Tab(len(tabNames))) % Tab(len(tabNames))
			return a, nil
		}

		// Forward to active tab
		switch a.activeTab {
		case TabWatchlist:
			switch msg.String() {
			case "enter":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					a.detail.SetSymbol(sym)
					a.detail.SetActive(true)
				}
				return a, nil
			case "c":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					cmd := a.chart.SetSymbol(sym)
					return a, cmd
				}
				return a, nil
			}
			var cmd tea.Cmd
			a.watchlist, cmd = a.watchlist.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

		case TabPortfolio:
			var cmd tea.Cmd
			a.portfolio, cmd = a.portfolio.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

		case TabAlerts:
			var cmd tea.Cmd
			a.alerts, cmd = a.alerts.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

		case TabChart:
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case QuoteUpdateMsg:
		a.watchlist.UpdateQuote(msg.Quote)
		a.detail.UpdateQuote(msg.Quote)
		a.portfolio.UpdateQuote(msg.Quote)
		a.statusbar.SetLastUpdate(msg.Quote.Timestamp)
		return a, nil

	case AlertTriggeredMsg:
		a.alerts.AddTriggered(msg.Alert)
		a.statusbar.SetAlertCount(a.alerts.TriggeredCount())
		return a, nil

	case ConnectionStatusMsg:
		a.statusbar.SetProviderStatus(msg.Provider, msg.Connected)
		return a, nil

	default:
		// Forward unknown messages to chart (for history loaded)
		if a.chart.Active() || a.activeTab == TabChart {
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return a, tea.Batch(cmds...)
}

func (a *App) View() tea.View {
	if !a.ready {
		return tea.NewView("Loading...")
	}

	// Full-screen chart mode
	if a.chart.Active() {
		s := a.chart.View()
		v := tea.NewView(s)
		v.AltScreen = true
		return v
	}

	// Detail panel overlay
	if a.detail.Active() {
		tabBar := a.renderTabBar()
		statusBar := a.statusbar.View()
		contentHeight := a.height - lipgloss.Height(tabBar) - lipgloss.Height(statusBar)
		a.detail.SetSize(a.width, contentHeight)
		content := lipgloss.NewStyle().
			Width(a.width).
			Height(contentHeight).
			Render(a.detail.View())
		s := lipgloss.JoinVertical(lipgloss.Left, tabBar, content, statusBar)
		v := tea.NewView(s)
		v.AltScreen = true
		return v
	}

	tabBar := a.renderTabBar()
	statusBar := a.statusbar.View()

	contentHeight := a.height - lipgloss.Height(tabBar) - lipgloss.Height(statusBar)
	var content string
	switch a.activeTab {
	case TabWatchlist:
		a.watchlist.SetSize(a.width, contentHeight)
		content = a.watchlist.View()
	case TabPortfolio:
		a.portfolio.SetSize(a.width, contentHeight)
		content = a.portfolio.View()
	case TabAlerts:
		a.alerts.SetSize(a.width, contentHeight)
		content = a.alerts.View()
	case TabChart:
		content = theme.StyleDim.Render("  Select a symbol from Watchlist and press 'c' for chart")
	}

	contentRendered := lipgloss.NewStyle().
		Width(a.width).
		Height(contentHeight).
		Render(content)

	s := lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		contentRendered,
		statusBar,
	)
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (a *App) renderTabBar() string {
	var tabs []string
	for i, name := range tabNames {
		num := string(rune('1' + i))
		text := num + " " + name
		if Tab(i) == a.activeTab {
			tabs = append(tabs, theme.StyleTabActive.Render(text))
		} else {
			tabs = append(tabs, theme.StyleTabInactive.Render(text))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	right := theme.StyleDim.Background(theme.ColorTabBg).Render(" mkt ")
	pad := a.width - lipgloss.Width(bar) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	filler := theme.StyleTabBar.Render(strings.Repeat(" ", pad))
	return lipgloss.JoinHorizontal(lipgloss.Top, bar, filler, right)
}

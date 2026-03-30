package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	alertsview "github.com/stxkxs/mkt/internal/tui/alerts"
	"github.com/stxkxs/mkt/internal/tui/alertdialog"
	"github.com/stxkxs/mkt/internal/tui/chart"
	"github.com/stxkxs/mkt/internal/tui/detail"
	"github.com/stxkxs/mkt/internal/tui/format"
	heatmapview "github.com/stxkxs/mkt/internal/tui/heatmap"
	macroview "github.com/stxkxs/mkt/internal/tui/macro"
	newsview "github.com/stxkxs/mkt/internal/tui/news"
	portfolioview "github.com/stxkxs/mkt/internal/tui/portfolio"
	"github.com/stxkxs/mkt/internal/tui/statusbar"
	"github.com/stxkxs/mkt/internal/tui/symbolinfo"
	"github.com/stxkxs/mkt/internal/tui/theme"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

// App is the root TUI model.
type App struct {
	activeTab   Tab
	width       int
	height      int
	ready       bool
	spinnerTick int

	watchlist   watchlist.Model
	detail      detail.Model
	chart       chart.Model
	compare     chart.CompareModel
	portfolio   portfolioview.Model
	alerts      alertsview.Model
	macro       macroview.Model
	news        newsview.Model
	heatmap     heatmapview.Model
	statusbar   statusbar.Model
	alertDialog alertdialog.Model
	symbolInfo  symbolinfo.Model
	cache       *market.Cache
}

// NewApp creates the root TUI model.
func NewApp(symbols []string, cache *market.Cache, histProvider chart.HistoryProvider, portfolios []portfolio.Portfolio, alertEngine *alert.Engine, yahooProv *yahoo.Provider) *App {
	a := &App{
		activeTab:   TabWatchlist,
		watchlist:   watchlist.New(symbols, cache),
		detail:      detail.New(cache),
		chart:       chart.New(histProvider),
		compare:     chart.NewCompare(histProvider),
		portfolio:   portfolioview.New(portfolios),
		alerts:      alertsview.New(alertEngine),
		macro:       macroview.New(),
		news:        newsview.New(),
		heatmap:     heatmapview.New(),
		statusbar:   statusbar.New(),
		alertDialog: alertdialog.New(alertEngine),
		symbolInfo:  symbolinfo.New(yahooProv),
		cache:       cache,
	}
	a.statusbar.SetThemeName(theme.CurrentName)
	return a
}

func (a *App) Init() tea.Cmd {
	return tea.Every(100*time.Millisecond, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		contentW, contentH := a.contentSize(msg.Width, msg.Height)
		a.watchlist.SetSize(contentW, contentH)
		a.statusbar.SetWidth(msg.Width)
		a.detail.SetSize(contentW, contentH)
		a.chart.SetSize(msg.Width, msg.Height-2)
		a.compare.SetSize(msg.Width, msg.Height-2)
		a.alertDialog.SetSize(msg.Width, msg.Height)
		a.symbolInfo.SetSize(msg.Width, msg.Height)
		return a, nil

	case SpinnerTickMsg:
		a.spinnerTick++
		return a, tea.Every(100*time.Millisecond, func(t time.Time) tea.Msg {
			return SpinnerTickMsg{}
		})

	case tea.KeyPressMsg:
		// Search mode guard: route all keys to watchlist while searching
		if a.activeTab == TabWatchlist && a.watchlist.Searching() {
			var cmd tea.Cmd
			a.watchlist, cmd = a.watchlist.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			a.statusbar.SetSearchQuery(a.watchlist.SearchQuery())
			return a, tea.Batch(cmds...)
		}

		// Alert dialog guard
		if a.alertDialog.Active() {
			var cmd tea.Cmd
			a.alertDialog, cmd = a.alertDialog.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

		// Symbol info overlay guard
		if a.symbolInfo.Active() {
			var cmd tea.Cmd
			a.symbolInfo, cmd = a.symbolInfo.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

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

		// If compare chart is active, route to it
		if a.compare.Active() {
			var cmd tea.Cmd
			a.compare, cmd = a.compare.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}

		// Theme switching
		if msg.String() == "T" {
			name := theme.NextTheme()
			theme.Apply(name)
			a.rebuildAllStyles()
			a.statusbar.SetThemeName(name)
			return a, nil
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
			case "a":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					a.compare.AddSymbol(sym)
				}
				return a, nil
			case "C":
				if len(a.compare.Symbols()) > 0 {
					cmd := a.compare.Open()
					return a, cmd
				}
				return a, nil
			case "A":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					price := a.watchlist.CurrentPrice(sym)
					a.alertDialog.Open(sym, price)
				}
				return a, nil
			case "?":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					cmd := a.symbolInfo.Open(sym)
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

		case TabMacro:
			// Macro has no Update (no interactive keys beyond tab switching)

		case TabNews:
			var cmd tea.Cmd
			a.news, cmd = a.news.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

		case TabHeatmap:
			var cmd tea.Cmd
			a.heatmap, cmd = a.heatmap.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseClickMsg:
		if msg.Y == 0 {
			// Tab bar click
			tab := a.tabAtX(msg.X)
			if tab >= 0 {
				a.activeTab = tab
			}
			return a, nil
		}
		// Forward to active content with adjusted Y
		if a.activeTab == TabWatchlist {
			adjusted := tea.MouseClickMsg{
				X:      msg.X,
				Y:      msg.Y - 1, // subtract tab bar height
				Button: msg.Button,
			}
			var cmd tea.Cmd
			a.watchlist, cmd = a.watchlist.Update(adjusted)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseWheelMsg:
		if a.activeTab == TabWatchlist {
			var cmd tea.Cmd
			a.watchlist, cmd = a.watchlist.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case QuoteUpdateMsg:
		a.watchlist.UpdateQuote(msg.Quote)
		a.detail.UpdateQuote(msg.Quote)
		a.portfolio.UpdateQuote(msg.Quote)
		a.heatmap.UpdateQuote(msg.Quote)
		a.statusbar.SetLastUpdate(msg.Quote.Timestamp)
		return a, nil

	case MacroUpdateMsg:
		a.macro.UpdateQuotes(msg.Quotes)
		return a, nil

	case NewsUpdateMsg:
		a.news.UpdateHeadlines(msg.Headlines)
		return a, nil

	case AlertTriggeredMsg:
		a.alerts.AddTriggered(msg.Alert)
		a.statusbar.SetAlertCount(a.alerts.TriggeredCount())
		return a, nil

	case ConnectionStatusMsg:
		a.statusbar.SetProviderStatus(msg.Provider, msg.Connected)
		return a, nil

	default:
		// Forward unknown messages to chart (for history loaded) and compare
		if a.chart.Active() || a.activeTab == TabChart {
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if a.compare.Active() {
			var cmd tea.Cmd
			a.compare, cmd = a.compare.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		// Forward to symbol info for async load messages
		if a.symbolInfo.Active() {
			var cmd tea.Cmd
			a.symbolInfo, cmd = a.symbolInfo.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return a, tea.Batch(cmds...)
}

func (a *App) View() tea.View {
	if !a.ready {
		spinner := format.SpinnerFrame(a.spinnerTick)
		return tea.NewView(theme.StyleAccentText(spinner + " Loading..."))
	}

	// Full-screen chart mode
	if a.chart.Active() {
		s := a.chart.View()
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouse(v)
	}

	// Comparison chart mode
	if a.compare.Active() {
		s := a.compare.View()
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouse(v)
	}

	// Detail panel overlay
	if a.detail.Active() {
		tabBar := a.renderTabBar()
		statusBar := a.statusbar.View()
		contentW, contentH := a.contentSize(a.width, a.height)
		a.detail.SetSize(contentW, contentH)
		content := a.detail.View()
		panel := a.renderContentPanel("Detail", content, contentH)
		s := lipgloss.JoinVertical(lipgloss.Left, tabBar, panel, statusBar)
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouse(v)
	}

	tabBar := a.renderTabBar()
	statusBar := a.statusbar.View()

	contentW, contentH := a.contentSize(a.width, a.height)
	var content string
	switch a.activeTab {
	case TabWatchlist:
		a.watchlist.SetSize(contentW, contentH)
		content = a.watchlist.View()
	case TabPortfolio:
		a.portfolio.SetSize(contentW, contentH)
		content = a.portfolio.View()
	case TabAlerts:
		a.alerts.SetSize(contentW, contentH)
		content = a.alerts.View()
	case TabChart:
		content = theme.StyleDim.Render("  Select a symbol from Watchlist and press 'c' for chart")
	case TabMacro:
		a.macro.SetSize(contentW, contentH)
		content = a.macro.View()
	case TabNews:
		a.news.SetSize(contentW, contentH)
		content = a.news.View()
	case TabHeatmap:
		a.heatmap.SetSize(contentW, contentH)
		content = a.heatmap.View()
	}

	panel := a.renderContentPanel(tabNames[a.activeTab], content, contentH)

	s := lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		panel,
		statusBar,
	)

	// Overlay: alert dialog
	if a.alertDialog.Active() {
		s = a.overlayCenter(s, a.alertDialog.View())
	}

	// Overlay: symbol info
	if a.symbolInfo.Active() {
		s = a.overlayCenter(s, a.symbolInfo.View())
	}

	v := tea.NewView(s)
	v.AltScreen = true
	return withMouse(v)
}

func (a *App) overlayCenter(_ string, overlay string) string {
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay)
}

func withMouse(v tea.View) tea.View {
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// usePanelBorders returns true if the terminal is large enough for panel borders.
func (a *App) usePanelBorders() bool {
	return a.width >= 30 && a.height >= 15
}

// contentSize returns the width and height available for content inside the panel.
func (a *App) contentSize(totalW, totalH int) (int, int) {
	// tab bar (1) + status bar (1) = 2
	h := totalH - 2
	w := totalW
	if a.usePanelBorders() {
		h -= 2 // top + bottom border
		w -= 2 // left + right border
	}
	if h < 1 {
		h = 1
	}
	if w < 1 {
		w = 1
	}
	return w, h
}

// renderContentPanel wraps content in a bordered panel with an embedded title.
func (a *App) renderContentPanel(title, content string, contentH int) string {
	if !a.usePanelBorders() {
		return lipgloss.NewStyle().
			Width(a.width).
			Height(contentH).
			Render(content)
	}

	innerWidth := a.width - 2

	// Top border: ╭─── Title ──────────────────────╮
	titleRendered := theme.StylePanelTitle.Render(" " + title + " ")
	titleVisualWidth := lipgloss.Width(titleRendered)
	topFillLen := innerWidth - 1 - titleVisualWidth // "─" + title + fill
	if topFillLen < 0 {
		topFillLen = 0
	}
	top := theme.StyleBorderChar.Render("╭─") + titleRendered + theme.StyleBorderChar.Render(strings.Repeat("─", topFillLen)+"╮")

	// Bottom border: ╰────────────────────────────────╯
	bottom := theme.StyleBorderChar.Render("╰" + strings.Repeat("─", innerWidth) + "╯")

	// Content lines with side borders
	contentRendered := lipgloss.NewStyle().
		Width(innerWidth).
		Height(contentH).
		Render(content)
	lines := strings.Split(contentRendered, "\n")

	var sb strings.Builder
	sb.WriteString(top)
	sb.WriteString("\n")
	border := theme.StyleBorderChar.Render("│")
	for _, line := range lines {
		lineW := lipgloss.Width(line)
		pad := innerWidth - lineW
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(border)
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(border)
		sb.WriteString("\n")
	}
	sb.WriteString(bottom)

	return sb.String()
}

func (a *App) renderTabBar() string {
	sep := theme.StyleTabSeparator.Render(" │ ")

	var parts []string
	for i, name := range tabNames {
		indicator := "◇"
		style := theme.StyleTabInactive
		if Tab(i) == a.activeTab {
			indicator = "◆"
			style = theme.StyleTabActive
		}
		parts = append(parts, style.Render(indicator+" "+name))
	}

	bar := theme.StyleTabBar.Render(" ") + strings.Join(parts, sep)
	right := theme.StyleBranding.Render("▸ mkt ")
	barW := lipgloss.Width(bar)
	rightW := lipgloss.Width(right)
	pad := a.width - barW - rightW
	if pad < 0 {
		pad = 0
	}
	filler := theme.StyleTabBar.Render(strings.Repeat(" ", pad))
	return lipgloss.JoinHorizontal(lipgloss.Top, bar, filler, right)
}

// tabAtX returns which tab index was clicked at the given X coordinate, or -1.
func (a *App) tabAtX(x int) Tab {
	sep := theme.StyleTabSeparator.Render(" │ ")
	sepW := lipgloss.Width(sep)
	cumX := 1 // leading space from StyleTabBar " "
	for i, name := range tabNames {
		indicator := "◇"
		style := theme.StyleTabInactive
		if Tab(i) == a.activeTab {
			indicator = "◆"
			style = theme.StyleTabActive
		}
		text := style.Render(indicator + " " + name)
		w := lipgloss.Width(text)
		if x >= cumX && x < cumX+w {
			return Tab(i)
		}
		cumX += w + sepW
	}
	return -1
}

func (a *App) rebuildAllStyles() {
	watchlist.RebuildStyles()
	chart.RebuildStyles()
	alertsview.RebuildStyles()
	portfolioview.RebuildStyles()
	detail.RebuildStyles()
	statusbar.RebuildStyles()
	macroview.RebuildStyles()
	newsview.RebuildStyles()
	heatmapview.RebuildStyles()
	alertdialog.RebuildStyles()
	symbolinfo.RebuildStyles()
}

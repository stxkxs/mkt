package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/alertdialog"
	alertsview "github.com/stxkxs/mkt/internal/tui/alerts"
	"github.com/stxkxs/mkt/internal/tui/chart"
	correlview "github.com/stxkxs/mkt/internal/tui/correlation"
	"github.com/stxkxs/mkt/internal/tui/detail"
	"github.com/stxkxs/mkt/internal/tui/format"
	heatmapview "github.com/stxkxs/mkt/internal/tui/heatmap"
	macroview "github.com/stxkxs/mkt/internal/tui/macro"
	newsview "github.com/stxkxs/mkt/internal/tui/news"
	optionsview "github.com/stxkxs/mkt/internal/tui/options"
	paletteview "github.com/stxkxs/mkt/internal/tui/palette"
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
	options     optionsview.Model
	correl      correlview.Model
	palette     paletteview.Model
	statusbar   statusbar.Model
	alertDialog alertdialog.Model
	symbolInfo  symbolinfo.Model
	cache       *market.Cache
}

// NewApp creates the root TUI model.
func NewApp(groups []watchlist.Group, cache *market.Cache, histProvider chart.HistoryProvider, portfolios []portfolio.Portfolio, alertEngine *alert.Engine, yahooProv *yahoo.Provider, coinbaseProv *coinbase.Provider) *App {
	union := unionSymbols(groups)
	a := &App{
		activeTab:   TabWatchlist,
		watchlist:   watchlist.New(groups, cache),
		detail:      detail.New(cache, coinbaseProv),
		chart:       chart.New(histProvider),
		compare:     chart.NewCompare(histProvider),
		portfolio:   portfolioview.New(portfolios),
		alerts:      alertsview.New(alertEngine),
		macro:       macroview.New(),
		news:        newsview.New(),
		heatmap:     heatmapview.New(),
		options:     optionsview.New(yahooProv),
		correl:      correlview.New(union, cache),
		palette:     paletteview.New(tabNames),
		statusbar:   statusbar.New(),
		alertDialog: alertdialog.New(alertEngine),
		symbolInfo:  symbolinfo.New(yahooProv),
		cache:       cache,
	}
	a.statusbar.SetThemeName(theme.CurrentName)
	return a
}

// LoadPastAlerts populates the alerts tab with previously persisted
// triggers. Should be called before Run so the tab shows the history
// from first paint. Does not fire any desktop notifications.
func (a *App) LoadPastAlerts(past []alert.TriggeredAlert) {
	for _, t := range past {
		a.alerts.AddTriggered(t)
	}
	a.statusbar.SetAlertCount(a.alerts.TriggeredCount())
}

// LoadEquityHistory seeds the portfolio model with previously persisted
// equity marks. Should be called before Run.
func (a *App) LoadEquityHistory(byName map[string][]portfolio.EquityMark) {
	a.portfolio.LoadEquityHistory(byName)
}

// LoadCalendarEvents seeds the macro tab with upcoming economic events.
func (a *App) LoadCalendarEvents(events []calendar.Event) {
	a.macro.UpdateEvents(events)
}

// LoadNotes seeds the detail panel with per-symbol freeform notes.
func (a *App) LoadNotes(notes map[string]string) {
	a.detail.SetNotes(notes)
}

// unionSymbols returns the deduplicated union of every group's symbols.
func unionSymbols(groups []watchlist.Group) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, g := range groups {
		for _, s := range g.Symbols {
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
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
		// Palette guard: consume all keys while open.
		if a.palette.Active() {
			var res paletteview.Result
			a.palette, res = a.palette.Update(msg)
			switch res.Action {
			case paletteview.ActionJumpTab:
				for i, n := range tabNames {
					if n == res.Arg {
						a.activeTab = Tab(i)
						break
					}
				}
			case paletteview.ActionSetTheme:
				theme.Apply(res.Arg)
				a.statusbar.SetThemeName(theme.CurrentName)
				return a, func() tea.Msg { return theme.ChangedMsg{Name: theme.CurrentName} }
			case paletteview.ActionQuit:
				return a, tea.Quit
			}
			return a, nil
		}

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

		// Open palette
		if msg.String() == ":" {
			a.palette.Open()
			return a, nil
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

		// Theme switching: Apply updates global colors; broadcast ChangedMsg
		// so each sub-model can rebuild its cached styles in its own Update.
		if msg.String() == "T" {
			name := theme.NextTheme()
			theme.Apply(name)
			a.statusbar.SetThemeName(name)
			return a, func() tea.Msg { return theme.ChangedMsg{Name: name} }
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
					cmd := a.detail.SetSymbol(sym)
					a.detail.SetActive(true)
					return a, cmd
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
			case "O":
				sym := a.watchlist.SelectedSymbol()
				if sym != "" {
					a.activeTab = TabOptions
					cmd := a.options.LoadSymbol(sym)
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

		case TabOptions:
			var cmd tea.Cmd
			a.options, cmd = a.options.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}

		case TabCorrel:
			var cmd tea.Cmd
			a.correl, cmd = a.correl.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}

	case tea.MouseClickMsg:
		if msg.Y == 0 {
			tab := a.tabAtX(msg.X)
			if tab >= 0 {
				a.activeTab = tab
			}
			return a, nil
		}
		// Full-screen overlays take mouse focus before tab routing.
		if a.chart.Active() {
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		if a.compare.Active() {
			var cmd tea.Cmd
			a.compare, cmd = a.compare.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		adjusted := tea.MouseClickMsg{
			X:      msg.X,
			Y:      msg.Y - 1, // subtract tab bar height
			Button: msg.Button,
		}
		var cmd tea.Cmd
		switch a.activeTab {
		case TabWatchlist:
			a.watchlist, cmd = a.watchlist.Update(adjusted)
		case TabPortfolio:
			a.portfolio, cmd = a.portfolio.Update(adjusted)
		case TabAlerts:
			a.alerts, cmd = a.alerts.Update(adjusted)
		case TabNews:
			a.news, cmd = a.news.Update(adjusted)
		case TabHeatmap:
			a.heatmap, cmd = a.heatmap.Update(adjusted)
		case TabOptions:
			a.options, cmd = a.options.Update(adjusted)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseWheelMsg:
		if a.chart.Active() {
			var cmd tea.Cmd
			a.chart, cmd = a.chart.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		if a.compare.Active() {
			var cmd tea.Cmd
			a.compare, cmd = a.compare.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		var cmd tea.Cmd
		switch a.activeTab {
		case TabWatchlist:
			a.watchlist, cmd = a.watchlist.Update(msg)
		case TabPortfolio:
			a.portfolio, cmd = a.portfolio.Update(msg)
		case TabAlerts:
			a.alerts, cmd = a.alerts.Update(msg)
		case TabNews:
			a.news, cmd = a.news.Update(msg)
		case TabHeatmap:
			a.heatmap, cmd = a.heatmap.Update(msg)
		case TabOptions:
			a.options, cmd = a.options.Update(msg)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
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

	case DeFiUpdateMsg:
		a.macro.UpdateDeFi(msg.Chains)
		return a, nil

	case FuturesUpdateMsg:
		a.macro.UpdateFutures(msg.Snapshots)
		return a, nil

	case EquityMarkMsg:
		a.portfolio.AppendEquityMark(msg.Mark)
		return a, nil

	case CalendarUpdateMsg:
		a.macro.UpdateEvents(msg.Events)
		return a, nil

	case AlertTriggeredMsg:
		a.alerts.AddTriggered(msg.Alert)
		a.statusbar.SetAlertCount(a.alerts.TriggeredCount())
		return a, nil

	case ConnectionStatusMsg:
		a.statusbar.SetProviderStatus(msg.Provider, msg.Connected)
		return a, nil

	case theme.ChangedMsg:
		statusbar.RebuildStyles()
		macroview.RebuildStyles()
		var cmd tea.Cmd
		a.watchlist, cmd = a.watchlist.Update(msg)
		cmds = append(cmds, cmd)
		a.chart, cmd = a.chart.Update(msg)
		cmds = append(cmds, cmd)
		a.compare, cmd = a.compare.Update(msg)
		cmds = append(cmds, cmd)
		a.portfolio, cmd = a.portfolio.Update(msg)
		cmds = append(cmds, cmd)
		a.alerts, cmd = a.alerts.Update(msg)
		cmds = append(cmds, cmd)
		a.news, cmd = a.news.Update(msg)
		cmds = append(cmds, cmd)
		a.heatmap, cmd = a.heatmap.Update(msg)
		cmds = append(cmds, cmd)
		a.detail, cmd = a.detail.Update(msg)
		cmds = append(cmds, cmd)
		a.alertDialog, cmd = a.alertDialog.Update(msg)
		cmds = append(cmds, cmd)
		a.symbolInfo, cmd = a.symbolInfo.Update(msg)
		cmds = append(cmds, cmd)
		a.options, cmd = a.options.Update(msg)
		cmds = append(cmds, cmd)
		a.correl, cmd = a.correl.Update(msg)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)

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
	case TabOptions:
		a.options.SetSize(contentW, contentH)
		content = a.options.View()
	case TabCorrel:
		a.correl.SetSize(contentW, contentH)
		content = a.correl.View()
	}

	panel := a.renderContentPanel(tabNames[a.activeTab], content, contentH)

	bottom := statusBar
	if a.palette.Active() {
		bottom = a.palette.View(a.width) + "\n" + statusBar
	}
	s := lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		panel,
		bottom,
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

// overlayCenter composites overlay on top of bg, centered. The underlying
// tab content stays visible around the modal instead of being replaced by
// blank space (which is what lipgloss.Place alone would produce).
func (a *App) overlayCenter(bg, overlay string) string {
	ow := lipgloss.Width(overlay)
	oh := lipgloss.Height(overlay)
	x := (a.width - ow) / 2
	if x < 0 {
		x = 0
	}
	y := (a.height - oh) / 2
	if y < 0 {
		y = 0
	}
	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(bg),
		lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
	)
	return c.Render()
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

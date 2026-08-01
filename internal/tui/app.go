package tui

import (
	"context"
	"fmt"
	"path/filepath"
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
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/alertdialog"
	alertsview "github.com/stxkxs/mkt/internal/tui/alerts"
	"github.com/stxkxs/mkt/internal/tui/chart"
	correlview "github.com/stxkxs/mkt/internal/tui/correlation"
	"github.com/stxkxs/mkt/internal/tui/detail"
	"github.com/stxkxs/mkt/internal/tui/format"
	heatmapview "github.com/stxkxs/mkt/internal/tui/heatmap"
	helpview "github.com/stxkxs/mkt/internal/tui/help"
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

// ConfigStatus describes how the on-disk config file looked to the
// loader. A degraded config does not stop the dashboard — mkt starts on
// built-in defaults — but the session is then showing somebody else's
// watchlist, so the App keeps a banner up for its whole lifetime and
// says whether config writes are being refused.
//
// Populate it from config.LoadResult:
//
//	app.SetConfigStatus(tui.ConfigStatus{
//	    Degraded:       res.Degraded,
//	    Path:           res.Path,
//	    Line:           res.Line,
//	    Err:            res.Err,
//	    WritesDisabled: res.Degraded && !force,
//	})
type ConfigStatus struct {
	// Degraded is true when the config file failed to parse and this
	// session is running on defaults.
	Degraded bool
	// Path is the config file that failed. Only its base name is shown.
	Path string
	// Line is the 1-based line the parse error points at, or 0 when the
	// loader could not localize it.
	Line int
	// Err is the underlying load error. Shown when Line is unknown.
	Err error
	// WritesDisabled is true when this session refuses to persist config
	// changes because it would overwrite a file it could not read.
	WritesDisabled bool
}

// banner renders the one-line degraded-config warning.
func (s ConfigStatus) banner() string {
	where := filepath.Base(s.Path)
	switch where {
	case "", ".", string(filepath.Separator):
		where = "config"
	}
	if s.Line > 0 {
		where = fmt.Sprintf("%s:%d", where, s.Line)
	}
	msg := fmt.Sprintf("⚠ %s failed to parse — running on defaults.", where)
	if s.Line <= 0 && s.Err != nil {
		msg = fmt.Sprintf("⚠ %s failed to parse (%v) — running on defaults.", where, s.Err)
	}
	if s.WritesDisabled {
		msg += " Config writes are disabled until fixed."
	}
	return msg
}

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
	help        helpview.Model

	// alertEngine is kept so the router can tell whether the alerts tab
	// actually has a rule to delete before mirroring its confirm prompt.
	alertEngine *alert.Engine

	// alertsConfirming mirrors the alerts tab's delete confirmation so
	// the root router can hand it the next key ahead of the global quit
	// binding. See armAlertConfirm.
	alertsConfirming bool

	// configStatus and unroutable drive the persistent notice rows drawn
	// under the tab bar.
	configStatus ConfigStatus
	unroutable   []string
}

// NewApp creates the root TUI model.
func NewApp(groups []watchlist.Group, cache *market.Cache, histProvider chart.HistoryProvider, portfolios []portfolio.Portfolio, alertEngine *alert.Engine, yahooProv *yahoo.Provider, coinbaseProv *coinbase.Provider) *App {
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
		correl:      correlview.New(nil, cache),
		palette:     paletteview.New(tabNames),
		statusbar:   statusbar.New(),
		alertDialog: alertdialog.New(alertEngine),
		symbolInfo:  symbolinfo.New(yahooProv),
		help:        helpview.New(),
		alertEngine: alertEngine,
	}
	a.SetWatchlistGroups(groups)
	a.statusbar.SetThemeName(theme.CurrentName)
	return a
}

// SetWatchlistGroups re-seeds every view whose universe is derived from
// the watchlist: the heatmap's sector tiles and the correlation matrix.
// Called by NewApp, and again by any caller that reloads the config —
// without it, pruning a watchlist group leaves the Heatmap tab painting
// symbols the hub no longer subscribes to.
func (a *App) SetWatchlistGroups(groups []watchlist.Group) {
	sectors := make([]heatmapview.Sector, 0, len(groups))
	for _, g := range groups {
		sectors = append(sectors, heatmapview.Sector{Name: g.Name, Symbols: canonicalAll(g.Symbols)})
	}
	a.heatmap.SetSectors(sectors)
	a.correl.SetSymbols(CanonicalSymbols(groups))
}

// CanonicalSymbols returns the deduplicated union of every group's
// symbols in the canonical spelling the hub subscribes with. It is the
// one place the TUI derives "the symbols this session cares about", and
// it is exported so callers that build the same list for the data plane
// share it instead of keeping their own copy of the same loop.
func CanonicalSymbols(groups []watchlist.Group) []string {
	var all []string
	for _, g := range groups {
		all = append(all, g.Symbols...)
	}
	return canonicalAll(all)
}

// canonicalAll normalizes a symbol list the way the hub does and drops
// duplicates. Both the heatmap and the correlation matrix key off
// incoming quotes, which always carry canonical symbols, so a
// hand-typed `btc` in the config would otherwise index a tile nothing
// ever fills — and deduplicating before normalizing would let `btc` and
// `BTC-USD` survive as two entries for the same instrument, which the
// correlation matrix would then draw as two identical rows. Canonical is
// idempotent, so this is a no-op when the caller already normalized.
func canonicalAll(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, s := range symbols {
		c := symbol.Canonical(s)
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// SetContext supplies the process context to the sub-models that own
// background streams — today the detail panel's level-2 order book — so
// they are torn down when the app exits instead of leaking a socket per
// session. Call before Run.
func (a *App) SetContext(ctx context.Context) {
	a.detail.SetContext(ctx)
}

// SetBenchmark selects the symbol the portfolio tab samples alongside
// each equity mark for its Beta readout. Pass "" to drop Beta entirely.
// Defaults to portfolioview.DefaultBenchmark.
func (a *App) SetBenchmark(sym string) {
	a.portfolio.SetBenchmark(sym)
}

// SetConfigStatus records how the config file loaded. A degraded status
// raises a banner that stays up for the whole session and marks the tab
// bar, because a silently-defaulted watchlist looks exactly like a
// correct one.
func (a *App) SetConfigStatus(s ConfigStatus) {
	a.configStatus = s
}

// SetUnroutable records the symbols the hub could not route to any
// provider (market.Hub.Start returns them). They will never produce a
// quote, so the dashboard says so rather than showing an empty row
// forever.
func (a *App) SetUnroutable(symbols []string) {
	a.unroutable = symbols
}

// LoadConfigBanner is the Load* form of SetConfigStatus, matching the
// optional interface the data plane wires models through. It assumes the
// session refuses config writes, which is the default for a degraded
// config; a caller that re-enabled them (--force) should call
// SetConfigStatus instead so the banner does not claim otherwise.
func (a *App) LoadConfigBanner(path string, line int, err error) {
	a.SetConfigStatus(ConfigStatus{
		Degraded:       true,
		Path:           path,
		Line:           line,
		Err:            err,
		WritesDisabled: true,
	})
}

// LoadUnroutableSymbols is the Load* form of SetUnroutable, matching the
// optional interface the data plane wires models through.
func (a *App) LoadUnroutableSymbols(symbols []string) {
	a.SetUnroutable(symbols)
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
		a.help.SetSize(msg.Width, msg.Height)
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

		// Help overlay guard: any key closes.
		if a.help.Active() {
			a.help, _ = a.help.Update(msg)
			return a, nil
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

		// A tab holding a confirm prompt sees the key before the global
		// bindings do. The alerts tab prompts "y: confirm, any other
		// key: cancel", so without this 'q' at the prompt quit mkt
		// instead of cancelling the delete.
		if a.activeTab != TabAlerts {
			a.alertsConfirming = false
		}
		if a.alertsConfirming {
			a.alertsConfirming = false
			var cmd tea.Cmd
			a.alerts, cmd = a.alerts.Update(msg)
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

		// Keybinding reference for the active tab
		if msg.String() == "?" {
			a.help.Open(tabNames[a.activeTab])
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

		// Watchlist shortcuts open other views, so they are handled
		// before the key reaches the watchlist model itself.
		if a.activeTab == TabWatchlist {
			if cmd, handled := a.watchlistShortcut(msg); handled {
				return a, cmd
			}
		}
		if cmd := a.forwardKeyToActiveTab(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseClickMsg:
		// The full-screen chart views own every row of the frame and draw
		// no tab bar, so they have to claim the click before the tab
		// hit-test does — otherwise a click on the chart's top row
		// switches a tab the user cannot see.
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
		// A centered modal is modal for the mouse as well: the key path
		// above returns before tab switching, so a click must not do what
		// the same modal refuses to let a key do.
		if a.modalActive() {
			return a, nil
		}
		if msg.Y < tabBarHeight {
			tab := a.tabAtX(msg.X)
			if tab >= 0 {
				// The detail panel is drawn over the content area while
				// the tab bar stays visible, so a click on a tab has to
				// close it — otherwise the tab the user picked stays
				// hidden behind the panel.
				a.detail.SetActive(false)
				a.activeTab = tab
			}
			return a, nil
		}
		// The detail panel covers the content area. Without this the
		// click would move the selection on the watchlist behind it,
		// invisibly, and the user would find a different row selected on
		// closing the panel.
		if a.detail.Active() {
			var cmd tea.Cmd
			a.detail, cmd = a.detail.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		adjusted := tea.MouseClickMsg(a.toContentCoords(tea.Mouse(msg)))
		if cmd := a.forwardMouseToActiveTab(adjusted); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case tea.MouseMotionMsg:
		// Only the full-screen chart views consume motion (for the hover
		// crosshair). Other tabs ignore it to keep the tab bar coordinate
		// math simple.
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
		return a, nil

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
		if a.modalActive() {
			return a, nil
		}
		// Same reasoning as the click path: the panel is on top, so the
		// wheel must not scroll the tab hidden underneath it.
		if a.detail.Active() {
			var cmd tea.Cmd
			a.detail, cmd = a.detail.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return a, tea.Batch(cmds...)
		}
		adjusted := tea.MouseWheelMsg(a.toContentCoords(tea.Mouse(msg)))
		if cmd := a.forwardMouseToActiveTab(adjusted); cmd != nil {
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

	case UnroutableSymbolsMsg:
		a.SetUnroutable(msg.Symbols)
		return a, nil

	case ConfigStatusMsg:
		a.SetConfigStatus(msg.Status)
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
		a.macro, cmd = a.macro.Update(msg)
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
		// Async results arrive as message types the sub-models keep
		// private, so a model with a load in flight has to be offered
		// every message it might be waiting on. Routing is unconditional
		// rather than gated on the model being visible: a fetch started
		// with 'c' or 'O' has to land even if the user tabbed away or
		// pressed esc while it was in flight, and order-book frames
		// stream in from a goroutine that outlives any one tab. Stale
		// results are each model's own problem — the chart views carry a
		// request sequence and drop anything older — so the router does
		// not try to second-guess which one is still wanted. Dropping
		// them here instead is what left the Options tab on "Loading…"
		// forever and the detail panel's order book permanently empty.
		var cmd tea.Cmd
		a.chart, cmd = a.chart.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.compare, cmd = a.compare.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.symbolInfo, cmd = a.symbolInfo.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.options, cmd = a.options.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.detail, cmd = a.detail.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return a, tea.Batch(cmds...)
}

// watchlistShortcut handles the Watch tab keys that open another view
// (detail panel, chart, alert dialog, options chain). It reports whether
// it consumed the key; anything it did not consume falls through to the
// watchlist model itself.
func (a *App) watchlistShortcut(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	sym := a.watchlist.SelectedSymbol()
	switch msg.String() {
	case "enter":
		if sym != "" {
			cmd := a.detail.SetSymbol(sym)
			a.detail.SetActive(true)
			return cmd, true
		}
		return nil, true
	case "c":
		if sym != "" {
			return a.chart.SetSymbol(sym), true
		}
		return nil, true
	case "a":
		if sym != "" {
			a.compare.AddSymbol(sym)
		}
		return nil, true
	case "C":
		if len(a.compare.Symbols()) > 0 {
			return a.compare.Open(), true
		}
		return nil, true
	case "A":
		if sym != "" {
			a.alertDialog.Open(sym, a.watchlist.CurrentPrice(sym))
		}
		return nil, true
	case "i":
		if sym != "" {
			return a.symbolInfo.Open(sym), true
		}
		return nil, true
	case "O":
		if sym != "" {
			a.activeTab = TabOptions
			return a.options.LoadSymbol(sym), true
		}
		return nil, true
	}
	return nil, false
}

// forwardKeyToActiveTab routes a key press to the model backing the
// active tab. Kept as one table so the normal path and the modal path
// (a tab holding a confirm prompt) cannot drift apart.
func (a *App) forwardKeyToActiveTab(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch a.activeTab {
	case TabWatchlist:
		a.watchlist, cmd = a.watchlist.Update(msg)
	case TabPortfolio:
		a.portfolio, cmd = a.portfolio.Update(msg)
	case TabAlerts:
		a.alerts, cmd = a.alerts.Update(msg)
		a.armAlertConfirm(msg)
	case TabChart:
		a.chart, cmd = a.chart.Update(msg)
	case TabMacro:
		a.macro, cmd = a.macro.Update(msg)
	case TabNews:
		a.news, cmd = a.news.Update(msg)
	case TabHeatmap:
		a.heatmap, cmd = a.heatmap.Update(msg)
	case TabOptions:
		a.options, cmd = a.options.Update(msg)
	case TabCorrel:
		a.correl, cmd = a.correl.Update(msg)
	}
	return cmd
}

// armAlertConfirm mirrors the alerts tab's delete confirmation. That
// prompt resolves on the very next key press ("y" confirms, anything
// else cancels), so a single one-shot flag is the whole state machine —
// but the root router has to know about it, or the global quit binding
// fires first and 'q' at the prompt kills the process. The tab exposes
// no predicate for its own prompt, so the arming rule is mirrored here;
// worst case the mirror is wrong by one key press, never by a lost
// quit.
func (a *App) armAlertConfirm(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "d", "delete":
		a.alertsConfirming = a.alertEngine != nil && len(a.alertEngine.Rules()) > 0
	default:
		a.alertsConfirming = false
	}
}

// modalActive reports whether one of the centered overlays owns input.
// They are drawn on top of the tab content, so anything they do not
// handle has to be swallowed rather than delivered to the tab behind
// them — the key path already returns early for each of these.
func (a *App) modalActive() bool {
	return a.palette.Active() || a.alertDialog.Active() || a.symbolInfo.Active() || a.help.Active()
}

// forwardMouseToActiveTab routes a mouse message to the model backing the
// active tab and returns its command. Only the tabs that consume mouse
// input are listed; the full-screen chart/compare overlays are handled by
// the caller before this point. Shared by the click and wheel handlers so
// the tab dispatch isn't written twice.
func (a *App) forwardMouseToActiveTab(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch a.activeTab {
	case TabWatchlist:
		a.watchlist, cmd = a.watchlist.Update(msg)
	case TabPortfolio:
		a.portfolio, cmd = a.portfolio.Update(msg)
	case TabAlerts:
		a.alerts, cmd = a.alerts.Update(msg)
	case TabMacro:
		a.macro, cmd = a.macro.Update(msg)
	case TabNews:
		a.news, cmd = a.news.Update(msg)
	case TabHeatmap:
		a.heatmap, cmd = a.heatmap.Update(msg)
	case TabOptions:
		a.options, cmd = a.options.Update(msg)
	case TabCorrel:
		a.correl, cmd = a.correl.Update(msg)
	}
	return cmd
}

func (a *App) View() tea.View {
	if !a.ready {
		spinner := format.SpinnerFrame(a.spinnerTick)
		return tea.NewView(theme.StyleAccentText(spinner + " Loading..."))
	}

	// Full-screen chart mode — needs AllMotion so the hover crosshair
	// can track the cursor without a click.
	if a.chart.Active() {
		s := a.chart.View()
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouseAllMotion(v)
	}

	// Comparison chart mode
	if a.compare.Active() {
		s := a.compare.View()
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouseAllMotion(v)
	}

	// Detail panel overlay
	if a.detail.Active() {
		contentW, contentH := a.contentSize(a.width, a.height)
		a.detail.SetSize(contentW, contentH)
		panel := a.renderContentPanel("Detail", a.detail.View(), contentH)
		s := a.frame(panel, a.statusbar.View())
		v := tea.NewView(s)
		v.AltScreen = true
		return withMouse(v)
	}

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

	bottom := a.statusbar.View()
	if a.palette.Active() {
		bottom = a.palette.View(a.width) + "\n" + bottom
	}
	s := a.frame(panel, bottom)

	// Overlay: alert dialog
	if a.alertDialog.Active() {
		s = a.overlayCenter(s, a.alertDialog.View())
	}

	// Overlay: symbol info
	if a.symbolInfo.Active() {
		s = a.overlayCenter(s, a.symbolInfo.View())
	}

	// Overlay: keybinding help
	if a.help.Active() {
		s = a.overlayCenter(s, a.help.View())
	}

	v := tea.NewView(s)
	v.AltScreen = true
	return withMouse(v)
}

// frame stacks the fixed chrome around a rendered content panel: tab
// bar, persistent notice rows, panel, then the bottom block. Every view
// path goes through it so the notice rows can never be drawn on one tab
// and skipped on another — which would also desynchronize the mouse
// offset from what is on screen.
func (a *App) frame(panel, bottom string) string {
	rows := make([]string, 0, 4)
	rows = append(rows, a.renderTabBar())
	rows = append(rows, a.renderNotices()...)
	rows = append(rows, panel, bottom)
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
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

// withMouseAllMotion is used by the full-screen chart views so the
// model receives MouseMotionMsg for hover-crosshair tracking.
func withMouseAllMotion(v tea.View) tea.View {
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// Fixed chrome rows around the content panel.
const (
	tabBarHeight    = 1
	statusBarHeight = 1
)

// usePanelBorders returns true if the terminal is large enough for panel borders.
func (a *App) usePanelBorders() bool {
	return a.width >= 30 && a.height >= 15
}

// notices returns the persistent warnings drawn under the tab bar, in
// priority order. They are facts about the whole session rather than
// transient toasts, so they are never dismissible: a silently-defaulted
// config or a symbol that routes nowhere looks exactly like a working
// dashboard otherwise.
func (a *App) notices() []string {
	var out []string
	if a.configStatus.Degraded {
		out = append(out, a.configStatus.banner())
	}
	if n := len(a.unroutable); n > 0 {
		noun := "symbols route"
		if n == 1 {
			noun = "symbol routes"
		}
		out = append(out, fmt.Sprintf("⚠ %d %s to no provider: %s",
			n, noun, strings.Join(a.unroutable, ", ")))
	}
	return out
}

// renderNotices styles the notice rows and clips them to the frame.
func (a *App) renderNotices() []string {
	src := a.notices()
	if len(src) == 0 {
		return nil
	}
	style := lipgloss.NewStyle().Foreground(theme.ColorYellow)
	out := make([]string, 0, len(src))
	for _, n := range src {
		out = append(out, style.Render(format.Truncate(n, a.width)))
	}
	return out
}

// contentSize returns the width and height available for content inside the panel.
func (a *App) contentSize(totalW, totalH int) (int, int) {
	h := totalH - tabBarHeight - statusBarHeight - len(a.notices())
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

// contentOrigin returns the screen coordinate of the top-left cell of the
// tab content: past the tab bar, past any notice rows, and past the
// panel's own border when one is drawn. Mouse coordinates are translated
// by it, so the hit-test is derived from the same layout the renderer
// uses instead of a second copy of the arithmetic. The missing border row
// here is what made every click on Watch/Portfolio/Alerts select the row
// below the one under the cursor.
func (a *App) contentOrigin() (int, int) {
	x, y := 0, tabBarHeight+len(a.notices())
	if a.usePanelBorders() {
		x++ // left border column
		y++ // top border row
	}
	return x, y
}

// toContentCoords rebases a mouse event from screen coordinates into the
// active tab's content coordinates.
func (a *App) toContentCoords(m tea.Mouse) tea.Mouse {
	x, y := a.contentOrigin()
	m.X -= x
	m.Y -= y
	return m
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
	top := theme.StyleBorderChar.Render("╭─") + titleRendered + theme.StyleBorderChar.Render(format.Repeat("─", topFillLen)+"╮")

	// Bottom border: ╰────────────────────────────────╯
	bottom := theme.StyleBorderChar.Render("╰" + format.Repeat("─", innerWidth) + "╯")

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
		sb.WriteString(border)
		sb.WriteString(line)
		sb.WriteString(format.Spaces(innerWidth - lipgloss.Width(line)))
		sb.WriteString(border)
		sb.WriteString("\n")
	}
	sb.WriteString(bottom)

	return sb.String()
}

// tabBarLeadPad is the leading pad rendered before the first tab label.
const tabBarLeadPad = 1

// tabSegment is one tab label as drawn in the tab bar, together with the
// column range it occupies.
type tabSegment struct {
	text  string
	start int
	width int
}

// tabSeparator is the glyph drawn between two tab labels.
func tabSeparator() string {
	return theme.StyleTabSeparator.Render(" │ ")
}

// tabSegments lays the tab labels out left to right. renderTabBar draws
// these and tabAtX hit-tests them, so the geometry on screen and the
// geometry the mouse is measured against are the same numbers rather
// than two copies of the same arithmetic.
func (a *App) tabSegments() []tabSegment {
	sepW := lipgloss.Width(tabSeparator())
	segs := make([]tabSegment, 0, len(tabNames))
	x := tabBarLeadPad
	for i, name := range tabNames {
		indicator := "◇"
		style := theme.StyleTabInactive
		if Tab(i) == a.activeTab {
			indicator = "◆"
			style = theme.StyleTabActive
		}
		text := style.Render(indicator + " " + name)
		w := lipgloss.Width(text)
		segs = append(segs, tabSegment{text: text, start: x, width: w})
		x += w + sepW
	}
	return segs
}

func (a *App) renderTabBar() string {
	segs := a.tabSegments()
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		parts = append(parts, s.text)
	}

	bar := theme.StyleTabBar.Render(format.Spaces(tabBarLeadPad)) + strings.Join(parts, tabSeparator())

	// Right side: the degraded-config marker rides alongside the
	// branding so the warning is on every tab, not only the tabs whose
	// content happens to mention it. It is dropped rather than allowed
	// to push the bar past the frame — the notice row below carries the
	// same warning and is never dropped.
	barW := lipgloss.Width(bar)
	right := theme.StyleBranding.Render("▸ mkt ")
	if a.configStatus.Degraded {
		marker := lipgloss.NewStyle().
			Background(theme.ColorTabBg).
			Foreground(theme.ColorYellow).
			Bold(true).
			Render("⚠ config ")
		if barW+lipgloss.Width(marker)+lipgloss.Width(right) <= a.width {
			right = marker + right
		}
	}

	pad := a.width - barW - lipgloss.Width(right)
	filler := theme.StyleTabBar.Render(format.Spaces(pad))
	return lipgloss.JoinHorizontal(lipgloss.Top, bar, filler, right)
}

// tabAtX returns which tab index was clicked at the given X coordinate, or -1.
func (a *App) tabAtX(x int) Tab {
	for i, s := range a.tabSegments() {
		if x >= s.start && x < s.start+s.width {
			return Tab(i)
		}
	}
	return -1
}

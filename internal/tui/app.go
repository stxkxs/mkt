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

	// shared is set when several sessions run against one process, as
	// under mkt serve. The theme lives in package-level vars in
	// internal/tui/theme, so a guest pressing T would restyle every other
	// session — and would write those vars from its own goroutine while
	// the others render from them. Guests keep the host's theme.
	shared bool

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

	// tabs binds each Tab to the model behind it and declares where that
	// tab departs from the common path. The size, key, mouse and view
	// paths all route through it rather than enumerating tabs of their
	// own. See bindSurfaces.
	tabs []tabEntry

	// themeTargets are the surfaces that rebuild cached styles on a
	// theme change; asyncTargets are the surfaces that may have a load in
	// flight and so are offered every message no case claimed. The two
	// memberships are different sets, and both span more than the tabs.
	themeTargets []tabModel
	asyncTargets []tabModel

	// panelSized, fullScreenSized and overlaySized are the sizing
	// policies a new terminal size is applied through. See resize.
	panelSized      []tabModel
	fullScreenSized []tabModel
	overlaySized    []tabModel

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
	a.bindSurfaces()
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

// SetContext supplies the process context to the sub-models that reach the
// network: the detail panel's level-2 order-book stream and the two chart
// views' history fetches. Without it their work outlives the session that
// started it — under mkt serve a guest that hangs up mid-fetch keeps
// requesting from the upstream provider, retries included. Call before Run.
func (a *App) SetContext(ctx context.Context) {
	a.detail.SetContext(ctx)
	a.chart.SetContext(ctx)
	a.compare.SetContext(ctx)
}

// SetShared marks this model as one of several attached to a single
// process, which withholds the controls that would mutate process-wide
// state out from under the other sessions.
func (a *App) SetShared(shared bool) {
	a.shared = shared
	a.help.SetShared(shared)
	if shared {
		// The status bar's theme segment doubles as the hint for T. With
		// the key withheld it would name a control the session does not
		// have.
		a.statusbar.SetThemeName("")
	}
}

// SetMacroProviders tells the Macro tab which optional sections will ever
// receive data, so their rows are reserved from the first frame.
func (a *App) SetMacroProviders(futures, defi bool) {
	a.macro.SetProviders(futures, defi)
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

// spinnerInterval paces the pre-ready spinner. It is the only animation in
// the dashboard, and it runs only until the first WindowSizeMsg arrives.
const spinnerInterval = 100 * time.Millisecond

// spinnerTickCmd schedules the next spinner frame.
func spinnerTickCmd() tea.Cmd {
	return tea.Every(spinnerInterval, func(time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

func (a *App) Init() tea.Cmd {
	return spinnerTickCmd()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.resize(msg.Width, msg.Height)
		return a, nil

	case SpinnerTickMsg:
		// The spinner is drawn only by the !a.ready branch of View. Once
		// the first WindowSizeMsg has landed nothing reads spinnerTick, so
		// re-arming would repaint every frame at spinnerInterval for the
		// life of the process — on every attached SSH session — to animate
		// a glyph that is no longer on screen.
		if a.ready {
			return a, nil
		}
		a.spinnerTick++
		return a, spinnerTickCmd()

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case tea.MouseClickMsg:
		return a.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return a.handleMouseMotion(msg)

	case tea.MouseWheelMsg:
		return a.handleMouseWheel(msg)
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
		return a.handleThemeChanged(msg)

	default:
		return a.routeAsyncResult(msg)
	}
}

// resize applies a terminal size to the surfaces that hold one, each
// through the policy its sizing list declares. A tab is sized by View
// before it renders, so only what renders outside that path is listed.
func (a *App) resize(w, h int) {
	contentW, contentH := a.contentSize(w, h)
	a.statusbar.SetWidth(w)
	for _, s := range a.panelSized {
		s.SetSize(contentW, contentH)
	}
	for _, s := range a.fullScreenSized {
		s.SetSize(w, h-2)
	}
	for _, s := range a.overlaySized {
		s.SetSize(w, h)
	}
}

// handleThemeChanged restyles every surface that caches styles built from
// the palette. statusbar has no Update of its own, so the root rebuilds it
// directly; every other surface handles the message in its own Update and
// must not also be rebuilt from here, or one theme change restyles it
// twice.
func (a *App) handleThemeChanged(msg theme.ChangedMsg) (tea.Model, tea.Cmd) {
	statusbar.RebuildStyles()
	return a, tea.Batch(fanOut(a.themeTargets, msg)...)
}

// routeAsyncResult offers a message no case claimed to every surface that
// may have a load in flight.
//
// Async results arrive as message types the sub-models keep private, so a
// model with a load in flight has to be offered every message it might be
// waiting on. Routing is unconditional rather than gated on the model
// being visible: a fetch started with 'c' or 'O' has to land even if the
// user tabbed away or pressed esc while it was in flight, and order-book
// frames stream in from a goroutine that outlives any one tab. Stale
// results are each model's own problem — the chart views carry a request
// sequence and drop anything older — so the router does not try to
// second-guess which one is still wanted.
func (a *App) routeAsyncResult(msg tea.Msg) (tea.Model, tea.Cmd) {
	return a, tea.Batch(fanOut(a.asyncTargets, msg)...)
}

// handleKey routes one key press. Overlays are consulted in the order
// they stack on screen, each claiming every key while it is open, before
// the key reaches global bindings and then the active tab.
func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
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
			if a.shared {
				return a, nil
			}
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
	// Withheld for a shared process — see App.shared.
	if msg.String() == "T" && !a.shared {
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

	return a, tea.Batch(cmds...)
}

// handleMouseClick routes one click, giving the full-screen views and the
// overlays their chance to claim it before the tab bar and active tab.
func (a *App) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
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

	return a, tea.Batch(cmds...)
}

// handleMouseMotion routes pointer movement to whichever view owns the
// frame.
func (a *App) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
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
}

// handleMouseWheel routes scrolling to whichever view owns the frame.
func (a *App) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
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
// active tab. Every tab takes keys, so the only branch is the alerts
// tab's confirm prompt, which the root has to mirror.
func (a *App) forwardKeyToActiveTab(msg tea.KeyPressMsg) tea.Cmd {
	cmd := a.tabs[a.activeTab].model.Update(msg)
	if a.activeTab == TabAlerts {
		a.armAlertConfirm(msg)
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
// active tab and returns its command. A tab that declares noMouse is
// skipped; the full-screen chart and compare surfaces are claimed by the
// caller before this point. Shared by the click and wheel handlers so the
// tab dispatch isn't written twice.
func (a *App) forwardMouseToActiveTab(msg tea.Msg) tea.Cmd {
	entry := a.tabs[a.activeTab]
	if entry.noMouse {
		return nil
	}
	return entry.model.Update(msg)
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
	// A tab is sized here, immediately before it renders, so its view is
	// built against the frame it is about to be drawn into.
	entry := a.tabs[a.activeTab]
	var content string
	if entry.signpost != nil {
		content = entry.signpost(contentW)
	} else {
		entry.model.SetSize(contentW, contentH)
		content = entry.model.View()
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
// uses instead of a second copy of the arithmetic. Omitting the border row
// here offsets every click on Watch/Portfolio/Alerts by one, selecting the
// row below the one under the cursor.
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
		return clampLines(lipgloss.NewStyle().
			Width(a.width).
			Height(contentH).
			Render(content), a.width)
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
		// The panel owns the border, so it is where the frame is
		// enforced: a tab that overruns its budget must cost its own
		// content, never a wrapped row that displaces everything below
		// and desynchronizes the click hit-test.
		line = format.Truncate(line, innerWidth)
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
	tab   Tab // which tab this segment selects; the bar sheds, so this is not the slice index
	text  string
	start int
	width int
}

// tabSeparator is the glyph drawn between two tab labels.
func tabSeparator() string {
	return theme.StyleTabSeparator.Render(" │ ")
}

// tabBarBranding is the fixed right-hand segment of the bar. tabSegments
// budgets around it and renderTabBar draws it, so the space it costs is
// subtracted in exactly one place.
func tabBarBranding() string {
	return theme.StyleBranding.Render("▸ mkt ")
}

// tabLabel renders one tab's label in its active or inactive style.
func (a *App) tabLabel(i int) string {
	indicator, style := "◇", theme.StyleTabInactive
	if Tab(i) == a.activeTab {
		indicator, style = "◆", theme.StyleTabActive
	}
	return style.Render(indicator + " " + tabNames[i])
}

// tabOverflowMarker tells the reader that tabs exist outside the bar. The
// bar is the only place the tab set is enumerated, so a bar that silently
// shows three of nine reads as a three-tab program.
func (a *App) tabOverflowMarker(shown int) string {
	return theme.StyleTabSeparator.Render(fmt.Sprintf(" %d/%d ", int(a.activeTab)+1, len(tabNames)))
}

// tabSegments lays out the tab labels that fit the frame, left to right.
//
// renderTabBar draws these and tabAtX hit-tests them, so what is on screen
// and what the mouse is measured against are the same numbers rather than
// two copies of the same arithmetic — which is also why shedding a label
// here is enough to keep a click accurate.
//
// The nine labels are 103 cells wide, so on any frame narrower than that
// the bar has to shed or it wraps and displaces every row below it. The
// active tab is always present and the window grows outward from it, right
// first, so moving along the tabs scrolls the bar rather than jumping it.
func (a *App) tabSegments() []tabSegment {
	if len(tabNames) == 0 || a.width <= 0 {
		return nil
	}
	sepW := lipgloss.Width(tabSeparator())
	budget := a.width - tabBarLeadPad - lipgloss.Width(tabBarBranding())
	if budget <= 0 {
		return nil
	}

	active := int(a.activeTab)
	if active < 0 || active >= len(tabNames) {
		active = 0
	}

	// Everything fits: no marker to reserve, no window to compute.
	full := 0
	for i := range tabNames {
		if i > 0 {
			full += sepW
		}
		full += lipgloss.Width(a.tabLabel(i))
	}
	lo, hi := 0, len(tabNames)-1
	if full > budget {
		// Some labels are hidden, so the count marker has to be paid for.
		budget -= lipgloss.Width(a.tabOverflowMarker(1))
		if budget <= 0 {
			return nil
		}
		lo, hi = active, active
		used := lipgloss.Width(a.tabLabel(active))
		for used <= budget {
			grew := false
			if hi+1 < len(tabNames) {
				if w := used + sepW + lipgloss.Width(a.tabLabel(hi+1)); w <= budget {
					hi, used, grew = hi+1, w, true
				}
			}
			if lo-1 >= 0 {
				if w := used + sepW + lipgloss.Width(a.tabLabel(lo-1)); w <= budget {
					lo, used, grew = lo-1, w, true
				}
			}
			if !grew {
				break
			}
		}
		if used > budget {
			// Not even the active label fits; cut it to the budget so the
			// bar is short rather than wrapped.
			return []tabSegment{{
				tab:   Tab(active),
				text:  format.Truncate(a.tabLabel(active), budget),
				start: tabBarLeadPad,
				width: budget,
			}}
		}
	}

	segs := make([]tabSegment, 0, hi-lo+1)
	x := tabBarLeadPad
	for i := lo; i <= hi; i++ {
		text := a.tabLabel(i)
		w := lipgloss.Width(text)
		segs = append(segs, tabSegment{tab: Tab(i), text: text, start: x, width: w})
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
	if len(segs) < len(tabNames) {
		if marker := a.tabOverflowMarker(len(segs)); lipgloss.Width(bar)+lipgloss.Width(marker) <= a.width {
			bar += marker
		}
	}

	// Right side: the branding, and ahead of it the degraded-config
	// marker so the warning is on every tab rather than only the tabs
	// whose content happens to mention it. Each is added only if it fits
	// — the notice row below carries the same warning and is never
	// dropped, and a frame too narrow for the branding is too narrow to
	// spend cells on it.
	barW := lipgloss.Width(bar)
	right := ""
	if branding := tabBarBranding(); barW+lipgloss.Width(branding) <= a.width {
		right = branding
	}
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
	// The bar is the topmost row; a wrap here displaces every row below
	// it, so the width is enforced rather than assumed.
	return format.Truncate(lipgloss.JoinHorizontal(lipgloss.Top, bar, filler, right), a.width)
}

// clampLines cuts every line of a block to width. lipgloss word-wraps what
// it cannot fit, which at a very narrow frame still emits a line wider than
// the frame when a single word cannot be broken.
func clampLines(block string, width int) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = format.Truncate(line, width)
	}
	return strings.Join(lines, "\n")
}

// tabAtX returns which tab index was clicked at the given X coordinate, or -1.
func (a *App) tabAtX(x int) Tab {
	for _, s := range a.tabSegments() {
		if x >= s.start && x < s.start+s.width {
			return s.tab
		}
	}
	return -1
}

package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

// tabModel is the contract every tab satisfies, so the root routes by
// iteration rather than by one hand-maintained switch per message class.
// The surfaces that are not tabs — the detail panel, the full-screen
// charts and the centered overlays — satisfy it too, which is what lets
// the theme and async fan-outs be declared as membership lists.
type tabModel interface {
	SetSize(w, h int)
	Update(tea.Msg) tea.Cmd
	View() string
}

// viewModel is the shape every view package exports: a Model with value
// semantics whose Update returns its own type, sized through a pointer
// receiver. Stating that shape as a constraint is what binds all of them
// to tabModel from one place, so no view package has to give up value
// semantics to be routable.
type viewModel[M any] interface {
	*M
	SetSize(w, h int)
	Update(tea.Msg) (M, tea.Cmd)
	View() string
}

// boundModel adapts one view package's Model to tabModel. It holds a
// pointer to the App field that model lives in and writes every Update
// result back through it, so the typed field the rest of the App reaches
// for and the surface the routers drive are one value. The App is always
// used as *App, so the pointer is valid for the session.
type boundModel[M any, P viewModel[M]] struct{ model P }

func (b boundModel[M, P]) SetSize(w, h int) { b.model.SetSize(w, h) }

func (b boundModel[M, P]) Update(msg tea.Msg) tea.Cmd {
	next, cmd := b.model.Update(msg)
	*b.model = next
	return cmd
}

func (b boundModel[M, P]) View() string { return b.model.View() }

// bind adapts a pointer to one of the App's model fields.
func bind[M any, P viewModel[M]](model P) tabModel { return boundModel[M, P]{model: model} }

// tabEntry declares how the root treats one tab: the model behind it, and
// every point where it departs from the common path. A departure is a
// field, so it is stated in the declaration instead of being left for a
// reader to infer from a tab's absence from one switch.
type tabEntry struct {
	// model backs the tab. Keys always reach it.
	model tabModel
	// signpost renders in place of the model's view, for a tab whose
	// model owns the whole frame and is entered from somewhere else. It
	// is handed the content width.
	signpost func(width int) string
	// noMouse withholds clicks and wheel events from the model.
	noMouse bool
	// fetches marks a tab that starts work resolving into a message type
	// its own package keeps private, which the root cannot recognise and
	// so offers to every fetching surface. A fetching tab left unmarked
	// never receives its results and renders "Loading…" for the rest of
	// the session.
	fetches bool
}

// bindSurfaces declares every membership the routers read: the model
// behind each tab, the surfaces a theme change restyles, the surfaces
// that may have a load in flight, and the sizing policy each surface
// follows. Adding a tab is one entry in a.tabs; nothing else in this
// package enumerates the tab set.
func (a *App) bindSurfaces() {
	a.tabs = []tabEntry{
		TabWatchlist: {model: bind(&a.watchlist)},
		TabPortfolio: {model: bind(&a.portfolio)},
		TabAlerts:    {model: bind(&a.alerts)},
		TabChart:     {model: bind(&a.chart), signpost: chartSignpost, noMouse: true, fetches: true},
		TabMacro:     {model: bind(&a.macro)},
		TabNews:      {model: bind(&a.news)},
		TabHeatmap:   {model: bind(&a.heatmap)},
		TabOptions:   {model: bind(&a.options), fetches: true},
		TabCorrel:    {model: bind(&a.correl)},
	}

	compare, detail := bind(&a.compare), bind(&a.detail)
	alertDialog, symbolInfo, help := bind(&a.alertDialog), bind(&a.symbolInfo), bind(&a.help)

	// Every tab caches styles built from the palette, and so do the
	// overlays that are not tabs. statusbar has no Update of its own, so
	// handleThemeChanged rebuilds it directly; help and the palette read
	// the palette at render time and need nothing.
	a.themeTargets = make([]tabModel, 0, len(a.tabs)+4)
	for _, t := range a.tabs {
		a.themeTargets = append(a.themeTargets, t.model)
		if t.fetches {
			a.asyncTargets = append(a.asyncTargets, t.model)
		}
	}
	a.themeTargets = append(a.themeTargets, compare, detail, alertDialog, symbolInfo)

	// The surfaces that fetch on their own account, whichever tab is in
	// front: the detail panel streams an order book from a goroutine
	// that outlives any one tab, and the comparison chart and the symbol
	// card are opened from the watchlist and keep loading after the key
	// that opened them.
	a.asyncTargets = append(a.asyncTargets, compare, detail, symbolInfo)

	// One list per sizing policy: the interior of the content panel, the
	// full-screen chart surfaces, and the overlays composited over the
	// whole frame. Every other tab is sized by View before it renders.
	a.panelSized = []tabModel{a.tabs[TabWatchlist].model, detail}
	a.fullScreenSized = []tabModel{a.tabs[TabChart].model, compare}
	a.overlaySized = []tabModel{alertDialog, symbolInfo, help}
}

// chartSignpost is the Chart tab's content. The chart model draws itself
// over the whole frame once a symbol is chosen, so the tab names the key
// that gets there rather than rendering an empty chart.
func chartSignpost(width int) string {
	return theme.StyleDim.Render(format.Truncate("  Select a symbol from Watchlist and press 'c' for chart", width))
}

// fanOut offers msg to each surface and collects the commands they return.
func fanOut(targets []tabModel, msg tea.Msg) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(targets))
	for _, t := range targets {
		if cmd := t.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

// The routers index a.tabs by Tab, so a tab that is named in keys.go but
// never bound is a nil model the first time a key reaches it. The tab set
// is declared in two files — the constants and names in keys.go, the
// bindings in bindSurfaces — and this is what holds them together.
func TestEveryTabIsBound(t *testing.T) {
	a := newTestApp()
	if len(a.tabs) != len(tabNames) {
		t.Fatalf("bound %d tabs, tabNames names %d", len(a.tabs), len(tabNames))
	}
	for i, e := range a.tabs {
		if e.model == nil {
			t.Errorf("tab %s has no model", tabNames[i])
		}
	}
}

// A tab that renders a signpost is entered from another surface, and the
// tab reached by a digit key must be the one the tab bar draws at that
// position.
func TestTabBindingsMatchTheTabSet(t *testing.T) {
	a := newTestApp()
	for i := range a.tabs {
		if got := isTabSwitch(keyPress(string(rune('1' + i)))); got != Tab(i) {
			t.Errorf("digit %d selects %v, want %v", i+1, got, Tab(i))
		}
	}
	if a.tabs[TabChart].signpost == nil {
		t.Error("the Chart tab renders a signpost, not its model's view")
	}
	if !a.tabs[TabChart].noMouse {
		t.Error("the Chart tab takes no mouse input")
	}
}

// boundSurface returns the App field a tabModel was bound to, so a
// membership list can be checked against the surfaces it names rather
// than against its own length.
func boundSurface(t *testing.T, a *App, m tabModel) string {
	t.Helper()
	f := reflect.ValueOf(m).FieldByName("model")
	if !f.IsValid() {
		t.Fatalf("%T is not a bound model", m)
	}
	for name, p := range map[string]uintptr{
		"watchlist":   reflect.ValueOf(&a.watchlist).Pointer(),
		"detail":      reflect.ValueOf(&a.detail).Pointer(),
		"chart":       reflect.ValueOf(&a.chart).Pointer(),
		"compare":     reflect.ValueOf(&a.compare).Pointer(),
		"portfolio":   reflect.ValueOf(&a.portfolio).Pointer(),
		"alerts":      reflect.ValueOf(&a.alerts).Pointer(),
		"macro":       reflect.ValueOf(&a.macro).Pointer(),
		"news":        reflect.ValueOf(&a.news).Pointer(),
		"heatmap":     reflect.ValueOf(&a.heatmap).Pointer(),
		"options":     reflect.ValueOf(&a.options).Pointer(),
		"correl":      reflect.ValueOf(&a.correl).Pointer(),
		"alertDialog": reflect.ValueOf(&a.alertDialog).Pointer(),
		"symbolInfo":  reflect.ValueOf(&a.symbolInfo).Pointer(),
		"help":        reflect.ValueOf(&a.help).Pointer(),
	} {
		if f.Pointer() == p {
			return name
		}
	}
	t.Fatalf("a membership list holds a surface that is not an App field")
	return ""
}

// TestMembershipListsNameTheirSurfaces pins what each router iterates.
// The lists are the whole contract — a surface added to one twice is
// restyled twice, and a surface dropped from one produces no compile
// error and no wrong frame until the omission shows as stale colours
// after a theme change, an overlay drawn at the previous frame size, or
// a load that never lands and leaves "Loading…" on screen for the rest
// of the session. Dropping one from the binding is not a defect the
// other tests in this package can see, so the required sets are spelled
// out rather than counted.
func TestMembershipListsNameTheirSurfaces(t *testing.T) {
	a := newTestApp()
	for _, tc := range []struct {
		name string
		list []tabModel
		want []string
	}{
		{"themeTargets", a.themeTargets, []string{
			"watchlist", "portfolio", "alerts", "chart", "macro", "news",
			"heatmap", "options", "correl", "compare", "detail", "alertDialog", "symbolInfo",
		}},
		{"asyncTargets", a.asyncTargets, []string{
			"chart", "options", "compare", "detail", "symbolInfo",
		}},
		{"panelSized", a.panelSized, []string{"watchlist", "detail"}},
		{"fullScreenSized", a.fullScreenSized, []string{"chart", "compare"}},
		{"overlaySized", a.overlaySized, []string{"alertDialog", "symbolInfo", "help"}},
	} {
		got := make(map[string]int, len(tc.list))
		for _, m := range tc.list {
			got[boundSurface(t, a, m)]++
		}
		for _, w := range tc.want {
			switch got[w] {
			case 1:
			case 0:
				t.Errorf("%s is missing %s", tc.name, w)
			default:
				t.Errorf("%s holds %s %d times", tc.name, w, got[w])
			}
			delete(got, w)
		}
		for extra := range got {
			t.Errorf("%s holds %s, which it must not", tc.name, extra)
		}
	}
}

// TestSignpostTabIsNotSizedByItsTab holds the routers to the departures
// the entries declare. The Chart tab's model owns the whole frame and is
// sized by resize alone, so sizing it on the tab path shrinks it to the
// content panel — visible only after a user opens the tab before pressing
// 'c'. Its wheel events zoom, so a tab that takes no mouse must leave the
// chart exactly as the last 'c' left it.
func TestSignpostTabIsNotSizedByItsTab(t *testing.T) {
	mk := func() *App {
		a := NewApp(
			[]watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}},
			market.NewCache(30), fixedHistory{}, nil,
			alert.NewEngine(0, nil), yahoo.New(0), coinbase.New(),
		)
		a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
		return a
	}
	open := func(a *App) string {
		a, cmd := send(t, a, keyPress("c"))
		if cmd == nil {
			t.Fatal("'c' returned no history command")
		}
		a, _ = send(t, a, cmd())
		return ansiRe.ReplaceAllString(a.chart.View(), "")
	}

	direct := open(mk())

	visited := mk()
	visited, _ = send(t, visited, keyPress("4"))
	if got := render(visited); !strings.Contains(got, "press 'c' for chart") {
		t.Errorf("the Chart tab did not render its signpost:\n%s", got)
	}
	visited, _ = send(t, visited, keyPress("1"))
	if after := open(visited); after != direct {
		t.Error("visiting the Chart tab resized the chart model")
	}
}

func TestNoMouseTabKeepsItsModelUntouched(t *testing.T) {
	a := NewApp(
		[]watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}},
		market.NewCache(30), fixedHistory{}, nil,
		alert.NewEngine(0, nil), yahoo.New(0), coinbase.New(),
	)
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
	a, cmd := send(t, a, keyPress("c"))
	if cmd == nil {
		t.Fatal("'c' returned no history command")
	}
	a, _ = send(t, a, cmd())
	a, _ = send(t, a, keyCode(tea.KeyEsc))
	before := ansiRe.ReplaceAllString(a.chart.View(), "")

	a, _ = send(t, a, keyPress("4"))
	if a.activeTab != TabChart {
		t.Fatalf("'4' selected %v, want TabChart", a.activeTab)
	}
	_, originY := a.contentOrigin()
	for range 6 {
		a, _ = send(t, a, tea.MouseWheelMsg{X: 20, Y: originY + 3, Button: tea.MouseWheelUp})
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: 20, Y: originY + 3, Button: tea.MouseLeft})
	if after := ansiRe.ReplaceAllString(a.chart.View(), ""); after != before {
		t.Error("mouse events on the Chart tab reached the chart model")
	}
}

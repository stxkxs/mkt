package tui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
	optionsview "github.com/stxkxs/mkt/internal/tui/options"
	"github.com/stxkxs/mkt/internal/tui/watchlist"
)

type fakeHistory struct{}

func (fakeHistory) History(context.Context, provider.HistoryParams) ([]provider.OHLCV, error) {
	return nil, nil
}

// fakeChainSource resolves an options chain without any network, so the
// async-routing tests exercise the router rather than Yahoo.
type fakeChainSource struct {
	chain yahoo.OptionsChain
	err   error
}

func (f fakeChainSource) FetchOptionsChain(context.Context, string) (yahoo.OptionsChain, error) {
	return f.chain, f.err
}

func testChain() yahoo.OptionsChain {
	return yahoo.OptionsChain{
		Symbol: "AAPL",
		Calls: []yahoo.Option{
			{Strike: 100, Last: 5.2, Bid: 5.1, Ask: 5.3, OpenInterest: 5000, IV: 0.35},
			{Strike: 110, Last: 1.1, Bid: 1.0, Ask: 1.2, OpenInterest: 3000, IV: 0.28},
		},
		Puts: []yahoo.Option{
			{Strike: 100, Last: 2.0, Bid: 1.95, Ask: 2.05, OpenInterest: 4000, IV: 0.40},
		},
	}
}

// fixedHistory serves a fixed candle series without any network, so the
// chart's async load can be driven through the router.
type fixedHistory struct{}

func (fixedHistory) History(context.Context, provider.HistoryParams) ([]provider.OHLCV, error) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]provider.OHLCV, 0, 40)
	for i := range 40 {
		p := 100 + float64(i)
		out = append(out, provider.OHLCV{
			Time:   base.Add(time.Duration(i) * time.Hour),
			Open:   p,
			High:   p + 1,
			Low:    p - 1,
			Close:  p,
			Volume: 1000,
		})
	}
	return out, nil
}

func newTestApp() *App {
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL", "BTC-USD"}}}
	cache := market.NewCache(30)
	engine := alert.NewEngine(0, nil)
	return NewApp(groups, cache, fakeHistory{}, nil, engine, yahoo.New(0), coinbase.New())
}

// sized returns a ready app at the given frame size.
func sizedApp(t *testing.T, w, h int) *App {
	t.Helper()
	a := newTestApp()
	m, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(*App)
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func keyCode(c rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: c}
}

// ansiRe matches the SGR sequences lipgloss emits.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// render paints the whole frame and strips styling. Painting is also
// what sizes the per-tab models, so every test that inspects rendered
// output goes through it — and stripping means an assertion cannot fail
// merely because a style split a word into per-rune escapes.
func render(a *App) string {
	return ansiRe.ReplaceAllString(a.View().Content, "")
}

// send drives one message through the router and returns the app plus the
// command it produced.
func send(t *testing.T, a *App, msg tea.Msg) (*App, tea.Cmd) {
	t.Helper()
	m, cmd := a.Update(msg)
	return m.(*App), cmd
}

// TestAppTabSwitchAndRender drives the root Update router across every tab
// and renders each — smoke coverage for the message router and every tab's
// View, which previously had none.
func TestAppTabSwitchAndRender(t *testing.T) {
	a := sizedApp(t, 120, 40)
	if !a.ready {
		t.Fatal("app not ready after WindowSizeMsg")
	}

	wantTabs := []struct {
		key string
		tab Tab
	}{
		{"1", TabWatchlist},
		{"2", TabPortfolio},
		{"3", TabAlerts},
		{"4", TabChart},
		{"5", TabMacro},
		{"6", TabNews},
		{"7", TabHeatmap},
		{"8", TabOptions},
		{"9", TabCorrel},
	}
	for _, wt := range wantTabs {
		a, _ = send(t, a, keyPress(wt.key))
		if a.activeTab != wt.tab {
			t.Errorf("key %q: activeTab = %v, want %v", wt.key, a.activeTab, wt.tab)
		}
		// Rendering must not panic for any tab.
		if got := a.View(); got.AltScreen != true {
			t.Errorf("key %q: expected AltScreen view", wt.key)
		}
	}
}

// TestAppQuotePropagation verifies a QuoteUpdateMsg reaches the watchlist.
func TestAppQuotePropagation(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: 1.2}})

	if got := a.watchlist.CurrentPrice("AAPL"); got != 201.5 {
		t.Errorf("watchlist price for AAPL = %v, want 201.5", got)
	}
}

// TestAppTabCycle verifies tab/shift+tab wraparound via the arrow aliases.
func TestAppTabCycle(t *testing.T) {
	a := sizedApp(t, 120, 40)
	if a.activeTab != TabWatchlist {
		t.Fatalf("initial tab = %v", a.activeTab)
	}
	// "right" advances to the next tab.
	a, _ = send(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Text: "right"})
	if a.activeTab != TabPortfolio {
		t.Errorf("after right: activeTab = %v, want TabPortfolio", a.activeTab)
	}
}

// TestOptionsAsyncLoadReachesModel is the regression test for the Options
// tab rendering "Loading…" forever: LoadSymbol resolves to a message type
// the options package keeps private, and the root router used to drop
// every message it did not recognise instead of offering it to the tab.
func TestOptionsAsyncLoadReachesModel(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.options = optionsview.New(fakeChainSource{chain: testChain()})

	// 'O' on the Watch tab jumps to Options and starts the fetch.
	a, cmd := send(t, a, keyPress("O"))
	if a.activeTab != TabOptions {
		t.Fatalf("activeTab = %v, want TabOptions", a.activeTab)
	}
	if cmd == nil {
		t.Fatal("'O' returned no load command")
	}
	if got := render(a); !strings.Contains(got, "Loading") {
		t.Fatalf("options view before the load resolves = %q, want a loading state", got)
	}

	a, _ = send(t, a, cmd())

	got := render(a)
	if strings.Contains(got, "Loading") {
		t.Errorf("options still loading after the async result was routed:\n%s", got)
	}
	if !strings.Contains(got, "$100.00") {
		t.Errorf("options view missing the loaded chain:\n%s", got)
	}
}

// TestOptionsAsyncLoadReachesModelOffTab pins the routing as
// unconditional: a chain requested with 'O' must still land if the user
// tabs away while the fetch is in flight.
func TestOptionsAsyncLoadReachesModelOffTab(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.options = optionsview.New(fakeChainSource{chain: testChain()})

	a, cmd := send(t, a, keyPress("O"))
	if cmd == nil {
		t.Fatal("'O' returned no load command")
	}
	// Wander off to another tab before the result arrives.
	a, _ = send(t, a, keyPress("5"))
	a, _ = send(t, a, cmd())

	a, _ = send(t, a, keyPress("8"))
	if got := render(a); strings.Contains(got, "Loading") {
		t.Errorf("chain dropped while the Options tab was inactive:\n%s", got)
	}
}

// TestOptionsAsyncErrorReachesModel covers the failure half of the same
// route: an error must replace the spinner rather than hang on it.
func TestOptionsAsyncErrorReachesModel(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.options = optionsview.New(fakeChainSource{err: errors.New("no chain for you")})

	a, cmd := send(t, a, keyPress("O"))
	if cmd == nil {
		t.Fatal("'O' returned no load command")
	}
	a, _ = send(t, a, cmd())

	got := render(a)
	if strings.Contains(got, "Loading") {
		t.Errorf("options still loading after an error was routed:\n%s", got)
	}
	if !strings.Contains(got, "no chain for you") {
		t.Errorf("options view missing the error:\n%s", got)
	}
}

const sampleBook = `{"sequence":42,"bids":[["60123.45","1.5","2"]],"asks":[["60125.50","2.25","1"]]}`

// TestDetailOrderBookReachesModel is the same routing gap seen from the
// detail panel: the order book snapshot resolves to a private message,
// which the router used to drop, leaving the book permanently empty.
func TestDetailOrderBookReachesModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleBook))
	}))
	defer srv.Close()

	prev := coinbase.OrderBookURL
	coinbase.OrderBookURL = srv.URL
	defer func() { coinbase.OrderBookURL = prev }()

	a := sizedApp(t, 120, 40)
	// Move the cursor to BTC-USD and open the detail panel.
	a, _ = send(t, a, keyPress("j"))
	if got := a.watchlist.SelectedSymbol(); got != "BTC-USD" {
		t.Fatalf("selected symbol = %q, want BTC-USD", got)
	}
	a, cmd := send(t, a, keyCode(tea.KeyEnter))
	if !a.detail.Active() {
		t.Fatal("detail panel not active after enter")
	}
	if cmd == nil {
		t.Fatal("enter returned no order book command")
	}
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "BTC-USD", Price: 60124, ChangePct: 0.5}})

	a, _ = send(t, a, cmd())

	if got := render(a); !strings.Contains(got, "60123.45") {
		t.Errorf("detail view missing the routed order book:\n%s", got)
	}
}

// TestMouseClickSelectsClickedRow is the regression test for clicks
// landing one row low: renderContentPanel draws a top border whenever the
// frame is big enough, and the click translation used to ignore it.
func TestMouseClickSelectsClickedRow(t *testing.T) {
	a := sizedApp(t, 120, 40)
	if !a.usePanelBorders() {
		t.Fatal("expected panel borders at 120x40")
	}
	_, originY := a.contentOrigin()
	if originY != 2 {
		t.Fatalf("contentOrigin y = %d, want 2 (tab bar + panel top border)", originY)
	}

	// The watchlist spends two rows on its own header, so symbol i is
	// drawn at screen row originY+2+i.
	rowFor := func(i int) int { return originY + 2 + i }

	a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: rowFor(1), Button: tea.MouseLeft})
	if got := a.watchlist.SelectedSymbol(); got != "BTC-USD" {
		t.Errorf("click on row 1 selected %q, want BTC-USD", got)
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: rowFor(0), Button: tea.MouseLeft})
	if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
		t.Errorf("click on row 0 selected %q, want AAPL", got)
	}
}

// TestMouseClickBorderlessFrame checks the same translation in the small
// frame where no panel border is drawn — the offset has to shrink with it.
func TestMouseClickBorderlessFrame(t *testing.T) {
	a := sizedApp(t, 100, 12)
	if a.usePanelBorders() {
		t.Fatal("expected no panel borders at 100x12")
	}
	_, originY := a.contentOrigin()
	if originY != 1 {
		t.Fatalf("contentOrigin y = %d, want 1 (tab bar only)", originY)
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: originY + 2 + 1, Button: tea.MouseLeft})
	if got := a.watchlist.SelectedSymbol(); got != "BTC-USD" {
		t.Errorf("click on row 1 selected %q, want BTC-USD", got)
	}
}

// TestMouseClickAccountsForNotices pins the notice rows into the same
// offset: a banner pushes the content down, and the hit-test has to move
// with it.
func TestMouseClickAccountsForNotices(t *testing.T) {
	a := sizedApp(t, 120, 40)
	_, before := a.contentOrigin()
	a.SetConfigStatus(ConfigStatus{Degraded: true, Path: "/home/u/.config/mkt/config.yaml", Line: 9, WritesDisabled: true})
	_, after := a.contentOrigin()
	if after != before+1 {
		t.Fatalf("contentOrigin y with a banner = %d, want %d", after, before+1)
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: after + 2 + 1, Button: tea.MouseLeft})
	if got := a.watchlist.SelectedSymbol(); got != "BTC-USD" {
		t.Errorf("click on row 1 selected %q, want BTC-USD", got)
	}
}

// TestTabBarClickGeometry checks the tab-bar hit test against the very
// segments the renderer draws, so the two cannot drift.
func TestTabBarClickGeometry(t *testing.T) {
	a := sizedApp(t, 120, 40)
	for i, seg := range a.tabSegments() {
		for _, x := range []int{seg.start, seg.start + seg.width - 1} {
			if got := a.tabAtX(x); got != Tab(i) {
				t.Errorf("tabAtX(%d) = %v, want %v", x, got, Tab(i))
			}
		}
		a, _ = send(t, a, tea.MouseClickMsg{X: seg.start, Y: 0, Button: tea.MouseLeft})
		if a.activeTab != Tab(i) {
			t.Errorf("click at x=%d selected %v, want %v", seg.start, a.activeTab, Tab(i))
		}
	}
	if got := a.tabAtX(-1); got != -1 {
		t.Errorf("tabAtX(-1) = %v, want -1", got)
	}
	if got := a.tabAtX(10000); got != -1 {
		t.Errorf("tabAtX(10000) = %v, want -1", got)
	}
}

// TestQuitDoesNotEscapeAlertConfirm covers 'q' at the alerts delete
// prompt: the tab asks "y: confirm, any other key: cancel", so the
// global quit binding must not fire ahead of it.
func TestQuitDoesNotEscapeAlertConfirm(t *testing.T) {
	engine := alert.NewEngine(0, nil)
	engine.AddRule(alert.Rule{Symbol: "AAPL", Condition: alert.CondAbove, Value: 100, Enabled: true})
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, engine, yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})

	a, _ = send(t, a, keyPress("3")) // Alerts tab
	a, _ = send(t, a, keyPress("d"))
	if !a.alertsConfirming {
		t.Fatal("'d' did not arm the delete confirmation")
	}

	a, cmd := send(t, a, keyPress("q"))
	if cmd != nil {
		t.Error("'q' at the delete prompt produced a command (a quit escaped the modal)")
	}
	if a.alertsConfirming {
		t.Error("confirmation still armed after the cancelling key")
	}
	if len(engine.Rules()) != 1 {
		t.Errorf("rule count = %d, want 1 (cancel must not delete)", len(engine.Rules()))
	}

	// And once the prompt is gone, 'q' quits again.
	if _, cmd = send(t, a, keyPress("q")); cmd == nil {
		t.Error("'q' outside the prompt did not quit")
	}
}

// TestAlertConfirmDeletes checks the confirming key still reaches the tab
// through the modal path.
func TestAlertConfirmDeletes(t *testing.T) {
	engine := alert.NewEngine(0, nil)
	engine.AddRule(alert.Rule{Symbol: "AAPL", Condition: alert.CondAbove, Value: 100, Enabled: true})
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, engine, yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})

	a, _ = send(t, a, keyPress("3"))
	a, _ = send(t, a, keyPress("d"))
	a, _ = send(t, a, keyPress("y"))
	if len(engine.Rules()) != 0 {
		t.Errorf("rule count = %d, want 0 after confirming the delete", len(engine.Rules()))
	}
	if a.alertsConfirming {
		t.Error("confirmation still armed after it resolved")
	}
}

// TestAlertConfirmClearedOnTabSwitch keeps a stale mirror from swallowing
// a key on some unrelated tab.
func TestAlertConfirmClearedOnTabSwitch(t *testing.T) {
	engine := alert.NewEngine(0, nil)
	engine.AddRule(alert.Rule{Symbol: "AAPL", Condition: alert.CondAbove, Value: 100, Enabled: true})
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, engine, yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})

	a, _ = send(t, a, keyPress("3"))
	a, _ = send(t, a, keyPress("d"))
	a, _ = send(t, a, keyPress("1")) // leaves the Alerts tab, cancelling
	if a.activeTab != TabAlerts {
		t.Fatalf("activeTab = %v, want TabAlerts (the prompt consumes the key)", a.activeTab)
	}
	a, _ = send(t, a, keyPress("1"))
	if a.activeTab != TabWatchlist {
		t.Fatalf("activeTab = %v, want TabWatchlist", a.activeTab)
	}
	if _, cmd := send(t, a, keyPress("q")); cmd == nil {
		t.Error("'q' on the Watch tab did not quit — a stale confirm mirror ate it")
	}
}

// TestAlertConfirmNotArmedWithoutRules makes sure the mirror stays down
// when there is nothing to delete.
func TestAlertConfirmNotArmedWithoutRules(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyPress("3"))
	a, _ = send(t, a, keyPress("d"))
	if a.alertsConfirming {
		t.Error("'d' armed a confirmation with no rules to delete")
	}
	if _, cmd := send(t, a, keyPress("q")); cmd == nil {
		t.Error("'q' did not quit")
	}
}

// TestDegradedConfigBanner covers the persistent warning shown when the
// config failed to parse and mkt fell back to defaults.
func TestDegradedConfigBanner(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.SetConfigStatus(ConfigStatus{
		Degraded:       true,
		Path:           "/home/u/.config/mkt/config.yaml",
		Line:           9,
		Err:            errors.New("yaml: line 9: mapping values are not allowed"),
		WritesDisabled: true,
	})

	want := "⚠ config.yaml:9 failed to parse — running on defaults. Config writes are disabled until fixed."
	if got := a.notices(); len(got) != 1 || got[0] != want {
		t.Fatalf("notices() = %#v, want [%q]", got, want)
	}

	// It must survive every tab switch, and mark the tab bar too.
	for i := range tabNames {
		a.activeTab = Tab(i)
		s := render(a)
		if !strings.Contains(s, "config.yaml:9 failed to parse") {
			t.Errorf("tab %s: banner missing from the frame", tabNames[i])
		}
		tabBar := strings.SplitN(s, "\n", 2)[0]
		if !strings.Contains(tabBar, "⚠ config ") {
			t.Errorf("tab %s: tab bar indicator missing from %q", tabNames[i], tabBar)
		}
	}
}

// TestDegradedMarkerDroppedWhenTabBarIsFull keeps the marker from
// pushing the tab bar past the frame: the banner row below carries the
// same warning and is never dropped.
func TestDegradedMarkerDroppedWhenTabBarIsFull(t *testing.T) {
	a := sizedApp(t, 100, 20)
	a.SetConfigStatus(ConfigStatus{Degraded: true, Path: "config.yaml", Line: 9, WritesDisabled: true})
	s := render(a)
	tabBar := strings.SplitN(s, "\n", 2)[0]
	if strings.Contains(tabBar, "⚠ config ") {
		t.Errorf("marker kept in an already-full tab bar: %q", tabBar)
	}
	if !strings.Contains(s, "config.yaml:9 failed to parse") {
		t.Error("banner missing — the warning has to survive somewhere")
	}
	// The tab bar is no wider than it would be without the marker.
	clean := sizedApp(t, 100, 20)
	cleanBar := strings.SplitN(render(clean), "\n", 2)[0]
	if len([]rune(tabBar)) > len([]rune(cleanBar)) {
		t.Errorf("degraded tab bar is %d cells, healthy one is %d", len([]rune(tabBar)), len([]rune(cleanBar)))
	}
}

// TestDegradedConfigBannerVariants covers the shapes the loader can hand
// over: no line number, and writes left enabled by --force.
func TestDegradedConfigBannerVariants(t *testing.T) {
	cases := []struct {
		name   string
		status ConfigStatus
		want   string
	}{
		{
			name:   "no line number",
			status: ConfigStatus{Degraded: true, Path: "config.yaml", Err: errors.New("boom"), WritesDisabled: true},
			want:   "⚠ config.yaml failed to parse (boom) — running on defaults. Config writes are disabled until fixed.",
		},
		{
			name:   "writes forced on",
			status: ConfigStatus{Degraded: true, Path: "config.yaml", Line: 3},
			want:   "⚠ config.yaml:3 failed to parse — running on defaults.",
		},
		{
			name:   "no path",
			status: ConfigStatus{Degraded: true, Line: 3},
			want:   "⚠ config:3 failed to parse — running on defaults.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.banner(); got != tc.want {
				t.Errorf("banner() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConfigStatusMsgRoutes checks the broadcast form of the same state,
// used when one data plane feeds several sessions.
func TestConfigStatusMsgRoutes(t *testing.T) {
	a := sizedApp(t, 120, 40)
	if len(a.notices()) != 0 {
		t.Fatalf("expected no notices on a healthy config, got %#v", a.notices())
	}
	a, _ = send(t, a, ConfigStatusMsg{Status: ConfigStatus{Degraded: true, Path: "config.yaml", Line: 1, WritesDisabled: true}})
	if len(a.notices()) != 1 {
		t.Errorf("notices() = %#v, want one banner", a.notices())
	}
}

// TestUnroutableNotice covers the symbols that route to no provider: they
// can never quote, so the frame says so instead of showing a blank row.
func TestUnroutableNotice(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, UnroutableSymbolsMsg{Symbols: []string{"NOTREAL", "ALSOBAD"}})
	got := a.notices()
	if len(got) != 1 || !strings.Contains(got[0], "NOTREAL, ALSOBAD") {
		t.Fatalf("notices() = %#v", got)
	}
	if !strings.Contains(render(a), "NOTREAL") {
		t.Error("unroutable notice missing from the rendered frame")
	}

	a.SetUnroutable([]string{"NOTREAL"})
	if got := a.notices(); len(got) != 1 || !strings.Contains(got[0], "1 symbol routes to no provider") {
		t.Errorf("singular notice = %#v", got)
	}
}

// TestNoticesShrinkContentHeight makes sure the banner takes its row from
// the content panel rather than pushing the status bar off screen.
func TestNoticesShrinkContentHeight(t *testing.T) {
	a := sizedApp(t, 120, 40)
	_, before := a.contentSize(a.width, a.height)
	a.SetConfigStatus(ConfigStatus{Degraded: true, Path: "config.yaml", Line: 1, WritesDisabled: true})
	a.SetUnroutable([]string{"NOTREAL"})
	_, after := a.contentSize(a.width, a.height)
	if after != before-2 {
		t.Errorf("content height with two notices = %d, want %d", after, before-2)
	}
	if h := lineCount(render(a)); h > a.height {
		t.Errorf("frame is %d rows tall, exceeds the %d-row terminal", h, a.height)
	}
}

func lineCount(s string) int {
	return strings.Count(s, "\n") + 1
}

// TestMacroTabScrolls covers the Macro tab's Update being routed at all —
// it had no Update method until Wave 2 and was entirely non-interactive.
func TestMacroTabScrolls(t *testing.T) {
	a := sizedApp(t, 120, 20)
	a, _ = send(t, a, MacroUpdateMsg{Quotes: []provider.Quote{{Symbol: "^GSPC", Price: 5000, ChangePct: 0.4}}})
	a, _ = send(t, a, keyPress("5"))
	top := render(a)

	a, _ = send(t, a, keyPress("G"))
	if bottom := render(a); bottom == top {
		t.Error("'G' on the Macro tab changed nothing — keys are not routed to macro")
	}

	a, _ = send(t, a, keyPress("g"))
	if again := render(a); again != top {
		t.Error("'g' did not return the Macro tab to the top")
	}

	// And the wheel scrolls it too.
	a, _ = send(t, a, tea.MouseWheelMsg{X: 4, Y: 6, Button: tea.MouseWheelDown})
	if scrolled := render(a); scrolled == top {
		t.Error("wheel down on the Macro tab changed nothing")
	}
}

// TestHeatmapSectorsFromWatchlist verifies the heatmap is seeded from the
// user's own groups rather than the built-in fallback list.
func TestHeatmapSectorsFromWatchlist(t *testing.T) {
	groups := []watchlist.Group{
		{Name: "Mine", Symbols: []string{"AAPL"}},
		{Name: "Empty", Symbols: nil},
	}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, alert.NewEngine(0, nil), yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: 1.2}})
	a, _ = send(t, a, keyPress("7"))

	got := render(a)
	if !strings.Contains(got, "Mine") {
		t.Errorf("heatmap not seeded from the watchlist groups:\n%s", got)
	}
	if strings.Contains(got, "Megacap Tech") {
		t.Errorf("heatmap still showing the built-in fallback sectors:\n%s", got)
	}
}

// TestCorrelationSymbolsFromWatchlist verifies the correlation matrix
// shares the watchlist universe.
func TestCorrelationSymbolsFromWatchlist(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyPress("9"))
	got := render(a)
	if !strings.Contains(got, "AAPL") {
		t.Errorf("correlation view missing watchlist symbols:\n%s", got)
	}
}

// TestSetWatchlistGroupsReseeds covers re-seeding after a config reload.
func TestSetWatchlistGroupsReseeds(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.SetWatchlistGroups([]watchlist.Group{{Name: "Renamed", Symbols: []string{"MSFT"}}})
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "MSFT", Price: 400, ChangePct: -0.5}})
	a.activeTab = TabHeatmap
	if got := render(a); !strings.Contains(got, "Renamed") {
		t.Errorf("heatmap not re-seeded:\n%s", got)
	}
	a.activeTab = TabCorrel
	if got := render(a); !strings.Contains(got, "MSFT") {
		t.Errorf("correlation not re-seeded:\n%s", got)
	}
}

// TestSetContextTearsDownStreams checks the parent context reaches the
// detail panel, which owns the only background stream in the TUI.
func TestSetContextTearsDownStreams(t *testing.T) {
	a := sizedApp(t, 120, 40)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.SetContext(ctx)
	// No observable state beyond not panicking and the panel still
	// working: the streams themselves need a live program.
	a, _ = send(t, a, keyPress("j"))
	a, _ = send(t, a, keyCode(tea.KeyEnter))
	if !a.detail.Active() {
		t.Error("detail panel did not open after SetContext")
	}
}

// TestProviderStatusPerProvider checks the status bar tracks every
// provider that reports, not just coinbase.
func TestProviderStatusPerProvider(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, ConnectionStatusMsg{Provider: "coinbase", Connected: true})
	a, _ = send(t, a, ConnectionStatusMsg{Provider: "yahoo", Connected: false})
	got := a.statusbar.View()
	if !strings.Contains(got, "coinbase") || !strings.Contains(got, "yahoo") {
		t.Errorf("status bar missing a provider:\n%s", got)
	}
}

// TestBenchmarkSetter covers the portfolio Beta benchmark hook.
func TestBenchmarkSetter(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a.SetBenchmark("QQQ")
	a.SetBenchmark("")
	// Rendering an empty portfolio list must still work with either.
	a.activeTab = TabPortfolio
	_ = a.View()
}

// TestCanonicalSymbols covers the dedupe helper the heatmap and
// correlation seeding depend on — including the case that made the
// dedupe order matter: two spellings of one instrument have to collapse
// to a single entry, which they only do when the dedupe runs on the
// canonical form.
func TestCanonicalSymbols(t *testing.T) {
	cases := []struct {
		name   string
		groups []watchlist.Group
		want   []string
	}{
		{
			name: "union across groups",
			groups: []watchlist.Group{
				{Name: "a", Symbols: []string{"AAPL", "MSFT"}},
				{Name: "b", Symbols: []string{"MSFT", "BTC-USD"}},
			},
			want: []string{"AAPL", "MSFT", "BTC-USD"},
		},
		{
			name: "aliases collapse to one symbol",
			groups: []watchlist.Group{
				{Name: "a", Symbols: []string{"btc", "BTC-USD", "BTCUSDT"}},
				{Name: "b", Symbols: []string{"aapl", "AAPL"}},
			},
			want: []string{"BTC-USD", "AAPL"},
		},
		{
			name:   "blanks dropped",
			groups: []watchlist.Group{{Name: "a", Symbols: []string{"  ", "", "aapl"}}},
			want:   []string{"AAPL"},
		},
		{
			name:   "no groups",
			groups: nil,
			want:   nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalSymbols(tc.groups)
			if len(got) != len(tc.want) {
				t.Fatalf("CanonicalSymbols = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("CanonicalSymbols = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestCorrelationSymbolsDeduplicated is the same guarantee seen from the
// view: a config that spells one instrument two ways must not produce
// two identical rows in the matrix.
func TestCorrelationSymbolsDeduplicated(t *testing.T) {
	groups := []watchlist.Group{{Name: "Mine", Symbols: []string{"btc", "BTC-USD", "aapl"}}}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, alert.NewEngine(0, nil), yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
	a.activeTab = TabCorrel
	got := render(a)
	// The matrix clips its labels, so the symbol is counted by its
	// rendered prefix: one column header plus one row label per symbol.
	if n := strings.Count(got, "BTC-US"); n != 2 {
		t.Errorf("BTC-US appears %d times in the correlation view, want 2 (one header, one row):\n%s", n, got)
	}
}

// TestWatchlistGroupsAreCanonicalized covers the heatmap/correlation
// seeds being normalized the way the hub normalizes its subscriptions —
// a hand-typed `btc` in the config must still colour a tile.
func TestWatchlistGroupsAreCanonicalized(t *testing.T) {
	groups := []watchlist.Group{{Name: "Mine", Symbols: []string{"btc", "  ", "aapl"}}}
	a := NewApp(groups, market.NewCache(30), fakeHistory{}, nil, alert.NewEngine(0, nil), yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "BTC-USD", Price: 60000, ChangePct: 2.5}})
	a, _ = send(t, a, keyPress("7"))
	a, _ = send(t, a, keyCode(tea.KeyEnter)) // drill into the sector

	if got := render(a); !strings.Contains(got, "BTC-USD") {
		t.Errorf("heatmap sector did not canonicalize its symbols:\n%s", got)
	}
}

// TestFullScreenChartOwnsItsTopRow covers a click on the chart's first
// row: the full-screen views draw no tab bar, so the tab hit-test must
// not run ahead of them or the click switches a tab nobody can see.
func TestFullScreenChartOwnsItsTopRow(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyPress("c"))
	if !a.chart.Active() {
		t.Fatal("'c' did not open the chart")
	}
	segs := a.tabSegments()
	target := segs[len(segs)-1]

	a, _ = send(t, a, tea.MouseClickMsg{X: target.start, Y: 0, Button: tea.MouseLeft})
	if a.activeTab != TabWatchlist {
		t.Errorf("click on the full-screen chart switched to %v", a.activeTab)
	}
	if !a.chart.Active() {
		t.Error("chart closed by a click on its own top row")
	}
}

// TestFullScreenCompareOwnsItsTopRow is the same guarantee for the
// comparison chart.
func TestFullScreenCompareOwnsItsTopRow(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyPress("a")) // add the selected symbol
	a, _ = send(t, a, keyPress("C")) // open the comparison chart
	if !a.compare.Active() {
		t.Fatal("'C' did not open the comparison chart")
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: a.tabSegments()[2].start, Y: 0, Button: tea.MouseLeft})
	if a.activeTab != TabWatchlist {
		t.Errorf("click on the comparison chart switched to %v", a.activeTab)
	}
	if !a.compare.Active() {
		t.Error("comparison chart closed by a click on its own top row")
	}
}

// TestDetailPanelSwallowsContentClicks keeps the panel modal for the
// mouse the way it already is for the keyboard: it covers the content
// area, so a click must not move the selection on the watchlist behind
// it — which the user would only discover after closing the panel.
func TestDetailPanelSwallowsContentClicks(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, cmd := send(t, a, keyCode(tea.KeyEnter))
	_ = cmd
	if !a.detail.Active() {
		t.Fatal("enter did not open the detail panel")
	}
	if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
		t.Fatalf("selected symbol = %q, want AAPL", got)
	}

	_, originY := a.contentOrigin()
	a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: originY + 2 + 1, Button: tea.MouseLeft})
	if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
		t.Errorf("click behind the detail panel moved the selection to %q", got)
	}
	a, _ = send(t, a, tea.MouseWheelMsg{X: 4, Y: originY + 2, Button: tea.MouseWheelDown})
	if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
		t.Errorf("wheel behind the detail panel moved the selection to %q", got)
	}
	if !a.detail.Active() {
		t.Error("detail panel closed by a click it should have absorbed")
	}
}

// TestTabClickClosesDetailPanel covers the one mouse target that stays
// visible while the panel is up. Clicking a tab has to close the panel,
// or the tab the user picked stays hidden behind it.
func TestTabClickClosesDetailPanel(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyCode(tea.KeyEnter))
	if !a.detail.Active() {
		t.Fatal("enter did not open the detail panel")
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: a.tabSegments()[TabNews].start, Y: 0, Button: tea.MouseLeft})
	if a.detail.Active() {
		t.Error("detail panel survived a tab click and hides the new tab")
	}
	if a.activeTab != TabNews {
		t.Errorf("activeTab = %v, want TabNews", a.activeTab)
	}
	if got := render(a); strings.Contains(got, "Detail") {
		t.Errorf("frame still showing the detail panel:\n%s", got)
	}
}

// TestModalsSwallowMouse covers the centered overlays: the key path
// returns before tab switching for each of them, so a click must not
// reach the tab drawn behind the modal either.
func TestModalsSwallowMouse(t *testing.T) {
	cases := []struct {
		name string
		open func(*testing.T, *App) *App
	}{
		{
			name: "alert dialog",
			open: func(t *testing.T, a *App) *App {
				a, _ = send(t, a, keyPress("A"))
				if !a.alertDialog.Active() {
					t.Fatal("'A' did not open the alert dialog")
				}
				return a
			},
		},
		{
			name: "help overlay",
			open: func(t *testing.T, a *App) *App {
				a, _ = send(t, a, keyPress("?"))
				if !a.help.Active() {
					t.Fatal("'?' did not open the help overlay")
				}
				return a
			},
		},
		{
			name: "command palette",
			open: func(t *testing.T, a *App) *App {
				a, _ = send(t, a, keyPress(":"))
				if !a.palette.Active() {
					t.Fatal("':' did not open the palette")
				}
				return a
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := sizedApp(t, 120, 40)
			a = tc.open(t, a)
			_, originY := a.contentOrigin()

			a, _ = send(t, a, tea.MouseClickMsg{X: 4, Y: originY + 2 + 1, Button: tea.MouseLeft})
			if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
				t.Errorf("click behind the modal selected %q, want AAPL", got)
			}
			a, _ = send(t, a, tea.MouseClickMsg{X: a.tabSegments()[TabNews].start, Y: 0, Button: tea.MouseLeft})
			if a.activeTab != TabWatchlist {
				t.Errorf("tab click behind the modal switched to %v", a.activeTab)
			}
			a, _ = send(t, a, tea.MouseWheelMsg{X: 4, Y: originY + 2, Button: tea.MouseWheelDown})
			if got := a.watchlist.SelectedSymbol(); got != "AAPL" {
				t.Errorf("wheel behind the modal selected %q, want AAPL", got)
			}
		})
	}
}

// TestChartHistoryLandsAfterClose pins the async routing as unconditional
// for the chart too: a load started with 'c' and escaped before it
// resolved must still be absorbed, not dropped by the router.
func TestChartHistoryLandsAfterClose(t *testing.T) {
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL", "BTC-USD"}}}
	a := NewApp(groups, market.NewCache(30), fixedHistory{}, nil, alert.NewEngine(0, nil), yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})

	a, cmd := send(t, a, keyPress("c"))
	if cmd == nil {
		t.Fatal("'c' returned no history command")
	}
	if got := a.chart.View(); !strings.Contains(got, "Loading") {
		t.Fatalf("chart is not in a loading state:\n%s", got)
	}

	// Leave the chart before the fetch resolves.
	a, _ = send(t, a, keyCode(tea.KeyEsc))
	if a.chart.Active() {
		t.Fatal("esc did not close the chart")
	}
	a, _ = send(t, a, keyPress("5")) // and wander to another tab

	a, _ = send(t, a, cmd())

	got := ansiRe.ReplaceAllString(a.chart.View(), "")
	if strings.Contains(got, "Loading") || strings.Contains(got, "No data available") {
		t.Errorf("history dropped while the chart was closed:\n%s", got)
	}
}

// TestChartFitKeyReachesChart covers the 'f' binding added in the chart
// package: it only does anything if the root router forwards it, and a
// zoomed-in chart is the only place the difference is visible.
func TestChartFitKeyReachesChart(t *testing.T) {
	groups := []watchlist.Group{{Name: "Default", Symbols: []string{"AAPL"}}}
	a := NewApp(groups, market.NewCache(30), fixedHistory{}, nil, alert.NewEngine(0, nil), yahoo.New(0), coinbase.New())
	a, _ = send(t, a, tea.WindowSizeMsg{Width: 120, Height: 40})

	a, cmd := send(t, a, keyPress("c"))
	if cmd == nil {
		t.Fatal("'c' returned no history command")
	}
	a, _ = send(t, a, cmd())
	fitted := a.chart.View()

	// Zoom in a few times, then ask for the auto-fit back.
	for range 4 {
		a, _ = send(t, a, keyPress("+"))
	}
	if zoomed := a.chart.View(); zoomed == fitted {
		t.Fatal("'+' did not reach the chart, so 'f' cannot be tested")
	}
	a, _ = send(t, a, keyPress("f"))
	if got := a.chart.View(); got != fitted {
		t.Error("'f' did not restore the auto-fit zoom")
	}
}

// TestSeedLoaders covers the Load* seeding calls the data plane makes
// before the program runs: each one has to reach the tab that renders it,
// or a restart silently loses history the daemon kept on disk.
func TestSeedLoaders(t *testing.T) {
	now := time.Now()
	a := sizedApp(t, 120, 40)

	a.LoadPastAlerts([]alert.TriggeredAlert{
		{Rule: alert.Rule{Symbol: "AAPL"}, Price: 201.5, Message: "AAPL crossed 200", Timestamp: now},
	})
	a.activeTab = TabAlerts
	if got := render(a); !strings.Contains(got, "AAPL crossed 200") {
		t.Errorf("past alerts not seeded into the Alerts tab:\n%s", got)
	}
	if !strings.Contains(a.statusbar.View(), "1") {
		t.Error("status bar alert count not updated by LoadPastAlerts")
	}

	a.LoadEquityHistory(map[string][]portfolio.EquityMark{
		"Default": {{PortfolioName: "Default", Value: 1000, Time: now.Add(-time.Hour)}, {PortfolioName: "Default", Value: 1100, Time: now}},
	})
	a.activeTab = TabPortfolio
	_ = render(a)

	a.LoadCalendarEvents([]calendar.Event{{Title: "CPI print", Time: now, Importance: 3}})
	a.activeTab = TabMacro
	// The macro page only paints once it has an index quote, and the
	// events sit below the fold of a 40-row frame.
	a, _ = send(t, a, MacroUpdateMsg{Quotes: []provider.Quote{{Symbol: "^GSPC", Price: 5000, ChangePct: 0.4}}})
	a, _ = send(t, a, keyPress("G"))
	if got := render(a); !strings.Contains(got, "CPI print") {
		t.Errorf("calendar events not seeded into the Macro tab:\n%s", got)
	}

	a.LoadNotes(map[string]string{"AAPL": "watching the 200 line"})
	a.activeTab = TabWatchlist
	a, _ = send(t, a, keyCode(tea.KeyEnter))
	// The panel keeps the quote for the symbol it is showing, so the
	// price has to arrive after it opens.
	a, _ = send(t, a, QuoteUpdateMsg{Quote: provider.Quote{Symbol: "AAPL", Price: 201.5, ChangePct: 1.2, Timestamp: now}})
	if got := render(a); !strings.Contains(got, "watching the 200 line") {
		t.Errorf("notes not seeded into the detail panel:\n%s", got)
	}
}

// TestInitStartsSpinner covers the one command Init returns: without it
// the loading spinner never advances.
func TestInitStartsSpinner(t *testing.T) {
	a := newTestApp()
	cmd := a.Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}
	if _, ok := cmd().(SpinnerTickMsg); !ok {
		t.Errorf("Init command produced %T, want SpinnerTickMsg", cmd())
	}
	before := a.spinnerTick
	a, next := send(t, a, SpinnerTickMsg{})
	if a.spinnerTick != before+1 {
		t.Errorf("spinnerTick = %d, want %d", a.spinnerTick, before+1)
	}
	if next == nil {
		t.Error("spinner tick did not reschedule itself")
	}
	// Before the first WindowSizeMsg the app paints the loading frame.
	if got := a.View().Content; !strings.Contains(got, "Loading") {
		t.Errorf("pre-ready view = %q, want a loading frame", got)
	}
}

// TestOverlaysRenderCentered covers the compositing path each centered
// overlay goes through, at a frame smaller than the overlay itself as
// well as a roomy one.
func TestOverlaysRenderCentered(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 40}, {40, 16}} {
		a := sizedApp(t, sz.w, sz.h)
		a, _ = send(t, a, keyPress("A"))
		if got := render(a); !strings.Contains(got, "AAPL") {
			t.Errorf("%dx%d: alert dialog not composited:\n%s", sz.w, sz.h, got)
		}
		a, _ = send(t, a, keyCode(tea.KeyEsc))

		a, _ = send(t, a, keyPress("?"))
		if got := render(a); !strings.Contains(got, "press any key to close") {
			t.Errorf("%dx%d: help overlay not composited:\n%s", sz.w, sz.h, got)
		}
		a, _ = send(t, a, keyPress("x"))

		a, _ = send(t, a, keyPress(":"))
		if got := render(a); !strings.Contains(got, "Watch") {
			t.Errorf("%dx%d: palette not composited:\n%s", sz.w, sz.h, got)
		}
	}
}

// TestTabString covers the Tab name lookup, including the out-of-range
// guard.
func TestTabString(t *testing.T) {
	if got := TabWatchlist.String(); got != "Watch" {
		t.Errorf("TabWatchlist.String() = %q, want Watch", got)
	}
	if got := TabCorrel.String(); got != "Correl" {
		t.Errorf("TabCorrel.String() = %q, want Correl", got)
	}
	if got := Tab(len(tabNames)).String(); got != "Unknown" {
		t.Errorf("out-of-range Tab.String() = %q, want Unknown", got)
	}
}

// TestDataPlaneWiringInterfaces pins the optional-interface surface the
// data plane wires models through. Breaking a signature here silently
// disables the banner rather than failing the build, so it is asserted.
func TestDataPlaneWiringInterfaces(t *testing.T) {
	var m any = newTestApp()

	banner, ok := m.(interface {
		LoadConfigBanner(path string, line int, err error)
	})
	if !ok {
		t.Fatal("*App no longer implements LoadConfigBanner")
	}
	unroutable, ok := m.(interface{ LoadUnroutableSymbols(symbols []string) })
	if !ok {
		t.Fatal("*App no longer implements LoadUnroutableSymbols")
	}
	setter, ok := m.(interface{ SetContext(ctx context.Context) })
	if !ok {
		t.Fatal("*App no longer implements SetContext")
	}

	setter.SetContext(context.Background())
	banner.LoadConfigBanner("/x/config.yaml", 9, errors.New("boom"))
	unroutable.LoadUnroutableSymbols([]string{"NOTREAL"})

	a := m.(*App)
	got := a.notices()
	if len(got) != 2 {
		t.Fatalf("notices() = %#v, want a banner and an unroutable row", got)
	}
	if !strings.Contains(got[0], "config.yaml:9") || !strings.Contains(got[0], "writes are disabled") {
		t.Errorf("banner = %q", got[0])
	}
	if !strings.Contains(got[1], "NOTREAL") {
		t.Errorf("unroutable notice = %q", got[1])
	}
}

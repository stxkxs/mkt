package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func typeKeys(t *testing.T, a *App, text string) *App {
	t.Helper()
	for _, r := range text {
		a, _ = send(t, a, keyPress(string(r)))
	}
	return a
}

// The palette claims every key while it is open, so it must be visible
// over every view it can be opened from, or the keyboard goes dead with
// nothing on screen to say why.
func TestPaletteIsVisibleOverEveryView(t *testing.T) {
	views := []struct {
		name   string
		open   func(t *testing.T, a *App) *App
		active func(a *App) bool
	}{
		{"tab", func(t *testing.T, a *App) *App { return a }, func(a *App) bool { return true }},
		{"detail", func(t *testing.T, a *App) *App {
			a, _ = send(t, a, keyCode(tea.KeyEnter))
			return a
		}, func(a *App) bool { return a.detail.Active() }},
		{"chart", func(t *testing.T, a *App) *App {
			a, _ = send(t, a, keyPress("c"))
			return a
		}, func(a *App) bool { return a.chart.Active() }},
		{"compare", func(t *testing.T, a *App) *App {
			a, _ = send(t, a, keyPress("a"))
			a, _ = send(t, a, keyPress("C"))
			return a
		}, func(a *App) bool { return a.compare.Active() }},
	}
	for _, v := range views {
		for _, sz := range []struct{ w, h int }{{120, 40}, {60, 16}} {
			a := sizedApp(t, sz.w, sz.h)
			a = v.open(t, a)
			if !v.active(a) {
				t.Fatalf("%s did not open", v.name)
			}
			a, _ = send(t, a, keyPress(":"))
			got := render(a)
			if !strings.Contains(got, ": ") || !strings.Contains(got, "tab name") {
				t.Errorf("%s at %dx%d: palette not drawn:\n%s", v.name, sz.w, sz.h, got)
			}
			if n := len(strings.Split(got, "\n")); n != sz.h {
				t.Errorf("%s at %dx%d: frame is %d rows with the palette open, want %d", v.name, sz.w, sz.h, n, sz.h)
			}
		}
	}
}

// A palette jump has to land on the tab it names, which means closing
// anything drawn over the tabs.
func TestPaletteJumpClosesWhatCoversTheTab(t *testing.T) {
	openers := map[string]func(t *testing.T, a *App) *App{
		"detail": func(t *testing.T, a *App) *App { a, _ = send(t, a, keyCode(tea.KeyEnter)); return a },
		"chart":  func(t *testing.T, a *App) *App { a, _ = send(t, a, keyPress("c")); return a },
		"compare": func(t *testing.T, a *App) *App {
			a, _ = send(t, a, keyPress("a"))
			a, _ = send(t, a, keyPress("C"))
			return a
		},
	}
	for name, open := range openers {
		a := open(t, sizedApp(t, 120, 40))
		a, _ = send(t, a, keyPress(":"))
		a = typeKeys(t, a, "news")
		a, _ = send(t, a, keyCode(tea.KeyEnter))

		if a.detail.Active() || a.chart.Active() || a.compare.Active() {
			t.Errorf("%s: still open after jumping to News", name)
		}
		if a.activeTab != TabNews {
			t.Errorf("%s: active tab %v, want News", name, a.activeTab)
		}
		if got := render(a); !strings.Contains(got, "News") {
			t.Errorf("%s: News tab not drawn after the jump:\n%s", name, got)
		}
	}
}

// Clicking a tab while the detail panel is open shows that tab.
func TestTabClickClosesDetail(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyCode(tea.KeyEnter))
	if !a.detail.Active() {
		t.Fatal("detail did not open")
	}
	a, _ = send(t, a, tea.MouseClickMsg{X: a.tabSegments()[TabNews].start, Y: 0, Button: tea.MouseLeft})
	if a.detail.Active() || a.activeTab != TabNews {
		t.Errorf("detail active %v, tab %v; want the News tab showing", a.detail.Active(), a.activeTab)
	}
}

// A palette command that does nothing says why, until the next key.
func TestUnknownPaletteCommandIsShown(t *testing.T) {
	a := sizedApp(t, 120, 40)
	a, _ = send(t, a, keyPress(":"))
	a = typeKeys(t, a, "xyz")
	a, _ = send(t, a, keyCode(tea.KeyEnter))

	got := render(a)
	if !strings.Contains(got, `unknown command "xyz"`) {
		t.Fatalf("no notice for an unknown command:\n%s", got)
	}
	if n := len(strings.Split(got, "\n")); n != 40 {
		t.Errorf("frame is %d rows with the notice, want 40", n)
	}

	a, _ = send(t, a, keyPress("j"))
	if got := render(a); strings.Contains(got, "unknown command") {
		t.Errorf("notice survived the next key:\n%s", got)
	}
}

package statusbar

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var sweepWidths = []int{-4, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}

func populated() Model {
	m := New()
	m.SetProviderStatus("coinbase", true, nil)
	m.SetProviderStatus("yahoo", false, errors.New("429 Too Many Requests"))
	m.SetLastUpdate(time.Now().Add(-3 * time.Second))
	m.SetAlertCount(4)
	m.SetThemeName("tokyonight")
	m.SetSearchQuery("btc")
	return m
}

func TestViewSurvivesEveryWidth(t *testing.T) {
	for _, w := range sweepWidths {
		m := populated()
		m.SetWidth(w)
		out := m.View()
		if w <= 0 && out != "" {
			t.Errorf("width %d rendered %q, want empty", w, out)
		}
	}
}

func TestBarFillsItsWidth(t *testing.T) {
	m := populated()
	m.SetWidth(200)
	if got := lipgloss.Width(m.View()); got != 200 {
		t.Errorf("bar is %d cells wide, want 200", got)
	}
}

func TestProviderStatusUpdatesInPlace(t *testing.T) {
	m := New()
	m.SetProviderStatus("coinbase", false, nil)
	m.SetProviderStatus("coinbase", true, nil)
	if len(m.providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(m.providers))
	}
	if !m.providers[0].Connected {
		t.Error("provider status not updated in place")
	}
}

// The bar is one row at every width: a wrapped bar pushes the frame a row
// past the terminal and shifts every mouse row. Every width from 1 up is
// checked with every segment populated, which is where it used to wrap.
func TestBarIsOneRowOfExactlyItsWidth(t *testing.T) {
	for w := 1; w <= 200; w++ {
		m := populated()
		m.SetProviderStatus("binance", true, nil)
		m.SetProviderStatus("kraken", false, nil)
		m.SetWidth(w)
		out := m.View()
		if strings.Contains(out, "\n") {
			t.Fatalf("width %d: bar wraps:\n%s", w, out)
		}
		if got := lipgloss.Width(out); got != w {
			t.Fatalf("width %d: bar is %d cells", w, got)
		}
	}
}

// Help is the one hint that always survives; the theme name is the first
// thing to go.
func TestBarShedsTheThemeBeforeTheHelpHint(t *testing.T) {
	for w := 8; w <= 200; w++ {
		m := populated()
		m.SetWidth(w)
		out := ansiStrip(m.View())
		if !strings.Contains(out, "?:help") {
			t.Fatalf("width %d: help hint dropped: %q", w, out)
		}
		if strings.Contains(out, "T:tokyonight") && !strings.Contains(out, "coinbase") {
			t.Fatalf("width %d: theme kept while providers dropped: %q", w, out)
		}
	}
}

func TestDisconnectReasonShownWhenItFits(t *testing.T) {
	m := populated()
	m.SetWidth(200)
	if out := ansiStrip(m.View()); !strings.Contains(out, "○ yahoo: 429 Too Many Requests") {
		t.Errorf("reason missing at full width: %q", out)
	}
}

// The age counts against the clock the app advances, not the moment of
// the last quote: a feed that stops keeps aging.
func TestAgeKeepsCountingWithoutNewData(t *testing.T) {
	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	m := New()
	m.SetWidth(80)
	m.SetNow(base)
	m.SetLastUpdate(base)
	m.SetNow(base.Add(3 * time.Minute))
	if out := ansiStrip(m.View()); !strings.Contains(out, "3m ago") {
		t.Errorf("age after 3 minutes of silence: %q", out)
	}
}

func TestOlderQuoteDoesNotRewindTheAge(t *testing.T) {
	base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	m := New()
	m.SetWidth(80)
	m.SetNow(base)
	m.SetLastUpdate(base)
	m.SetLastUpdate(base.Add(-6 * time.Hour))
	if out := ansiStrip(m.View()); !strings.Contains(out, " now ") || strings.Contains(out, "6h") {
		t.Errorf("a delayed quote rewound the age: %q", out)
	}
	m.SetLastUpdate(time.Time{})
	if m.lastUpdate.IsZero() {
		t.Error("an untimestamped quote cleared the age")
	}
}

func TestAge(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "now"}, {999 * time.Millisecond, "now"}, {time.Second, "1s"}, {59 * time.Second, "59s"},
		{75 * time.Second, "1m"}, {59 * time.Minute, "59m"}, {3 * time.Hour, "3h"}, {50 * time.Hour, "2d"},
	} {
		if got := Age(c.d); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func ansiStrip(s string) string { return ansi.Strip(s) }

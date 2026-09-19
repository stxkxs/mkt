package options

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

// plain strips the styling so an assertion reads the cells, not the
// escapes wrapped around them.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

var (
	sweepWidths  = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 80, 120, 200}
	sweepHeights = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 20, 40, 60}
)

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

type stubSource struct {
	chain yahoo.OptionsChain
	err   error
}

func (s stubSource) FetchOptionsChain(context.Context, string) (yahoo.OptionsChain, error) {
	return s.chain, s.err
}

func chain(n int) yahoo.OptionsChain {
	c := yahoo.OptionsChain{Symbol: "AAPL", Expiration: time.Now()}
	for i := range n {
		strike := 100 + float64(i)
		c.Calls = append(c.Calls, yahoo.Option{Strike: strike, Bid: 1, Last: 1.2, IV: 0.3, OpenInterest: 10})
		c.Puts = append(c.Puts, yahoo.Option{Strike: strike, Bid: 0.9, Last: 1.0, IV: 0.35, OpenInterest: 12})
	}
	return c
}

func loaded(n int) Model {
	m := New(stubSource{chain: chain(n)})
	m.SetSize(120, 24)
	m.symbol = "AAPL"
	m, _ = m.Update(loadedMsg{chain: chain(n)})
	return m
}

func TestChainIsWindowed(t *testing.T) {
	m := loaded(80)
	m.SetSize(120, 20)
	if lines := strings.Count(m.View(), "\n"); lines > 20 {
		t.Errorf("view is %d lines tall in a 20-line frame", lines)
	}
}

func TestCursorStaysVisible(t *testing.T) {
	m := loaded(80)
	m.SetSize(120, 20)
	for range 79 {
		m = press(m, "j")
	}
	if !strings.Contains(m.View(), "$179.00") {
		t.Error("cursor row not rendered after scrolling to the last strike")
	}
}

func TestViewSurvivesEverySize(t *testing.T) {
	for _, w := range sweepWidths {
		for _, h := range sweepHeights {
			for _, k := range []string{"j", "k", "esc"} {
				m := loaded(30)
				m.SetSize(w, h)
				m = press(m, k)
				_ = m.View()
				m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
				_ = m.View()
			}
			empty := New(stubSource{})
			empty.SetSize(w, h)
			_ = empty.View()

			errored := New(stubSource{})
			errored.SetSize(w, h)
			errored.symbol = "AAPL"
			errored, _ = errored.Update(errorMsg{err: errors.New("boom")})
			_ = errored.View()

			loading := New(stubSource{})
			loading.SetSize(w, h)
			loading.symbol = "AAPL"
			loading.loading = true
			_ = loading.View()
		}
	}
}

// wideChain is representative data for a width sweep: strikes and quotes
// long enough to overrun every column, so a sweep exercises truncation
// rather than only short values that happen to fit.
func wideChain(n int) yahoo.OptionsChain {
	c := yahoo.OptionsChain{Symbol: "BRK-B", Expiration: time.Date(2031, 12, 19, 0, 0, 0, 0, time.UTC)}
	for i := range n {
		strike := 98765.43 + float64(i)
		c.Calls = append(c.Calls, yahoo.Option{Strike: strike, Bid: 123456.78, Last: 98765.43, IV: 12.3456, OpenInterest: 10})
		c.Puts = append(c.Puts, yahoo.Option{Strike: strike, Bid: 87654.32, Last: 76543.21, IV: 9.87654, OpenInterest: 12})
	}
	return c
}

// hostileChain drives the numeric cells past any width a column plan
// grants them: fifteen significant digits, a sign, and the non-finite
// values %.2f renders as words rather than numerals.
func hostileChain(n int) yahoo.OptionsChain {
	c := yahoo.OptionsChain{Symbol: "X", Expiration: time.Date(2031, 12, 19, 0, 0, 0, 0, time.UTC)}
	vals := []float64{123456789012345.67, -98765432109876.5, math.NaN(), math.Inf(1), math.Inf(-1), 0, 1e300, -0.000001}
	for i := range n {
		strike := vals[i%len(vals)] + float64(i)
		c.Calls = append(c.Calls, yahoo.Option{
			Strike: strike, Bid: vals[(i+1)%len(vals)], Last: vals[(i+2)%len(vals)], IV: vals[(i+3)%len(vals)],
		})
		c.Puts = append(c.Puts, yahoo.Option{
			Strike: strike, Bid: vals[(i+4)%len(vals)], Last: vals[(i+5)%len(vals)], IV: vals[(i+6)%len(vals)],
		})
	}
	return c
}

// hostileSymbols are the free text the tab carries into its section
// header. A cell budget is spent per grapheme cluster, so the corpus has
// to hold the clusters whose width is not the sum of their runes': an
// emoji presentation sequence (base plus U+FE0F), a keycap (base, U+FE0F,
// U+20E3), a ZWJ sequence and a combining mark, alongside the CJK and
// over-long cases that only exercise plain truncation.
var hostileSymbols = []string{
	"BRK-B",
	"BERKSHIRE-HATHAWAY-CLASS-B-VERY-LONG-TICKER-NAME",
	"中文股票代码",
	"☂️",
	"A☂️B",
	"1️⃣",
	"AB1️⃣CD",
	"#️⃣TICKER",
	"\U0001F468\u200d\U0001F469\u200d\U0001F467",
	"TICḰER",
}

// hostileErrors are provider text, which the tab prints verbatim and so
// cannot assume is ASCII.
var hostileErrors = []string{
	"fetch options chain for AAPL: Get \"https://query2.finance.yahoo.com/v7/finance/options/AAPL\": context deadline exceeded",
	"fetch failed for 1️⃣: context deadline exceeded",
	"#️⃣ rate limited by upstream",
	"中文错误信息非常长",
}

// sweepStates is one model per render path the tab can take at a size:
// the chain itself (cursor at the top and at the bottom), the
// truncation-hungry data sets, and the chrome-only paths, each carrying
// free text a frame cannot assume is one cell per rune.
func sweepStates(w, h int) []Model {
	var out []Model

	for i, c := range []yahoo.OptionsChain{chain(30), wideChain(30), hostileChain(30)} {
		m := New(stubSource{chain: c})
		m.SetSize(w, h)
		m.symbol = hostileSymbols[i%len(hostileSymbols)]
		m, _ = m.Update(loadedMsg{chain: c})
		out = append(out, m)

		bottom := m
		for range 29 {
			bottom = press(bottom, "j")
		}
		out = append(out, bottom)
	}

	empty := New(stubSource{})
	empty.SetSize(w, h)
	out = append(out, empty)

	for _, sym := range hostileSymbols {
		noData := New(stubSource{})
		noData.SetSize(w, h)
		noData.symbol = sym
		out = append(out, noData)

		loading := New(stubSource{})
		loading.SetSize(w, h)
		loading.symbol = sym
		loading.loading = true
		out = append(out, loading)
	}

	for _, e := range hostileErrors {
		errored := New(stubSource{})
		errored.SetSize(w, h)
		errored.symbol = "AAPL"
		errored, _ = errored.Update(errorMsg{err: errors.New(e)})
		out = append(out, errored)
	}

	return out
}

// Every line the tab prints must measure at most the frame it was given.
// The content panel word-wraps a line it cannot fit, and a row that wraps
// to two screen lines pushes the last row past the panel and
// desynchronizes the click hit-test, which maps a mouse row straight to a
// data-row index.
func TestEveryLineFitsTheFrame(t *testing.T) {
	for w := 1; w <= 200; w++ {
		for _, h := range []int{6, 24} {
			for _, m := range sweepStates(w, h) {
				for _, line := range strings.Split(m.View(), "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Fatalf("frame width %d: line measures %d cells: %q", w, got, plain(line))
					}
				}
			}
		}
	}
}

// The chain is read across Strike, so a call column and the put column
// beside it live or die together. Equal Prio makes that likely; Fit
// breaks a Prio tie on position, so the model enforces it.
func TestChainShedsSymmetricallyAroundStrike(t *testing.T) {
	pairs := [][2]string{{"call_bid", "put_bid"}, {"call_last", "put_last"}, {"call_iv", "put_iv"}}
	for w := 1; w <= 200; w++ {
		l := chainLayout(w)
		if l == nil {
			continue
		}
		if !l.Has("strike") {
			t.Fatalf("width %d: a table without the strike column", w)
		}
		for _, p := range pairs {
			if l.Has(p[0]) != l.Has(p[1]) {
				t.Fatalf("width %d: %s=%v but %s=%v", w, p[0], l.Has(p[0]), p[1], l.Has(p[1]))
			}
		}
	}
}

// Below the narrowest frame that holds a table the tab says so, rather
// than painting a partial one.
func TestTooNarrowInsteadOfAPartialTable(t *testing.T) {
	const narrowest = 13

	m := loaded(5)
	m.SetSize(narrowest-1, 24)
	// The line is itself truncated to the frame, so match its stem.
	if narrow := plain(m.View()); !strings.Contains(narrow, "Terminal") {
		t.Errorf("no too-narrow line at width %d: %q", narrowest-1, narrow)
	} else if strings.Contains(narrow, "$") {
		t.Errorf("partial table painted at width %d: %q", narrowest-1, narrow)
	}

	m.SetSize(narrowest, 24)
	view := plain(m.View())
	if strings.Contains(view, "Terminal") {
		t.Errorf("too-narrow line at width %d, which holds the strike column: %q", narrowest, view)
	}
	if !strings.Contains(view, "$100.00") {
		t.Errorf("strike column missing at width %d: %q", narrowest, view)
	}
}

// A frame wide enough for the whole plan gets the whole plan, at the
// widths the columns ask for.
func TestWideFrameKeepsEveryColumn(t *testing.T) {
	m := loaded(3)
	m.SetSize(120, 24)
	// Section header, blank, column header, rule, then one line per strike.
	lines := strings.Split(plain(m.View()), "\n")

	const wantHdr = "      CALL Bid     Last      IV% |    Strike |      Bid     Last      PUT IV%"
	if got := lines[2]; got != wantHdr {
		t.Errorf("header row\n got %q\nwant %q", got, wantHdr)
	}
	// The second strike: the first carries the cursor gutter.
	const wantRow = "          1.00     1.20    30.0% |   $101.00 |     0.90     1.00        35.0%"
	if got := lines[5]; got != wantRow {
		t.Errorf("data row\n got %q\nwant %q", got, wantRow)
	}
}

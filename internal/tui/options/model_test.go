package options

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider/yahoo"
)

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

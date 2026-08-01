package chart

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

func newCompareModel(symbols ...string) CompareModel {
	m := NewCompare(nil)
	m.SetSize(120, 40)
	for _, s := range symbols {
		m.AddSymbol(s)
	}
	return m
}

func TestCompareColorsAreDeterministic(t *testing.T) {
	// The legend colored by position in the symbol list while the plot
	// colored by position in the fetched-entry list, and the entries
	// arrived in whatever order the concurrent fetches finished. Both
	// now come from compareColorFor.
	symbols := []string{"BTC-USD", "ETH-USD", "SOL-USD"}
	palette := compareColorList()

	for i, sym := range symbols {
		if got, want := compareColorFor(symbols, sym), palette[i]; got != want {
			t.Fatalf("color for %s = %v, want %v", sym, got, want)
		}
	}
	if got := compareColorFor(symbols, "NOPE"); got != theme.ColorDim {
		t.Fatalf("unknown symbol color = %v, want dim", got)
	}

	// Whatever order the entries land in, each series keeps its color.
	for _, order := range [][]string{
		{"BTC-USD", "ETH-USD", "SOL-USD"},
		{"SOL-USD", "BTC-USD", "ETH-USD"},
		{"ETH-USD", "SOL-USD", "BTC-USD"},
	} {
		m := newCompareModel(symbols...)
		for _, sym := range order {
			m.entries = append(m.entries, CompareEntry{Symbol: sym, Data: genCandles(80)})
		}
		for _, s := range m.buildSeries(m.visibleCount()) {
			want := compareColorFor(symbols, s.symbol)
			if s.color != want {
				t.Fatalf("entry order %v: %s plotted %v, legend shows %v", order, s.symbol, s.color, want)
			}
		}
	}
}

func TestCompareEntriesKeepSymbolOrderRegardlessOfCompletionOrder(t *testing.T) {
	// The first symbol is answered last; the returned set must still be
	// in symbol order.
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		switch p.Symbol {
		case "AAA":
			time.Sleep(40 * time.Millisecond)
		case "BBB":
			time.Sleep(20 * time.Millisecond)
		}
		return genCandles(80), nil
	}}
	m := NewCompare(fp)
	m.SetSize(120, 40)
	for _, s := range []string{"AAA", "BBB", "CCC"} {
		m.AddSymbol(s)
	}

	cmd := m.Open()
	msg, ok := cmd().(compareLoadedMsg)
	if !ok {
		t.Fatal("Open did not produce a compareLoadedMsg")
	}
	got := make([]string, len(msg.entries))
	for i, e := range msg.entries {
		got[i] = e.Symbol
	}
	want := []string{"AAA", "BBB", "CCC"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entries = %v, want %v", got, want)
		}
	}
}

func TestCompareDropsFailedSymbolWithoutShiftingColors(t *testing.T) {
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		if p.Symbol == "AAA" {
			return nil, context.Canceled
		}
		return genCandles(80), nil
	}}
	m := NewCompare(fp)
	m.SetSize(120, 40)
	for _, s := range []string{"AAA", "BBB", "CCC"} {
		m.AddSymbol(s)
	}
	cmd := m.Open()
	m, _ = m.Update(cmd())

	if len(m.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(m.entries))
	}
	palette := compareColorList()
	for _, s := range m.buildSeries(m.visibleCount()) {
		var want = palette[1]
		if s.symbol == "CCC" {
			want = palette[2]
		}
		if s.color != want {
			t.Fatalf("%s took color %v, want %v — a failed fetch shifted the palette", s.symbol, s.color, want)
		}
	}
}

func TestCompareStaleBatchIsIgnored(t *testing.T) {
	fp := &fakeProvider{respond: func(p provider.HistoryParams) ([]provider.OHLCV, error) {
		return intervalMarker(p.Interval), nil
	}}
	m := NewCompare(fp)
	m.SetSize(120, 40)
	m.AddSymbol("AAA")

	cmd1 := m.Open() // 1d
	var cmd2 tea.Cmd
	m, cmd2 = m.Update(key("]")) // 1w

	m, _ = m.Update(cmd2())
	m, _ = m.Update(cmd1()) // stale

	if len(m.entries) == 0 {
		t.Fatal("stale batch cleared the loaded entries")
	}
	if got := markerInterval(m.entries[0].Data); got != provider.Interval1w {
		t.Fatalf("compare shows %s bars, want 1w", got)
	}
}

func TestCompareNormalizesOverACommonWindow(t *testing.T) {
	// A short series and a long one must be compared over the same
	// number of bars, or their percentages start from different dates.
	m := newCompareModel("AAA", "BBB")
	m.entries = []CompareEntry{
		{Symbol: "AAA", Data: genCandles(200)},
		{Symbol: "BBB", Data: genCandles(60)},
	}
	series := m.buildSeries(m.visibleCount())
	if len(series) != 2 {
		t.Fatalf("series = %d, want 2", len(series))
	}
	if len(series[0].pcts) != len(series[1].pcts) {
		t.Fatalf("series lengths differ: %d vs %d", len(series[0].pcts), len(series[1].pcts))
	}
	if len(series[0].pcts) > 60 {
		t.Fatalf("window %d longer than the shortest series", len(series[0].pcts))
	}
	for _, s := range series {
		if s.pcts[0] != 0 {
			t.Fatalf("%s does not start at 0%%: %v", s.symbol, s.pcts[0])
		}
	}
}

func TestCompareFillsTheTerminalWidth(t *testing.T) {
	m := newCompareModel("AAA")
	m.entries = []CompareEntry{{Symbol: "AAA", Data: genCandles(400)}}
	m.SetSize(215, 50)
	if got, want := m.visibleCount(), m.plotWidth(); got != want {
		t.Fatalf("compare shows %d points, want %d", got, want)
	}
}

func TestCompareRemoveKeepsRemainingColors(t *testing.T) {
	m := newCompareModel("AAA", "BBB", "CCC")
	m.entries = []CompareEntry{
		{Symbol: "AAA", Data: genCandles(80)},
		{Symbol: "BBB", Data: genCandles(80)},
		{Symbol: "CCC", Data: genCandles(80)},
	}
	m.active = true
	m, _ = m.Update(key("x"))

	if len(m.symbols) != 2 {
		t.Fatalf("symbols = %v, want 2", m.symbols)
	}
	for _, e := range m.entries {
		if e.Symbol == "CCC" {
			t.Fatal("removed symbol still has an entry")
		}
	}
	palette := compareColorList()
	if got := compareColorFor(m.symbols, "BBB"); got != palette[1] {
		t.Fatalf("BBB changed color after CCC was removed: %v", got)
	}
}

func TestCompareViewNeverPanics(t *testing.T) {
	for _, w := range []int{0, 1, 2, 11, 12, 13, 40, 215} {
		for _, h := range []int{0, 1, 5, 6, 7, 40} {
			m := newCompareModel("AAA", "BBB", "CCC")
			m.SetSize(w, h)
			m.entries = []CompareEntry{
				{Symbol: "AAA", Data: genCandles(200)},
				{Symbol: "BBB", Data: genCandles(3)},
				{Symbol: "CCC", Data: nil},
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("compare View panicked at %dx%d: %v", w, h, r)
					}
				}()
				_ = m.View()
			}()
		}
	}
}

func TestCompareCapsSymbolCount(t *testing.T) {
	m := newCompareModel("A", "B", "C", "D")
	if len(m.symbols) != maxCompareSymbols {
		t.Fatalf("symbols = %v, want %d", m.symbols, maxCompareSymbols)
	}
	m.AddSymbol("A")
	if len(m.symbols) != maxCompareSymbols {
		t.Fatal("duplicate symbol added")
	}
	if len(compareColorList()) < maxCompareSymbols {
		t.Fatal("palette is smaller than the comparison set; two series would share a color")
	}
}

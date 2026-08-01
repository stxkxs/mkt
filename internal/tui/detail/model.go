package detail

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stxkxs/mkt/internal/market"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/coinbase"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/format"
	"github.com/stxkxs/mkt/internal/tui/theme"
)

var (
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
)

// RebuildStyles refreshes local styles from current theme colors.
func RebuildStyles() {
	styleLabel = lipgloss.NewStyle().Foreground(theme.ColorDim)
	styleValue = lipgloss.NewStyle().Foreground(theme.ColorFg)
}

// bookSource is the slice of *coinbase.Provider the detail panel needs:
// a REST snapshot to paint immediately and a reconnecting level2 stream
// to keep it current. Narrow so tests can supply a fake.
type bookSource interface {
	FetchOrderBook(ctx context.Context, productID string) (coinbase.OrderBook, error)
	StreamOrderBookLoop(ctx context.Context, productID string, out chan<- coinbase.OrderBook, status chan<- coinbase.OrderBookStatus) error
}

// Model is the detail panel for a selected symbol.
type Model struct {
	symbol string
	quote  provider.Quote
	cache  *market.Cache
	cb     bookSource
	book   coinbase.OrderBook
	notes  map[string]string
	width  int
	height int
	active bool

	// ctx is the parent for live streams, so an app-level shutdown
	// reaches them. nil falls back to context.Background.
	ctx context.Context

	// Live level2 state. liveCancel tears the streamer down when the
	// symbol changes or the panel closes; liveSym records what it was
	// started for so a late message from a previous symbol is dropped.
	// The rest is the last reported connection state, which is what
	// lets the view mark a frozen book stale instead of rendering it as
	// if it were live.
	liveCancel context.CancelFunc
	liveSym    string
	bookLive   bool
	bookErr    string
	bookRetry  time.Duration
	bookAt     time.Time

	// now is injectable for tests; nil means time.Now.
	now func() time.Time
}

// New creates a detail model. The coinbase provider is used to fetch
// order books for crypto symbols when shown; pass nil to disable.
func New(cache *market.Cache, cb *coinbase.Provider) Model {
	m := Model{cache: cache}
	// A typed nil in an interface is not nil, so only store a live one.
	if cb != nil {
		m.cb = cb
	}
	return m
}

// SetContext supplies the parent context for live level2 streams so an
// app-level cancel tears them down. Call before the first SetSymbol;
// streams started without it hang off context.Background.
func (m *Model) SetContext(ctx context.Context) {
	m.ctx = ctx
}

func (m Model) parentContext() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m Model) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// SetNotes seeds the per-symbol freeform notes map, keyed by uppercased
// symbol. Normalizing here makes lookups robust to config casing — viper
// lowercases map keys, so a `notes: { NVDA: ... }` entry arrives as
// "nvda" and would otherwise never match the uppercase active symbol.
func (m *Model) SetNotes(notes map[string]string) {
	up := make(map[string]string, len(notes))
	for k, v := range notes {
		up[strings.ToUpper(k)] = v
	}
	m.notes = up
}

// orderBookProgram is satisfied by tea.Program — kept narrow so tests
// can swap a fake.
type orderBookProgram interface {
	Send(msg tea.Msg)
}

// liveProgram is set by the dashboard before the program starts so the
// detail panel can push live OrderBook updates into the event loop, and
// cleared at shutdown. It is written from the main goroutine and read
// from every streamer goroutine, so it is guarded — an unsynchronized
// read racing the shutdown write is how the streamer ends up calling
// Send on a torn-down program. Optional: when nil, the panel falls back
// to the REST snapshot only.
var (
	liveMu      sync.RWMutex
	liveProgram orderBookProgram
)

// SetLiveProgram registers the bubbletea program for live-update
// dispatch from the level2 streamer goroutine. Pass nil to disable.
func SetLiveProgram(p orderBookProgram) {
	liveMu.Lock()
	liveProgram = p
	liveMu.Unlock()
}

// sendLive dispatches a message to the registered program, if any. The
// lookup happens per send so a shutdown between two order-book updates
// stops the second one.
func sendLive(msg tea.Msg) {
	liveMu.RLock()
	p := liveProgram
	liveMu.RUnlock()
	if p != nil {
		p.Send(msg)
	}
}

// liveEnabled reports whether a program is currently registered.
func liveEnabled() bool {
	liveMu.RLock()
	defer liveMu.RUnlock()
	return liveProgram != nil
}

// SetSymbol updates the displayed symbol and returns a tea.Cmd that
// fetches an initial REST snapshot of the order book if the symbol is
// crypto. The Cmd is nil for non-crypto symbols or when no coinbase
// provider is configured. SetSymbol also tears down any prior live
// level2 streamer and (when a liveProgram is registered) starts a new
// one for the new symbol.
func (m *Model) SetSymbol(sym string) tea.Cmd {
	m.stopLive()
	m.symbol = sym
	m.book = coinbase.OrderBook{}
	m.bookAt = time.Time{}

	if m.cb == nil || !symbol.IsCrypto(sym) {
		return nil
	}
	cb := m.cb
	ctx := m.parentContext()

	// Start the reconnecting level2 streamer when a live program is
	// registered. Each book snapshot and each connection transition
	// becomes a message in the bubbletea loop.
	if liveEnabled() {
		streamCtx, cancel := context.WithCancel(ctx)
		m.liveCancel = cancel
		m.liveSym = sym
		go streamLevel2(streamCtx, cb, sym)
	}

	return func() tea.Msg {
		b, err := cb.FetchOrderBook(ctx, sym)
		if err != nil {
			return nil
		}
		return orderBookLoadedMsg{book: b}
	}
}

// stopLive cancels any running level2 streamer and forgets its
// connection state. Safe to call repeatedly.
func (m *Model) stopLive() {
	if m.liveCancel != nil {
		m.liveCancel()
		m.liveCancel = nil
	}
	m.liveSym = ""
	m.bookLive = false
	m.bookErr = ""
	m.bookRetry = 0
}

// streamLevel2 pumps a reconnecting level2 subscription into the
// bubbletea event loop until ctx is cancelled.
//
// The single-shot StreamOrderBook returns on the first dropped socket;
// discarding that error left the book frozen at whatever the last update
// happened to be, forever, with nothing on screen to say so. Using the
// looping form and forwarding its status transitions is what makes a
// dead socket visible.
func streamLevel2(ctx context.Context, src bookSource, sym string) {
	books := make(chan coinbase.OrderBook, 4)
	status := make(chan coinbase.OrderBookStatus, 4)
	go func() {
		defer close(books)
		defer close(status)
		_ = src.StreamOrderBookLoop(ctx, sym, books, status)
	}()
	for books != nil || status != nil {
		select {
		case b, ok := <-books:
			if !ok {
				books = nil
				continue
			}
			sendLive(orderBookLoadedMsg{book: b, symbol: sym})
		case st, ok := <-status:
			if !ok {
				status = nil
				continue
			}
			sendLive(orderBookStatusMsg{status: st, symbol: sym})
		}
	}
}

// orderBookLoadedMsg carries a book snapshot. symbol is the symbol the
// stream was started for, empty for the one-shot REST fetch.
type orderBookLoadedMsg struct {
	book   coinbase.OrderBook
	symbol string
}

// orderBookStatusMsg carries a level2 connection transition.
type orderBookStatusMsg struct {
	status coinbase.OrderBookStatus
	symbol string
}

// SetSize updates dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetActive sets whether this panel is active. When deactivated, any
// live level2 streamer is cancelled — otherwise every open/close cycle
// leaks a WebSocket and its keepalive goroutine.
func (m *Model) SetActive(a bool) {
	m.active = a
	if !a {
		m.stopLive()
	}
}

// Active returns whether the panel is active.
func (m Model) Active() bool {
	return m.active
}

// Symbol returns the current symbol.
func (m Model) Symbol() string {
	return m.symbol
}

// UpdateQuote processes a new quote.
func (m *Model) UpdateQuote(q provider.Quote) {
	if q.Symbol == m.symbol {
		m.quote = q
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case theme.ChangedMsg:
		RebuildStyles()
		return m, nil
	case orderBookLoadedMsg:
		// Only keep the book if it still matches the displayed symbol.
		if !m.forThisSymbol(msg.symbol) {
			return m, nil
		}
		if msg.book.ProductID == "" || strings.EqualFold(msg.book.ProductID, m.symbol) {
			m.book = msg.book
			m.bookAt = m.clock()
		}
		return m, nil
	case orderBookStatusMsg:
		if !m.forThisSymbol(msg.symbol) {
			return m, nil
		}
		m.bookLive = msg.status.Connected
		m.bookRetry = msg.status.Retry
		m.bookErr = ""
		if msg.status.Err != nil {
			m.bookErr = msg.status.Err.Error()
		}
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			// Closing the panel must tear the stream down; leaving
			// active=false alone leaked one socket per open/close.
			m.stopLive()
			m.active = false
		}
	}
	return m, nil
}

// forThisSymbol reports whether a streamer message belongs to the
// symbol currently on screen. Messages from a cancelled stream can still
// be in flight when the user moves on.
func (m Model) forThisSymbol(sym string) bool {
	return sym == "" || strings.EqualFold(sym, m.symbol)
}

// bookHeading labels the order book with its live state: connected,
// reconnecting after a drop, or a plain REST snapshot with no stream.
func (m Model) bookHeading() string {
	head := lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).
		Render("Order Book (top 5)")
	if m.liveSym == "" {
		return head
	}
	if m.bookLive {
		age := ""
		if !m.bookAt.IsZero() {
			age = fmt.Sprintf(" · %s ago", m.clock().Sub(m.bookAt).Truncate(time.Second))
		}
		return head + theme.StyleUp.Render("  ● live"+age)
	}
	// Before the first transition arrives there is nothing stale yet —
	// the stream is simply still coming up.
	if m.bookErr == "" && m.bookRetry == 0 {
		return head + theme.StyleDim.Render("  ○ connecting")
	}
	msg := "  ○ stale"
	if m.bookRetry > 0 {
		msg += fmt.Sprintf(" · retrying in %s", m.bookRetry.Truncate(time.Second))
	}
	if m.bookErr != "" {
		msg += " · " + format.Truncate(m.bookErr, 40)
	}
	return head + theme.StyleDown.Render(msg)
}

// View renders the detail panel.
func (m Model) View() string {
	if m.symbol == "" || m.width <= 0 {
		return ""
	}

	var sb strings.Builder

	// Header
	header := lipgloss.NewStyle().
		Foreground(theme.ColorAccent).
		Bold(true).
		Render(fmt.Sprintf("  %s Detail", m.symbol))
	sb.WriteString(header)
	sb.WriteString("\n\n")

	if m.quote.Price == 0 {
		sb.WriteString(styleLabel.Render("  Waiting for data..."))
		return sb.String()
	}

	q := m.quote

	// Price + change
	changeStyle := theme.StyleUp
	arrow := "▲"
	if q.ChangePct < 0 {
		changeStyle = theme.StyleDown
		arrow = "▼"
	}
	sb.WriteString(fmt.Sprintf("  %s  %s\n\n",
		styleValue.Bold(true).Render(format.FormatPrice(q.Price)),
		changeStyle.Render(fmt.Sprintf("%s %.2f (%.2f%%)", arrow, q.Change, q.ChangePct)),
	))

	// Details grid
	details := []struct{ label, value string }{
		{"24h High", format.FormatPrice(q.High24h)},
		{"24h Low", format.FormatPrice(q.Low24h)},
		{"Volume", format.FormatVolume(q.Volume)},
		{"Provider", q.Provider},
		{"Type", q.Asset.String()},
	}
	for _, d := range details {
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			styleLabel.Render(fmt.Sprintf("%-12s", d.label)),
			styleValue.Render(d.value),
		))
	}

	// Sparkline chart
	sb.WriteString("\n")
	prices := m.cache.Prices(m.symbol)
	if len(prices) > 0 {
		chartWidth := m.width - 4
		if chartWidth > 80 {
			chartWidth = 80
		}
		if chartWidth < 1 {
			chartWidth = 1
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorCyan).Render(
			"  " + format.BrailleSparkline(prices, chartWidth),
		))
		sb.WriteString("\n")
	}

	// Freeform notes for this symbol (keys normalized to uppercase).
	if note := m.notes[strings.ToUpper(m.symbol)]; note != "" {
		sb.WriteString("\n  ")
		sb.WriteString(lipgloss.NewStyle().Foreground(theme.ColorAccent).Bold(true).Render("Notes"))
		sb.WriteString("\n")
		for _, line := range strings.Split(note, "\n") {
			sb.WriteString("  ")
			sb.WriteString(styleValue.Render(line))
			sb.WriteString("\n")
		}
	}

	// Order book (top 5 per side) for crypto symbols. The heading shows
	// up as soon as a stream exists, so "connecting" and "stale" are
	// visible even before the first snapshot lands.
	if m.liveSym != "" || len(m.book.Bids) > 0 || len(m.book.Asks) > 0 {
		sb.WriteString("\n  ")
		sb.WriteString(m.bookHeading())
		sb.WriteString("\n")
		n := 5
		if n > len(m.book.Bids) {
			n = len(m.book.Bids)
		}
		na := 5
		if na > len(m.book.Asks) {
			na = len(m.book.Asks)
		}
		rows := n
		if na > rows {
			rows = na
		}
		for i := 0; i < rows; i++ {
			var bidStr, askStr string
			if i < n {
				bidStr = fmt.Sprintf("%10.2f x %.4f", m.book.Bids[i].Price, m.book.Bids[i].Size)
			} else {
				bidStr = format.Spaces(21)
			}
			if i < na {
				askStr = fmt.Sprintf("%10.2f x %.4f", m.book.Asks[i].Price, m.book.Asks[i].Size)
			}
			sb.WriteString(fmt.Sprintf("  %s    %s\n",
				theme.StyleUp.Render(bidStr),
				theme.StyleDown.Render(askStr),
			))
		}
	}

	return sb.String()
}

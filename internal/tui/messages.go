package tui

import (
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/news"
	"github.com/stxkxs/mkt/internal/provider"
)

// QuoteUpdateMsg is sent when a new quote arrives from any provider.
type QuoteUpdateMsg struct {
	Quote provider.Quote
}

// HistoryLoadedMsg is sent when historical OHLCV data is loaded.
type HistoryLoadedMsg struct {
	Symbol string
	Data   []provider.OHLCV
}

// AlertTriggeredMsg is sent when a price alert fires.
type AlertTriggeredMsg struct {
	Alert alert.TriggeredAlert
}

// ConnectionStatusMsg reports provider connection state.
type ConnectionStatusMsg struct {
	Provider  string
	Connected bool
	Error     error
}

// ErrorMsg wraps errors for display.
type ErrorMsg struct {
	Err error
}

// MacroUpdateMsg is sent when macro indicator data arrives.
type MacroUpdateMsg struct {
	Quotes []provider.Quote
}

// NewsUpdateMsg is sent when news headlines are fetched.
type NewsUpdateMsg struct {
	Headlines []news.Headline
}

// SpinnerTickMsg drives the braille loading spinner animation.
type SpinnerTickMsg struct{}

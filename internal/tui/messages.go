package tui

import (
	"github.com/stxkxs/mkt/internal/alert"
	"github.com/stxkxs/mkt/internal/news"
	"github.com/stxkxs/mkt/internal/portfolio"
	"github.com/stxkxs/mkt/internal/provider"
	"github.com/stxkxs/mkt/internal/provider/binance"
	calendarpkg "github.com/stxkxs/mkt/internal/provider/calendar"
	"github.com/stxkxs/mkt/internal/provider/defillama"
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

// DeFiUpdateMsg is sent when DeFi TVL data arrives.
type DeFiUpdateMsg struct {
	Chains []defillama.TVLSnapshot
}

// FuturesUpdateMsg is sent when Binance futures snapshots arrive.
type FuturesUpdateMsg struct {
	Snapshots []binance.FuturesSnapshot
}

// EquityMarkMsg is sent when a newly persisted portfolio equity mark
// should be reflected in the TUI.
type EquityMarkMsg struct {
	Mark portfolio.EquityMark
}

// CalendarUpdateMsg is sent when the calendar events list changes
// (e.g., earnings fetched asynchronously after startup).
type CalendarUpdateMsg struct {
	Events []calendarpkg.Event
}

// SpinnerTickMsg drives the braille loading spinner animation.
type SpinnerTickMsg struct{}

// UnroutableSymbolsMsg carries the symbols the hub could not route to any
// provider (the return value of market.Hub.Start). They will never quote,
// so the dashboard names them instead of leaving a blank row up forever.
// Equivalent to calling App.SetUnroutable; use the message when the data
// plane is shared by several sessions.
type UnroutableSymbolsMsg struct {
	Symbols []string
}

// ConfigStatusMsg carries how the config file loaded. Equivalent to
// calling App.SetConfigStatus; use the message when the data plane is
// shared by several sessions.
type ConfigStatusMsg struct {
	Status ConfigStatus
}

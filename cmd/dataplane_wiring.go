package cmd

import (
	"context"
)

// The interfaces below describe the small surface the data plane needs on
// the root TUI model for state only cmd can know: a parse error in the
// config file, the symbols no provider accepted, and the parent context that
// bounds live order-book streams.
//
// They are consumed as optional interfaces rather than called directly on
// *tui.App so the data plane and the TUI can land independently: cmd wires
// whatever the model implements and skips what it does not. Once every
// method exists on *tui.App these can collapse into plain calls.
type (
	// configBannerLoader receives the degraded-config state so the TUI can
	// render a persistent banner. path is the file that failed to parse,
	// line is a best-effort 1-based position (0 when unknown), and err is
	// the parse error. Called at most once, before the program runs.
	configBannerLoader interface {
		LoadConfigBanner(path string, line int, err error)
	}

	// unroutableLoader receives the symbols no provider claimed, in the
	// user's own spelling, so the affected watchlist rows can be marked as
	// unroutable instead of sitting at "no price yet" forever.
	unroutableLoader interface {
		LoadUnroutableSymbols(symbols []string)
	}

	// contextSetter receives the process-lifetime context so per-session
	// streams (the crypto detail view's level-2 book) stop when the data
	// plane is cancelled instead of leaking a goroutine per visit.
	contextSetter interface {
		SetContext(ctx context.Context)
	}
)

// applyWiring hands a model whatever data-plane state it is able to accept.
// Every step is optional; a model that implements none of the interfaces is
// left untouched.
//
// It is idempotent and safe to call more than once on the same model, which
// is what lets `mkt` call it at build time and again once hub routing has
// resolved. Calls must happen before the program runs, since they mutate the
// model outside Bubbletea's Update loop.
func (b *backend) applyWiring(ctx context.Context, model any) {
	if s, ok := model.(contextSetter); ok {
		s.SetContext(ctx)
	}
	if b.degraded {
		if l, ok := model.(configBannerLoader); ok {
			l.LoadConfigBanner(b.configPath, b.configLine, b.configErr)
		}
	}
	if len(b.unroutable) > 0 {
		if l, ok := model.(unroutableLoader); ok {
			l.LoadUnroutableSymbols(b.unroutable)
		}
	}
}

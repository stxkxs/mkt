// Package broadcast fans out bubbletea messages to a dynamic set of
// running programs.
//
// The dashboard (`mkt`) registers its single program; the SSH server
// (`mkt serve`) registers one program per live session and removes it on
// disconnect. That lets a single shared data plane — the hub plus every
// background poller — drive many independent TUIs without knowing how
// many exist. With no programs registered, Send is a harmless no-op, so
// the data plane may run before (or after) any UI is attached.
package broadcast

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// Sender is the narrow slice of *tea.Program the broadcaster depends on.
// *tea.Program satisfies it; tests can supply a fake.
type Sender interface {
	Send(msg tea.Msg)
}

// Broadcaster holds a set of registered senders and forwards each message
// to all of them. Safe for concurrent use by the data-plane goroutines
// and the session (de)registration path.
type Broadcaster struct {
	mu      sync.RWMutex
	senders map[Sender]struct{}
}

// New constructs an empty Broadcaster.
func New() *Broadcaster {
	return &Broadcaster{senders: make(map[Sender]struct{})}
}

// Add registers a sender to receive future broadcasts.
func (b *Broadcaster) Add(s Sender) {
	b.mu.Lock()
	b.senders[s] = struct{}{}
	b.mu.Unlock()
}

// Remove deregisters a sender — call it when an SSH session ends so the
// broadcaster stops forwarding to a finished program.
func (b *Broadcaster) Remove(s Sender) {
	b.mu.Lock()
	delete(b.senders, s)
	b.mu.Unlock()
}

// Send forwards msg to every registered sender. tea.Program.Send is
// itself safe to call on a finished program (it selects on the program's
// done context), so a session that disconnected between Remove calls
// won't block the fan-out.
func (b *Broadcaster) Send(msg tea.Msg) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.senders {
		s.Send(msg)
	}
}

// Len reports how many senders are registered (used for serve logging).
func (b *Broadcaster) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.senders)
}

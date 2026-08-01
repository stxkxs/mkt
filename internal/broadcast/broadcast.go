// Package broadcast fans out bubbletea messages to a dynamic set of
// running programs.
//
// The dashboard (`mkt`) registers its single program; the SSH server
// (`mkt serve`) registers one program per live session and removes it on
// disconnect. That lets a single shared data plane — the hub plus every
// background poller — drive many independent TUIs without knowing how
// many exist. With no programs registered, Send is a harmless no-op, so
// the data plane may run before (or after) any UI is attached.
//
// # Isolation
//
// tea.Program.Send is a blocking send on an unbuffered channel: it parks
// until the program's event loop picks the message up (or the program
// exits). A session whose loop is stalled — a SIGSTOP'd client, a frozen
// terminal, a link that stopped draining — therefore blocks its caller
// indefinitely. Since Send runs on the hub's dispatcher goroutine, a
// direct fan-out would let one wedged session stall quote delivery for
// every other session, and stall SSH connect/disconnect behind it.
//
// So the broadcaster never calls a Sender inline. Each registered sender
// gets its own bounded queue and pump goroutine; Send only enqueues.
// A sender that stops draining fills its own queue and starts dropping
// its own messages, and nothing else notices.
package broadcast

import (
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

// DefaultQueueSize is the per-sender queue depth used by New. It is
// generous on purpose: a session that falls this far behind is not
// merely slow, it is wedged, so dropping its messages costs nothing a
// live session would have seen.
const DefaultQueueSize = 256

// Sender is the narrow slice of *tea.Program the broadcaster depends on.
// *tea.Program satisfies it; tests can supply a fake.
type Sender interface {
	Send(msg tea.Msg)
}

// Broadcaster holds a set of registered senders and forwards each message
// to all of them. Safe for concurrent use by the data-plane goroutines
// and the session (de)registration path.
type Broadcaster struct {
	queueSize int

	mu   sync.RWMutex
	subs map[Sender]*subscriber

	// retired carries the drop counts of removed subscribers so Drops
	// stays monotonic across session churn.
	retired atomic.Uint64
}

// New constructs an empty Broadcaster with DefaultQueueSize queue depth
// per sender.
func New() *Broadcaster {
	return NewSized(DefaultQueueSize)
}

// NewSized constructs an empty Broadcaster whose senders each get a
// queue of the given depth. Sizes below 1 fall back to
// DefaultQueueSize. Mostly useful for tests that want to provoke
// overflow without pushing hundreds of messages.
func NewSized(queue int) *Broadcaster {
	if queue < 1 {
		queue = DefaultQueueSize
	}
	return &Broadcaster{queueSize: queue, subs: make(map[Sender]*subscriber)}
}

// Add registers a sender to receive future broadcasts and starts its
// pump goroutine. Registering the same sender twice is a no-op — the
// first registration keeps its queue and its pump.
func (b *Broadcaster) Add(s Sender) {
	b.mu.Lock()
	if _, dup := b.subs[s]; dup {
		b.mu.Unlock()
		return
	}
	sub := newSubscriber(s, b.queueSize)
	b.subs[s] = sub
	b.mu.Unlock()

	go sub.pump()
}

// Remove deregisters a sender — call it when an SSH session ends so the
// broadcaster stops forwarding to a finished program. It returns
// immediately: the sender's queue is discarded and its pump wakes up and
// exits. Safe to call concurrently with Send, and safe to call more than
// once.
//
// If the pump is parked inside a Sender.Send that never returns, that one
// goroutine stays parked until the call unblocks — for *tea.Program that
// happens as soon as the program's context is done, which is exactly what
// a finished SSH session provides. No further messages are handed to a
// removed sender either way.
func (b *Broadcaster) Remove(s Sender) {
	b.mu.Lock()
	sub, ok := b.subs[s]
	if ok {
		delete(b.subs, s)
		b.retired.Add(sub.drops.Load())
	}
	b.mu.Unlock()

	if ok {
		sub.stop()
	}
}

// Send hands msg to every registered sender's queue and returns. It never
// blocks on a sender: delivery happens on that sender's pump goroutine,
// and a full queue drops rather than waits.
func (b *Broadcaster) Send(msg tea.Msg) {
	b.mu.RLock()
	for _, sub := range b.subs {
		sub.push(msg)
	}
	b.mu.RUnlock()
}

// Len reports how many senders are registered (used for serve logging).
func (b *Broadcaster) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Drops reports how many messages have been dropped across all senders,
// including senders that have since been removed. It only moves when a
// sender stopped draining its queue, so `mkt serve` can log it as a
// direct signal that some session is wedged.
func (b *Broadcaster) Drops() uint64 {
	total := b.retired.Load()
	b.mu.RLock()
	for _, sub := range b.subs {
		total += sub.drops.Load()
	}
	b.mu.RUnlock()
	return total
}

// DropsFor reports how many messages have been dropped for one
// registered sender, so a per-session log line can name the session that
// is falling behind. Unknown or already-removed senders report 0.
func (b *Broadcaster) DropsFor(s Sender) uint64 {
	b.mu.RLock()
	sub, ok := b.subs[s]
	b.mu.RUnlock()
	if !ok {
		return 0
	}
	return sub.drops.Load()
}

// subscriber owns one sender's queue and the goroutine that drains it.
//
// The queue is a fixed-size ring guarded by mu, with cond used to park
// the pump while it is empty. A ring plus a condition variable — rather
// than a buffered channel — keeps Remove race-free: there is no channel
// to close, so a Send racing a Remove can never send on a closed channel.
type subscriber struct {
	sender Sender

	mu      sync.Mutex
	cond    *sync.Cond
	buf     []tea.Msg
	head    int  // index of the oldest queued message
	count   int  // number of queued messages
	stopped bool // set by stop; the pump exits and push becomes a no-op

	drops atomic.Uint64

	// done is closed when the pump goroutine returns. Only tests read it;
	// production code has nothing to wait for.
	done chan struct{}
}

func newSubscriber(s Sender, queue int) *subscriber {
	sub := &subscriber{
		sender: s,
		buf:    make([]tea.Msg, queue),
		done:   make(chan struct{}),
	}
	sub.cond = sync.NewCond(&sub.mu)
	return sub
}

// push queues msg without ever blocking. When the queue is full the
// oldest queued message is evicted to make room — newest-wins, because
// the payload is market data: a fresh quote supersedes the stale one it
// replaces, and a session that is behind wants the current price, not
// the backlog it missed. Alert notifications ride the same path, which is
// why the queue is deep enough that only a wedged session ever overflows.
func (s *subscriber) push(msg tea.Msg) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if s.count == len(s.buf) {
		s.buf[s.head] = nil
		s.head = (s.head + 1) % len(s.buf)
		s.count--
		s.drops.Add(1)
	}
	s.buf[(s.head+s.count)%len(s.buf)] = msg
	s.count++
	s.mu.Unlock()

	s.cond.Signal()
}

// next blocks until a message is queued or the subscriber is stopped,
// reporting false once the pump should exit.
func (s *subscriber) next() (tea.Msg, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.count == 0 && !s.stopped {
		s.cond.Wait()
	}
	if s.stopped {
		return nil, false
	}
	msg := s.buf[s.head]
	s.buf[s.head] = nil
	s.head = (s.head + 1) % len(s.buf)
	s.count--
	return msg, true
}

// stop discards the queue and wakes the pump so it exits. Idempotent.
func (s *subscriber) stop() {
	s.mu.Lock()
	s.stopped = true
	clear(s.buf)
	s.head, s.count = 0, 0
	s.mu.Unlock()

	s.cond.Broadcast()
}

// pump delivers queued messages to the sender, one at a time and in
// order, until the subscriber is stopped. This is the only place a
// Sender is called, so a Send that blocks forever costs exactly one
// goroutine and affects no other sender.
func (s *subscriber) pump() {
	defer close(s.done)
	for {
		msg, ok := s.next()
		if !ok {
			return
		}
		s.sender.Send(msg)
	}
}

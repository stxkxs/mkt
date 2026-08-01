package alert

import (
	"context"
	"log"

	"golang.org/x/time/rate"
)

// Per-notifier queue depth for the asynchronous fan-out. Deep enough to
// absorb a burst of triggers while one destination is slow; past that the
// engine drops rather than stalling the goroutine that called Check.
const notifierQueueDepth = 64

// notifierSink owns one Notifier and delivers to it from a single
// goroutine fed by a bounded queue. Check and Inject only enqueue, so a
// webhook that takes its full five-second timeout costs one parked
// goroutine instead of stalling quote fan-out for the whole app, and one
// wedged destination cannot delay its siblings.
type notifierSink struct {
	engine  *Engine
	n       Notifier
	limiter *rate.Limiter
	queue   chan TriggeredAlert
}

// newNotifierSink wraps n in a queue and starts its delivery goroutine.
// The goroutine lives for the process; there is exactly one per
// registered notifier.
func newNotifierSink(e *Engine, n Notifier) *notifierSink {
	s := &notifierSink{
		engine:  e,
		n:       n,
		limiter: rate.NewLimiter(rate.Every(notifierMinInterval), notifierBurst),
		queue:   make(chan TriggeredAlert, notifierQueueDepth),
	}
	go s.pump()
	return s
}

// send queues an alert for delivery. A full queue means the destination
// has stopped keeping up, so the alert is dropped and counted rather than
// blocking the caller.
func (s *notifierSink) send(a TriggeredAlert) {
	s.engine.inflight.Add(1)
	select {
	case s.queue <- a:
	default:
		s.engine.inflight.Add(-1)
		s.engine.notifyDrops.Add(1)
		log.Printf("alert notifier %s: queue full, dropping alert for %s", s.n.Name(), a.Rule.Symbol)
	}
}

// pump delivers queued alerts in order, one at a time.
func (s *notifierSink) pump() {
	for a := range s.queue {
		s.deliver(a)
		s.engine.inflight.Add(-1)
	}
}

// deliver applies the per-notifier rate limit and calls Notify with a
// deadline. Errors are logged and never propagated — one failing
// destination must not affect the others.
func (s *notifierSink) deliver(a TriggeredAlert) {
	if !s.limiter.Allow() {
		s.engine.notifyDrops.Add(1)
		log.Printf("alert notifier %s: rate-limited, dropping alert for %s", s.n.Name(), a.Rule.Symbol)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	if err := s.n.Notify(ctx, a); err != nil {
		log.Printf("alert notifier %s: %v", s.n.Name(), err)
	}
}

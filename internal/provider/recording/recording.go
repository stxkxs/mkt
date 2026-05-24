package recording

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/stxkxs/mkt/internal/provider"
)

// Sink is a thread-safe NDJSON writer that one or more Recordings may
// share. The caller owns the sink's lifecycle and must call Close when
// recording is complete.
type Sink struct {
	mu   sync.Mutex
	w    io.WriteCloser
	path string
}

// NewSink opens path for writing, truncating any existing content.
func NewSink(path string) (*Sink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("recording: create %s: %w", path, err)
	}
	return &Sink{w: f, path: path}, nil
}

// Path returns the sink's file path.
func (s *Sink) Path() string { return s.path }

// Close releases the underlying file. Subsequent writes return an error.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return nil
	}
	err := s.w.Close()
	s.w = nil
	return err
}

func (s *Sink) write(q provider.Quote) error {
	b, err := encode(q)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return fmt.Errorf("sink closed")
	}
	_, err = s.w.Write(b)
	return err
}

// Recording wraps a provider.QuoteProvider and writes every observed
// quote to a Sink before forwarding it to the consumer.
type Recording struct {
	inner provider.QuoteProvider
	sink  *Sink
}

// New creates a Recording that delegates to inner and tees quotes to sink.
// The caller owns the sink and is responsible for closing it.
func New(inner provider.QuoteProvider, sink *Sink) *Recording {
	return &Recording{inner: inner, sink: sink}
}

// Name implements provider.QuoteProvider.
func (r *Recording) Name() string {
	return "recording(" + r.inner.Name() + ")"
}

// Supports implements provider.QuoteProvider by delegating to the inner.
func (r *Recording) Supports(symbol string) bool {
	return r.inner.Supports(symbol)
}

// Subscribe implements provider.QuoteProvider. Each quote produced by
// the inner provider is written to the sink and then forwarded to out.
// Inner errors are returned; sink-write errors are logged but never
// break forwarding.
func (r *Recording) Subscribe(ctx context.Context, symbols []string, out chan<- provider.Quote) error {
	tee := make(chan provider.Quote, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case q, ok := <-tee:
				if !ok {
					return
				}
				if err := r.sink.write(q); err != nil {
					log.Printf("recording %s: %v", r.sink.Path(), err)
				}
				select {
				case out <- q:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	err := r.inner.Subscribe(ctx, symbols, tee)
	close(tee)
	<-done
	return err
}

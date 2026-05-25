package cmd

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// poll must run fetch once immediately and continue on each tick until
// the context cancels.
func TestPollRunsImmediatelyThenOnEveryTick(t *testing.T) {
	var n int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		poll(ctx, 20*time.Millisecond, func() {
			atomic.AddInt32(&n, 1)
		})
		close(done)
	}()

	// Wait long enough for the initial fire + ~2 ticks, then stop.
	time.Sleep(70 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll did not return after ctx cancel")
	}

	got := atomic.LoadInt32(&n)
	if got < 3 {
		t.Errorf("expected at least 3 invocations (initial + 2 ticks), got %d", got)
	}
}

// Cancelling before the first tick must still let the initial fetch
// run (caller depends on at-least-once semantics on startup).
func TestPollFiresInitialEvenIfCancelledEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var n int32
	done := make(chan struct{})
	go func() {
		poll(ctx, time.Hour, func() {
			atomic.AddInt32(&n, 1)
		})
		close(done)
	}()

	// Give the goroutine a moment to start, then cancel before any tick.
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll did not return after ctx cancel")
	}
	if got := atomic.LoadInt32(&n); got != 1 {
		t.Errorf("expected exactly 1 initial invocation, got %d", got)
	}
}

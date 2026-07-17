package broadcast

import (
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type fakeSender struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (f *fakeSender) Send(msg tea.Msg) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	f.mu.Unlock()
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

type msg struct{ n int }

func TestBroadcastFansOutToAll(t *testing.T) {
	b := New()
	a, c := &fakeSender{}, &fakeSender{}
	b.Add(a)
	b.Add(c)
	if b.Len() != 2 {
		t.Fatalf("Len = %d, want 2", b.Len())
	}
	b.Send(msg{1})
	b.Send(msg{2})
	if a.count() != 2 || c.count() != 2 {
		t.Errorf("got a=%d c=%d, want both 2", a.count(), c.count())
	}
}

func TestRemoveStopsDelivery(t *testing.T) {
	b := New()
	a := &fakeSender{}
	b.Add(a)
	b.Send(msg{1})
	b.Remove(a)
	b.Send(msg{2})
	if a.count() != 1 {
		t.Errorf("got %d, want 1 (removed before second send)", a.count())
	}
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
}

func TestSendWithNoSendersIsNoop(t *testing.T) {
	b := New()
	b.Send(msg{1}) // must not panic
}

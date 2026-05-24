package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/provider"
)

func writeTape(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.ndjson")
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tape: %v", err)
	}
	return path
}

func drain(t *testing.T, ch <-chan provider.Quote, want int, timeout time.Duration) []provider.Quote {
	t.Helper()
	var got []provider.Quote
	deadline := time.After(timeout)
	for len(got) < want {
		select {
		case q := <-ch:
			got = append(got, q)
		case <-deadline:
			return got
		}
	}
	return got
}

func TestReplayFilter(t *testing.T) {
	path := writeTape(t, []string{
		`{"v":1,"symbol":"BTC-USD","price":50000,"asset":0,"ts":1700000000000000000}`,
		`{"v":1,"symbol":"ETH-USD","price":3000,"asset":0,"ts":1700000000100000000}`,
		`{"v":1,"symbol":"AAPL","price":200,"asset":1,"ts":1700000000200000000}`,
	})

	rep := NewReplay(path, ModeBurst)
	out := make(chan provider.Quote, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = rep.Subscribe(ctx, []string{"AAPL"}, out)
		close(done)
	}()

	got := drain(t, out, 1, 2*time.Second)
	<-done
	if len(got) != 1 {
		t.Fatalf("got %d quotes, want 1", len(got))
	}
	if got[0].Symbol != "AAPL" {
		t.Errorf("got symbol %q, want AAPL", got[0].Symbol)
	}
}

func TestReplayMalformedLineSkipped(t *testing.T) {
	path := writeTape(t, []string{
		`{"v":1,"symbol":"BTC-USD","price":50000,"asset":0,"ts":1700000000000000000}`,
		`this is not json`,
		`{"v":1,"symbol":"ETH-USD","price":3000,"asset":0,"ts":1700000000100000000}`,
	})

	rep := NewReplay(path, ModeBurst)
	out := make(chan provider.Quote, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = rep.Subscribe(ctx, nil, out)
		close(done)
	}()

	got := drain(t, out, 2, 2*time.Second)
	<-done
	if len(got) != 2 {
		t.Fatalf("got %d quotes, want 2 (malformed line should be skipped)", len(got))
	}
	if got[0].Symbol != "BTC-USD" || got[1].Symbol != "ETH-USD" {
		t.Errorf("ordering broken: %q %q", got[0].Symbol, got[1].Symbol)
	}
}

func TestReplayVersionRejected(t *testing.T) {
	path := writeTape(t, []string{
		`{"v":1,"symbol":"BTC-USD","price":50000,"asset":0,"ts":1700000000000000000}`,
		`{"v":999,"symbol":"FUTURE","price":1,"asset":0,"ts":1700000000100000000}`,
		`{"v":1,"symbol":"ETH-USD","price":3000,"asset":0,"ts":1700000000200000000}`,
	})

	rep := NewReplay(path, ModeBurst)
	out := make(chan provider.Quote, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = rep.Subscribe(ctx, nil, out)
		close(done)
	}()

	got := drain(t, out, 2, 2*time.Second)
	<-done
	if len(got) != 2 {
		t.Fatalf("got %d quotes, want 2 (future-version line should be skipped)", len(got))
	}
	for _, q := range got {
		if q.Symbol == "FUTURE" {
			t.Errorf("future-version quote leaked through: %+v", q)
		}
	}
}

func TestReplayMissingVersionRejected(t *testing.T) {
	path := writeTape(t, []string{
		`{"symbol":"BTC-USD","price":50000,"asset":0,"ts":1700000000000000000}`,
		`{"v":1,"symbol":"ETH-USD","price":3000,"asset":0,"ts":1700000000100000000}`,
	})

	rep := NewReplay(path, ModeBurst)
	out := make(chan provider.Quote, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = rep.Subscribe(ctx, nil, out)
		close(done)
	}()

	got := drain(t, out, 1, 2*time.Second)
	<-done
	if len(got) != 1 {
		t.Fatalf("got %d quotes, want 1", len(got))
	}
	if got[0].Symbol != "ETH-USD" {
		t.Errorf("unversioned line should be skipped, got %q first", got[0].Symbol)
	}
}

func TestReplayMissingFile(t *testing.T) {
	rep := NewReplay("/nonexistent/path/here.ndjson", ModeBurst)
	out := make(chan provider.Quote, 1)
	err := rep.Subscribe(context.Background(), nil, out)
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
}

func TestReplayName(t *testing.T) {
	rep := NewReplay("/tmp/foo.ndjson", ModeBurst)
	if got, want := rep.Name(), "replay(/tmp/foo.ndjson)"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

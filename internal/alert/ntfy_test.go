package alert

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type ntfyCapture struct {
	mu    sync.Mutex
	calls int
	path  string
	title string
	body  string
}

func TestNtfyPostsMessageAndTitle(t *testing.T) {
	cap := &ntfyCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.calls++
		cap.path = r.URL.Path
		cap.title = r.Header.Get("Title")
		cap.body = string(body)
		cap.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfyNotifier(srv.URL, "test-topic")
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.calls != 1 {
		t.Fatalf("calls = %d, want 1", cap.calls)
	}
	if cap.path != "/test-topic" {
		t.Errorf("path = %q, want /test-topic", cap.path)
	}
	if !strings.Contains(cap.title, "BTC-USD") {
		t.Errorf("title = %q, expected to contain BTC-USD", cap.title)
	}
	if !strings.Contains(cap.body, "crossed above") {
		t.Errorf("body = %q, expected message content", cap.body)
	}
}

func TestNtfyEmptyTopicIsNoop(t *testing.T) {
	n := NewNtfyNotifier("https://ntfy.sh", "")
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("expected nil for empty topic, got %v", err)
	}
}

func TestNtfyNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewNtfyNotifier(srv.URL, "topic")
	if err := n.Notify(context.Background(), sampleAlert()); err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

func TestNtfyDefaultsServer(t *testing.T) {
	// With an empty server arg, NewNtfyNotifier should default to ntfy.sh.
	// We don't make a network call — just verify the server field.
	n := NewNtfyNotifier("", "topic")
	if n.server != "https://ntfy.sh" {
		t.Errorf("server default = %q, want https://ntfy.sh", n.server)
	}
}

func TestNtfyName(t *testing.T) {
	n := NewNtfyNotifier("", "")
	if n.Name() != "ntfy" {
		t.Errorf("Name() = %q, want ntfy", n.Name())
	}
}

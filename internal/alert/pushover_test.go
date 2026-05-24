package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type pushoverCapture struct {
	mu          sync.Mutex
	calls       int
	form        map[string]string
	contentType string
}

func TestPushoverPostsFormFields(t *testing.T) {
	cap := &pushoverCapture{form: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cap.mu.Lock()
		cap.calls++
		cap.contentType = r.Header.Get("Content-Type")
		for k, v := range r.PostForm {
			if len(v) > 0 {
				cap.form[k] = v[0]
			}
		}
		cap.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewPushoverNotifier("u-key", "a-token")
	n.endpoint = srv.URL
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.calls != 1 {
		t.Fatalf("calls = %d, want 1", cap.calls)
	}
	if cap.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", cap.contentType)
	}
	if cap.form["token"] != "a-token" || cap.form["user"] != "u-key" {
		t.Errorf("auth fields wrong: %+v", cap.form)
	}
	if cap.form["title"] == "" || cap.form["message"] == "" {
		t.Errorf("title/message missing: %+v", cap.form)
	}
}

func TestPushoverEmptyConfigIsNoop(t *testing.T) {
	n := NewPushoverNotifier("", "")
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("expected nil for empty config, got %v", err)
	}

	n2 := NewPushoverNotifier("user", "")
	if err := n2.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("expected nil for missing token, got %v", err)
	}
}

func TestPushoverNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	n := NewPushoverNotifier("u", "t")
	n.endpoint = srv.URL
	if err := n.Notify(context.Background(), sampleAlert()); err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestPushoverName(t *testing.T) {
	n := NewPushoverNotifier("", "")
	if n.Name() != "pushover" {
		t.Errorf("Name() = %q, want pushover", n.Name())
	}
}

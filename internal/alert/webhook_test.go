package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func sampleAlert(webhooks ...string) TriggeredAlert {
	return TriggeredAlert{
		Rule: Rule{
			Symbol:    "BTC-USD",
			Condition: CondAbove,
			Value:     50000,
			Enabled:   true,
			Webhooks:  webhooks,
		},
		Price:     51000,
		Message:   "BTC-USD crossed above 50000",
		Timestamp: time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	}
}

type captured struct {
	mu      sync.Mutex
	calls   int
	last    webhookPayload
	lastCT  string
	lastReq string
}

func newTestServer(t *testing.T, status int, cap *captured) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.mu.Lock()
		cap.calls++
		cap.lastCT = r.Header.Get("Content-Type")
		cap.lastReq = r.Method
		_ = json.Unmarshal(body, &cap.last)
		cap.mu.Unlock()
		w.WriteHeader(status)
	}))
}

func TestWebhookDefaultURL(t *testing.T) {
	cap := &captured{}
	srv := newTestServer(t, 200, cap)
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.calls != 1 {
		t.Fatalf("calls = %d, want 1", cap.calls)
	}
	if cap.lastReq != "POST" {
		t.Errorf("method = %q, want POST", cap.lastReq)
	}
	if cap.lastCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", cap.lastCT)
	}
	if cap.last.Symbol != "BTC-USD" || cap.last.Price != 51000 {
		t.Errorf("payload mismatch: %+v", cap.last)
	}
}

func TestWebhookPerRuleOverridesDefault(t *testing.T) {
	defaultCap := &captured{}
	defaultSrv := newTestServer(t, 200, defaultCap)
	defer defaultSrv.Close()

	overrideCap := &captured{}
	overrideSrv := newTestServer(t, 200, overrideCap)
	defer overrideSrv.Close()

	n := NewWebhookNotifier(defaultSrv.URL)
	if err := n.Notify(context.Background(), sampleAlert(overrideSrv.URL)); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	defaultCap.mu.Lock()
	defer defaultCap.mu.Unlock()
	overrideCap.mu.Lock()
	defer overrideCap.mu.Unlock()
	if defaultCap.calls != 0 {
		t.Errorf("default URL should be skipped when rule has webhooks, got %d calls", defaultCap.calls)
	}
	if overrideCap.calls != 1 {
		t.Errorf("override URL should have received 1 call, got %d", overrideCap.calls)
	}
}

func TestWebhookMultipleURLsAllAttempted(t *testing.T) {
	cap1 := &captured{}
	srv1 := newTestServer(t, 200, cap1)
	defer srv1.Close()
	cap2 := &captured{}
	srv2 := newTestServer(t, 200, cap2)
	defer srv2.Close()

	n := NewWebhookNotifier("")
	if err := n.Notify(context.Background(), sampleAlert(srv1.URL, srv2.URL)); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	cap1.mu.Lock()
	defer cap1.mu.Unlock()
	cap2.mu.Lock()
	defer cap2.mu.Unlock()
	if cap1.calls != 1 || cap2.calls != 1 {
		t.Errorf("both URLs should be called, got %d / %d", cap1.calls, cap2.calls)
	}
}

func TestWebhookNoURLIsNoop(t *testing.T) {
	n := NewWebhookNotifier("")
	if err := n.Notify(context.Background(), sampleAlert()); err != nil {
		t.Fatalf("Notify: expected nil for no-URL config, got %v", err)
	}
}

func TestWebhookNon2xxReturnsError(t *testing.T) {
	cap := &captured{}
	srv := newTestServer(t, 500, cap)
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	err := n.Notify(context.Background(), sampleAlert())
	if err == nil {
		t.Fatalf("expected error for 500 response, got nil")
	}
}

func TestWebhookOneFailDoesNotBlockOthers(t *testing.T) {
	failCap := &captured{}
	failSrv := newTestServer(t, 500, failCap)
	defer failSrv.Close()
	okCap := &captured{}
	okSrv := newTestServer(t, 200, okCap)
	defer okSrv.Close()

	n := NewWebhookNotifier("")
	err := n.Notify(context.Background(), sampleAlert(failSrv.URL, okSrv.URL))
	if err == nil {
		t.Fatalf("expected error from failing URL")
	}

	okCap.mu.Lock()
	defer okCap.mu.Unlock()
	if okCap.calls != 1 {
		t.Errorf("ok URL should still have been called, got %d", okCap.calls)
	}
}

func TestWebhookName(t *testing.T) {
	n := NewWebhookNotifier("")
	if got, want := n.Name(), "webhook"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

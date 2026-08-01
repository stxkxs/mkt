package yahoo

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
)

// TestMain neutralizes the package pacing for the suite: production waits
// seconds between retries and caps the request rate, neither of which a unit
// test should pay for. Tests that assert on pacing install their own gate or
// policy with swapPacing.
func TestMain(m *testing.M) {
	apiGate = newGate(math.Inf(1), 1)
	policy = fastPolicy

	// No test may reach the live Yahoo hosts. Parking every default endpoint
	// on a local stub keeps initSession instant for tests that only swap a
	// side endpoint (earnings, options) and turns a forgotten swap into a
	// decode error instead of a network call.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"crumb":"testcrumb"}`))
	}))
	baseURL = stub.URL
	chartURL = stub.URL + "/v8/finance/chart"
	sessionURL = stub.URL
	crumbURL = stub.URL

	code := m.Run()
	stub.Close()
	os.Exit(code)
}

// fastPolicy is the production schedule with every delay collapsed.
var fastPolicy = retryPolicy{
	attempts: 3,
	base:     time.Millisecond,
	max:      5 * time.Millisecond,
	cooldown: 2 * time.Millisecond,
	ceiling:  20 * time.Millisecond,
}

// swapPacing installs a gate and retry policy for one test and restores the
// suite defaults afterwards.
func swapPacing(t *testing.T, g *gate, pol retryPolicy) {
	t.Helper()
	og, op := apiGate, policy
	apiGate, policy = g, pol
	t.Cleanup(func() { apiGate, policy = og, op })
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"0", 0},
		{"-3", 0},
		{"garbage", 0},
		{"Sun, 01 Mar 2026 12:00:30 GMT", 30 * time.Second},
		{"Sun, 01 Mar 2026 11:59:30 GMT", 0}, // already past
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.in, now); got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGetJSONHonorsRetryAfterOn429(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Negligible backoff and a generous ceiling: the delay under test is the
	// one the Retry-After header installs, not the exponential step.
	pol := fastPolicy
	pol.ceiling = time.Minute
	swapPacing(t, newGate(math.Inf(1), 1), pol)

	p := New(time.Second)
	var out struct {
		OK bool `json:"ok"`
	}
	start := time.Now()
	if err := p.getJSON(context.Background(), srv.URL, nil, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	elapsed := time.Since(start)
	if !out.OK {
		t.Error("body not decoded after retry")
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want 2", hits.Load())
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("retried after %v, want >= ~1s from Retry-After", elapsed)
	}
	if rateLimited.Value() == 0 {
		t.Error("429 not counted")
	}
}

func TestGetJSONNoRetryOnAuthError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := New(time.Second)
	var out map[string]any
	err := p.getJSON(context.Background(), srv.URL, nil, &out)
	var se *httpx.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusUnauthorized {
		t.Fatalf("err = %v, want 401 StatusError", err)
	}
	if hits.Load() != 1 {
		t.Errorf("requests = %d, want 1 — 401 must not be retried", hits.Load())
	}
}

func TestGetJSONRetriesServerErrorsUpToPolicy(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	p := New(time.Second)
	var out map[string]any
	if err := p.getJSON(context.Background(), srv.URL, nil, &out); err == nil {
		t.Fatal("expected error")
	}
	if int(hits.Load()) != policy.attempts {
		t.Errorf("requests = %d, want %d", hits.Load(), policy.attempts)
	}
}

func TestGateCooldownBlocksEveryCallSite(t *testing.T) {
	pol := fastPolicy
	pol.ceiling = time.Hour
	swapPacing(t, newGate(math.Inf(1), 1), pol)

	g := newGate(math.Inf(1), 1)
	g.penalize(150 * time.Millisecond)
	start := time.Now()
	if err := g.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if d := time.Since(start); d < 120*time.Millisecond {
		t.Errorf("cooldown waited %v, want >= ~150ms", d)
	}
	// A shorter penalty must not shorten a pending cooldown.
	g.penalize(10 * time.Minute)
	g.penalize(time.Millisecond)
	g.mu.Lock()
	until := g.until
	g.mu.Unlock()
	if time.Until(until) < 5*time.Minute {
		t.Errorf("cooldown shortened to %v", time.Until(until))
	}
	// …and nothing parks the package past the ceiling.
	pol.ceiling = time.Second
	policy = pol
	g2 := newGate(math.Inf(1), 1)
	g2.penalize(time.Hour)
	g2.mu.Lock()
	capped := time.Until(g2.until)
	g2.mu.Unlock()
	if capped > time.Second {
		t.Errorf("cooldown of %v exceeds the ceiling", capped)
	}
}

func TestGateRateLimits(t *testing.T) {
	g := newGate(50, 1) // 50 rps, burst 1 → ~20ms between requests
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := g.wait(ctx); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}
	if d := time.Since(start); d < 30*time.Millisecond {
		t.Errorf("3 requests took %v, want >= ~40ms at 50 rps", d)
	}
}

func TestGateWaitRespectsContext(t *testing.T) {
	g := newGate(math.Inf(1), 1)
	g.penalize(time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g.wait(ctx); err == nil {
		t.Fatal("expected context error during cooldown")
	}
}

func TestHealthTransitions(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := New(time.Second)
	if !p.Healthy() {
		t.Fatal("provider should start healthy")
	}
	var out map[string]any
	for i := 0; i < unhealthyAfter; i++ {
		_ = p.getJSON(context.Background(), srv.URL, nil, &out)
	}
	if p.Healthy() {
		t.Fatalf("provider still healthy after %d failed requests", unhealthyAfter)
	}
	if p.LastError() == nil {
		t.Error("LastError should report why the provider is down")
	}
	select {
	case up := <-p.StatusChan():
		if up {
			t.Error("status channel reported up, want down")
		}
	default:
		t.Error("no status transition published")
	}

	fail.Store(false)
	if err := p.getJSON(context.Background(), srv.URL, nil, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if !p.Healthy() || p.LastError() != nil {
		t.Errorf("provider did not recover: healthy=%v err=%v", p.Healthy(), p.LastError())
	}
	select {
	case up := <-p.StatusChan():
		if !up {
			t.Error("status channel reported down, want up")
		}
	default:
		t.Error("no recovery transition published")
	}
}

func TestHealthIgnoresNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := New(time.Second)
	var out map[string]any
	for i := 0; i < unhealthyAfter+2; i++ {
		_ = p.getJSON(context.Background(), srv.URL, nil, &out)
	}
	if !p.Healthy() {
		t.Error("a 404 on one symbol must not mark the whole provider down")
	}
}

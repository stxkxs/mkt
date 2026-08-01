package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
	"github.com/stxkxs/mkt/internal/observe"
	"golang.org/x/time/rate"
)

// Pacing counters surfaced on /metrics.
var (
	rateLimited = observe.NewCounter("mkt_provider_yahoo_rate_limited_total")
	retries     = observe.NewCounter("mkt_provider_yahoo_retries_total")
)

// Request pacing against Yahoo's public, unauthenticated endpoints.
//
// Yahoo does not publish a quota; empirically a browser session sustains a
// few requests per second before the edge starts answering 429. The budget
// below is deliberately conservative because every mkt process shares one
// source IP with whatever else the user is running, and because a 429 storm
// is self-sustaining: the fallback path used to turn one failed 50-symbol
// batch into 50 chart requests, which earned more 429s, which triggered more
// fallbacks. Pacing is package-level (not per-Provider) because Yahoo limits
// by client, and mkt creates several Providers (dashboard, MCP, daemon).
const (
	// requestsPerSecond is the sustained budget across every Yahoo call
	// site in this package.
	requestsPerSecond = 4
	// requestBurst allows a poll cycle's worth of batches (three 50-symbol
	// batches covers the ~150-symbol default watchlist) to go out back to
	// back without waiting, while still capping the standing burst.
	requestBurst = 8
	// maxAttempts bounds retries of a single request. Four attempts spans
	// roughly 5s of backoff — long enough to ride out a transient edge
	// hiccup, short enough that a poll cycle never outlives its interval.
	maxAttempts = 4
	// retryBaseDelay is the first backoff step; it doubles per attempt.
	retryBaseDelay = 750 * time.Millisecond
	// retryMaxDelay caps a single backoff step.
	retryMaxDelay = 20 * time.Second
	// retryJitter randomizes each backoff by ±this fraction so concurrent
	// requests (macro fan-out, MCP) don't retry in lockstep.
	retryJitter = 0.3
	// cooldownDefault is the package-wide pause applied after a 429 that
	// carries no Retry-After header. Doubling per attempt.
	cooldownDefault = 5 * time.Second
	// cooldownMax caps how long a Retry-After can park the whole package.
	// Yahoo occasionally returns hour-scale values; honoring those verbatim
	// would look like a hang, so we retry sooner and accept another 429.
	cooldownMax = 2 * time.Minute
	// unhealthyAfter is how many consecutive fully-retried failures flip the
	// provider to unhealthy. Two avoids flapping on a single blip while
	// still surfacing a real outage within a couple of poll cycles.
	unhealthyAfter = 2
)

// retryPolicy is the active backoff and cooldown schedule. It is a var, not
// a const set, only so tests can collapse the delays; production uses the
// constants above.
type retryPolicy struct {
	attempts int
	base     time.Duration // first backoff step, doubled per attempt
	max      time.Duration // cap on one backoff step
	cooldown time.Duration // package-wide pause after a 429 with no Retry-After
	ceiling  time.Duration // cap on any cooldown, Retry-After included
}

var policy = retryPolicy{
	attempts: maxAttempts,
	base:     retryBaseDelay,
	max:      retryMaxDelay,
	cooldown: cooldownDefault,
	ceiling:  cooldownMax,
}

// gate paces every outbound Yahoo request: a token bucket for the sustained
// rate plus a cooldown deadline installed when the edge answers 429. Both are
// package-wide so one call site's 429 slows all of them down.
type gate struct {
	lim *rate.Limiter

	mu    sync.Mutex
	until time.Time
}

// apiGate is the package-level limiter shared by every Yahoo call site.
var apiGate = newGate(requestsPerSecond, requestBurst)

func newGate(rps float64, burst int) *gate {
	return &gate{lim: rate.NewLimiter(rate.Limit(rps), burst)}
}

// wait blocks until the cooldown (if any) has elapsed and a token is
// available, or ctx is done.
func (g *gate) wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		until := g.until
		g.mu.Unlock()
		d := time.Until(until)
		if d <= 0 {
			break
		}
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return g.lim.Wait(ctx)
}

// penalize parks every call site for d (capped at policy.ceiling). A pending
// cooldown is extended, never shortened.
func (g *gate) penalize(d time.Duration) {
	if d <= 0 {
		return
	}
	if d > policy.ceiling {
		d = policy.ceiling
	}
	deadline := time.Now().Add(d)
	g.mu.Lock()
	if deadline.After(g.until) {
		g.until = deadline
	}
	g.mu.Unlock()
}

// getJSON issues a rate-limited, retrying GET and decodes a 2xx JSON body
// into out. Retries cover transport errors, 429 and 5xx; 4xx responses
// (including the 401/403 that invalidate the crumb) fail immediately so the
// caller can react. Every Yahoo request in this package goes through here.
func (p *Provider) getJSON(ctx context.Context, endpoint string, headers map[string]string, out any) error {
	var lastErr error
	for attempt := 1; attempt <= policy.attempts; attempt++ {
		if attempt > 1 {
			retries.Inc()
			if err := sleepCtx(ctx, backoffDelay(attempt-1)); err != nil {
				return err
			}
		}
		if err := apiGate.wait(ctx); err != nil {
			return err
		}

		body, retryAfter, err := p.get(ctx, endpoint, headers)
		if err == nil {
			if err := json.Unmarshal(body, out); err != nil {
				// A malformed body is not an outage; don't touch health.
				return fmt.Errorf("decode json: %w", err)
			}
			p.markHealthy()
			return nil
		}
		lastErr = err

		if isRateLimited(err) {
			rateLimited.Inc()
			if retryAfter <= 0 {
				retryAfter = policy.cooldown << (attempt - 1)
			}
			apiGate.penalize(retryAfter)
		}
		if !retryable(err) {
			break
		}
	}
	if retryable(lastErr) {
		p.markFailure(lastErr)
	}
	return lastErr
}

// get issues a GET and returns the body capped at httpx.MaxResponseBytes.
// It mirrors httpx.Get — same *httpx.StatusError contract — but also
// surfaces the Retry-After header, which httpx does not expose and which the
// 429 handling above needs.
func (p *Provider) get(ctx context.Context, endpoint string, headers map[string]string) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpx.MaxResponseBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			&httpx.StatusError{Code: resp.StatusCode, Body: truncate(string(body), 256)}
	}
	return body, 0, nil
}

// parseRetryAfter decodes a Retry-After header in either of its RFC 9110
// forms — delta-seconds or an HTTP-date — relative to now. Returns 0 when
// absent or unparseable.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// backoffDelay returns the jittered delay before retry number n (1-based).
func backoffDelay(n int) time.Duration {
	d := policy.base << (n - 1)
	if d > policy.max || d <= 0 {
		d = policy.max
	}
	delta := float64(d) * retryJitter
	return d + time.Duration((rand.Float64()*2-1)*delta)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryable reports whether err is worth another attempt: transport errors
// and the codes that mean "later, not never" (429, 5xx). Context
// cancellation never is.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *httpx.StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	return true
}

func isRateLimited(err error) bool {
	var se *httpx.StatusError
	return errors.As(err, &se) && se.Code == http.StatusTooManyRequests
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Healthy reports whether Yahoo is currently reachable. It goes false after
// unhealthyAfter consecutive requests exhaust their retries against a
// transport error, a 429, or a 5xx, and back true on the next success. A
// per-symbol 4xx (bad ticker) does not affect it. Providers start healthy —
// an outage is only claimed once observed.
func (p *Provider) Healthy() bool {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	return p.healthy
}

// LastError returns the error that most recently made the provider
// unhealthy, or nil while healthy. Intended for an outage banner.
func (p *Provider) LastError() error {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	if p.healthy {
		return nil
	}
	return p.lastErr
}

// StatusChan returns a channel that receives health transitions (true =
// reachable), mirroring coinbase's StatusChan so the TUI can consume both
// providers the same way. The channel is buffered by one and lossy: a
// transition is dropped if nobody is reading, so the current state must be
// read from Healthy rather than reconstructed from the stream.
func (p *Provider) StatusChan() <-chan bool {
	return p.statusCh
}

// markHealthy records a successful request and reports recovery.
func (p *Provider) markHealthy() {
	p.healthMu.Lock()
	p.failures = 0
	changed := !p.healthy
	p.healthy = true
	p.lastErr = nil
	p.healthMu.Unlock()
	if changed {
		p.notifyStatus(true)
	}
}

// markFailure records a request that exhausted its retries and flips the
// provider unhealthy once unhealthyAfter of them pile up.
func (p *Provider) markFailure(err error) {
	p.healthMu.Lock()
	p.failures++
	p.lastErr = err
	changed := p.healthy && p.failures >= unhealthyAfter
	if changed {
		p.healthy = false
	}
	p.healthMu.Unlock()
	if changed {
		p.notifyStatus(false)
	}
}

func (p *Provider) notifyStatus(up bool) {
	select {
	case p.statusCh <- up:
	default:
	}
}

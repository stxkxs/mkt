// Package httpx centralizes the GET → check-status → read-capped-body →
// decode pattern that every HTTP provider (yahoo, coinbase REST, binance,
// defillama, news) was hand-rolling. Beyond removing that duplication it
// caps the response body with an io.LimitReader so a hostile or
// compromised upstream can't stream an unbounded body into memory.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/stxkxs/mkt/internal/textsafe"
)

// MaxResponseBytes bounds how much of a response body we will read. All of
// the JSON/XML/CSV endpoints mkt talks to fit comfortably; anything larger
// is treated as hostile and truncated.
const MaxResponseBytes = 16 << 20 // 16 MiB

// StatusError reports a non-2xx HTTP response. Callers that need to react
// to a specific code (e.g. Yahoo's 401/403 → reset crumb) can recover it
// with errors.As.
type StatusError struct {
	Code int
	Body string // truncated snippet, for diagnostics
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http status %d", e.Code)
	}
	return fmt.Sprintf("http status %d: %s", e.Code, e.Body)
}

// Get issues a GET with the given headers using client (which must carry
// its own timeout) and returns the response body capped at
// MaxResponseBytes. A non-2xx response yields a *StatusError.
func Get(ctx context.Context, client *http.Client, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode, Body: errorSnippet(string(body))}
	}
	return body, nil
}

// Post issues a POST with the given headers using client (which must carry
// its own timeout), drains and discards the response body, and reports a
// non-2xx response as a *StatusError.
//
// The body is drained rather than ignored so the connection returns to the
// pool: every caller here is a long-lived process sending on a schedule.
// Callers that must keep a destination out of their errors wrap the result
// themselves — the URL is the credential for a webhook, and this package
// cannot tell which caller that applies to.
func Post(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Code: resp.StatusCode}
	}
	return nil
}

// GetJSON issues a GET and decodes a 2xx JSON body into out.
func GetJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, out any) error {
	body, err := Get(ctx, client, url, headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// errorSnippetRunes bounds the upstream text carried in a StatusError.
// Enough to identify the failure, short enough that a body cannot flood a
// log line or a terminal row.
const errorSnippetRunes = 256

// errorSnippet prepares an upstream response body to travel inside an
// error. StatusError.Error interpolates it verbatim, and the result reaches
// a Bubbletea frame (the Options and Symbol Info tabs render fetch errors)
// and MCP tool output, so the body is sanitized here rather than at each
// renderer — a control character that survives this point has several ways
// to reach a terminal and only one place to be caught.
func errorSnippet(body string) string {
	return textsafe.Truncate(textsafe.Clean(body), errorSnippetRunes)
}

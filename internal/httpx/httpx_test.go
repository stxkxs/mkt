package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func testClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func TestGetJSON_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Errorf("header not forwarded: %q", got)
		}
		w.Write([]byte(`{"name":"eth","tvl":42.5}`))
	}))
	defer srv.Close()

	var out struct {
		Name string  `json:"name"`
		TVL  float64 `json:"tvl"`
	}
	err := GetJSON(context.Background(), testClient(), srv.URL, map[string]string{"X-Test": "yes"}, &out)
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if out.Name != "eth" || out.TVL != 42.5 {
		t.Fatalf("decoded %+v", out)
	}
}

func TestGet_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("nope"))
	}))
	defer srv.Close()

	_, err := Get(context.Background(), testClient(), srv.URL, nil)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want *StatusError, got %v", err)
	}
	if se.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", se.Code)
	}
}

func TestGet_CapsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stream more than the cap; Get must stop reading at MaxResponseBytes.
		chunk := strings.Repeat("a", 1<<20)
		for i := 0; i < 20; i++ { // 20 MiB > 16 MiB cap
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	body, err := Get(context.Background(), testClient(), srv.URL, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(body) > MaxResponseBytes {
		t.Fatalf("body not capped: %d > %d", len(body), MaxResponseBytes)
	}
}

// An upstream error body is relayed verbatim into StatusError.Error, which
// the Options and Symbol Info tabs render into a Bubbletea frame and the
// MCP server returns as tool output. Unlike the XML feed paths, nothing
// parses this body first, so a control character here reaches a terminal.
func TestStatusErrorBodyIsSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down\x1b[2J\x1b[H\x1b[31mSELL EVERYTHING\x1b[0m\u202egnitset"))
	}))
	defer srv.Close()

	_, err := Get(context.Background(), srv.Client(), srv.URL, nil)
	if err == nil {
		t.Fatal("429 should yield an error")
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want *StatusError, got %T", err)
	}
	for _, r := range se.Error() {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("U+%04X survived into the rendered error: %q", r, se.Error())
		}
	}
	if !strings.Contains(se.Body, "slow down") {
		t.Fatalf("sanitizing dropped the diagnostic text: %q", se.Body)
	}
}

// A body far past the snippet cap must not flood a terminal row or a log
// line, and truncation must not split a multi-byte rune.
func TestStatusErrorBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("日", 5000)))
	}))
	defer srv.Close()

	_, err := Get(context.Background(), srv.Client(), srv.URL, nil)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want *StatusError, got %T", err)
	}
	if n := utf8.RuneCountInString(se.Body); n > errorSnippetRunes+1 {
		t.Fatalf("snippet is %d runes, want <= %d", n, errorSnippetRunes+1)
	}
	if !utf8.ValidString(se.Body) {
		t.Fatalf("truncation split a rune: %q", se.Body)
	}
}

func TestGetRecordsDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	before, beforeSum := getDuration.Count(), getDuration.Sum()
	if _, err := Get(context.Background(), testClient(), srv.URL, nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := getDuration.Count() - before; got != 1 {
		t.Errorf("observations recorded by one Get: %d, want 1", got)
	}
	if getDuration.Sum() < beforeSum {
		t.Errorf("duration sum went backwards: %v → %v", beforeSum, getDuration.Sum())
	}
}

func TestGetRecordsDurationOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	before := getDuration.Count()
	if _, err := Get(context.Background(), testClient(), srv.URL, nil); err == nil {
		t.Fatal("Get against a 500 returned no error")
	}
	if got := getDuration.Count() - before; got != 1 {
		t.Errorf("observations recorded by one failed Get: %d, want 1", got)
	}
}

func TestPostRecordsDuration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	before := postDuration.Count()
	if err := Post(context.Background(), testClient(), srv.URL, nil, []byte("x")); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if got := postDuration.Count() - before; got != 1 {
		t.Errorf("observations recorded by one Post: %d, want 1", got)
	}
}

func TestDurationSeriesNamesAreStable(t *testing.T) {
	// These names are documented in the README's metrics table and scraped
	// by Prometheus; renaming one silently breaks every dashboard reading it.
	if got := getDuration.Name(); got != "mkt_http_fetch_duration_seconds" {
		t.Errorf("fetch histogram name = %q", got)
	}
	if got := postDuration.Name(); got != "mkt_http_post_duration_seconds" {
		t.Errorf("post histogram name = %q", got)
	}
}

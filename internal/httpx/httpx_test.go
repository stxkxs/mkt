package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

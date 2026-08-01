package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// slackSecret is the shape of a real Slack incoming-webhook path. The path
// IS the credential: anyone holding it can post to the workspace.
const slackSecret = "T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"

func TestRedactURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"slack webhook", "https://hooks.slack.com/services/" + slackSecret, "https://hooks.slack.com/…"},
		{"query secret", "https://example.com/hook?token=s3cr3t", "https://example.com/…"},
		{"fragment secret", "https://example.com#s3cr3t", "https://example.com/…"},
		{"userinfo", "https://user:pass@example.com/x", "https://example.com/…"},
		{"host only", "https://ntfy.sh", "https://ntfy.sh"},
		{"trailing slash", "https://ntfy.sh/", "https://ntfy.sh"},
		{"ntfy topic", "https://ntfy.sh/my-secret-topic", "https://ntfy.sh/…"},
		{"with port", "http://127.0.0.1:8080/hook", "http://127.0.0.1:8080/…"},
		{"not a url", "definitely-not-a-url", redactedPlaceholder},
		{"empty", "", redactedPlaceholder},
		{"control chars", "http://\x7f/x", redactedPlaceholder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURL(tc.in); got != tc.want {
				t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWebhookErrorsNeverLeakTheSecretPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dest := srv.URL + "/services/" + slackSecret
	n := NewWebhookNotifier(dest)
	err := n.Notify(context.Background(), sampleAlert())
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	assertNoSecret(t, err.Error())
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("error should still name the host for debugging, got %q", err)
	}
}

func TestWebhookTransportErrorsNeverLeakTheSecretPath(t *testing.T) {
	// A closed listener: the http client returns a *url.Error whose Error
	// string embeds the full request URL.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dest := srv.URL + "/services/" + slackSecret
	srv.Close()

	n := NewWebhookNotifier(dest)
	err := n.Notify(context.Background(), sampleAlert())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	assertNoSecret(t, err.Error())
}

func TestNtfyErrorsNeverLeakTheTopic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewNtfyNotifier(srv.URL, slackSecret)
	err := n.Notify(context.Background(), sampleAlert())
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	assertNoSecret(t, err.Error())
}

func TestNtfyTransportErrorsNeverLeakTheTopic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()

	n := NewNtfyNotifier(base, slackSecret)
	err := n.Notify(context.Background(), sampleAlert())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	assertNoSecret(t, err.Error())
}

// assertNoSecret fails when any component of a known-secret path survives
// into text that gets logged.
func assertNoSecret(t *testing.T, text string) {
	t.Helper()
	for _, seg := range strings.Split(slackSecret, "/") {
		if strings.Contains(text, seg) {
			t.Fatalf("secret segment %q leaked into %q", seg, text)
		}
	}
	if strings.Contains(text, "services") {
		t.Fatalf("secret path leaked into %q", text)
	}
}

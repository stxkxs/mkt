package alert

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/stxkxs/mkt/internal/httpx"
)

const defaultPushoverEndpoint = "https://api.pushover.net/1/messages.json"

// PushoverNotifier delivers triggered alerts to a Pushover user via the
// Pushover messages API. Requires an application token and the user's
// (or group's) key.
type PushoverNotifier struct {
	user     string
	token    string
	endpoint string
	client   *http.Client
}

// NewPushoverNotifier returns a Notifier for Pushover. The endpoint
// defaults to the production messages API; tests override it.
func NewPushoverNotifier(user, token string) *PushoverNotifier {
	return &PushoverNotifier{
		user:     user,
		token:    token,
		endpoint: defaultPushoverEndpoint,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

// Name implements Notifier.
func (p *PushoverNotifier) Name() string { return "pushover" }

// Notify implements Notifier. No-ops when user or token is empty.
func (p *PushoverNotifier) Notify(ctx context.Context, a TriggeredAlert) error {
	if p.user == "" || p.token == "" {
		return nil
	}
	form := url.Values{}
	form.Set("token", p.token)
	form.Set("user", p.user)
	form.Set("title", "mkt Alert: "+a.Rule.Symbol)
	form.Set("message", a.Message)

	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	// *url.Error embeds the request URL verbatim. The Pushover endpoint is
	// a constant and the token rides in the form body, so nothing leaks
	// today; redacting keeps the three notifier error paths identical, so
	// making the endpoint configurable cannot quietly turn this into a
	// token in the logs.
	if err := httpx.Post(ctx, p.client, p.endpoint, headers, []byte(form.Encode())); err != nil {
		return fmt.Errorf("pushover: %w", redactErr(err))
	}
	return nil
}

package alert

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("pushover: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pushover: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pushover: status %d", resp.StatusCode)
	}
	return nil
}

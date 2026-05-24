package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookNotifier posts triggered alerts as JSON to configured URLs.
// Each TriggeredAlert can route to its rule's Webhooks list; otherwise
// the notifier falls back to its default URL. With neither set the
// notifier silently no-ops.
type WebhookNotifier struct {
	defaultURL string
	client     *http.Client
}

// NewWebhookNotifier returns a Notifier that posts to defaultURL when a
// rule has no Webhooks. If defaultURL is empty and a rule also lacks
// webhooks, Notify is a no-op.
func NewWebhookNotifier(defaultURL string) *WebhookNotifier {
	return &WebhookNotifier{
		defaultURL: defaultURL,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

// Name implements Notifier.
func (w *WebhookNotifier) Name() string { return "webhook" }

type webhookPayload struct {
	Symbol    string    `json:"symbol"`
	Condition string    `json:"condition"`
	Value     float64   `json:"value"`
	Price     float64   `json:"price"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// Notify implements Notifier. Posts to each resolved URL in order;
// returns the first error encountered but always attempts every URL.
func (w *WebhookNotifier) Notify(ctx context.Context, a TriggeredAlert) error {
	urls := a.Rule.Webhooks
	if len(urls) == 0 && w.defaultURL != "" {
		urls = []string{w.defaultURL}
	}
	if len(urls) == 0 {
		return nil
	}

	payload := webhookPayload{
		Symbol:    a.Rule.Symbol,
		Condition: string(a.Rule.Condition),
		Value:     a.Rule.Value,
		Price:     a.Price,
		Message:   a.Message,
		Timestamp: a.Timestamp,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	var firstErr error
	for _, url := range urls {
		if err := w.post(ctx, url, body); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (w *WebhookNotifier) post(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook %s: build request: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s: status %d", url, resp.StatusCode)
	}
	return nil
}

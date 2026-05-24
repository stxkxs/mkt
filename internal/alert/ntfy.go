package alert

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultNtfyServer = "https://ntfy.sh"

// NtfyNotifier delivers triggered alerts to an ntfy.sh topic. No
// credentials are required — anyone with the topic name can publish
// or subscribe, so users should pick a hard-to-guess topic.
type NtfyNotifier struct {
	server string
	topic  string
	client *http.Client
}

// NewNtfyNotifier returns a Notifier for ntfy.sh. An empty server
// defaults to https://ntfy.sh.
func NewNtfyNotifier(server, topic string) *NtfyNotifier {
	if server == "" {
		server = defaultNtfyServer
	}
	server = strings.TrimRight(server, "/")
	return &NtfyNotifier{
		server: server,
		topic:  topic,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Name implements Notifier.
func (n *NtfyNotifier) Name() string { return "ntfy" }

// Notify implements Notifier. No-ops when topic is empty.
func (n *NtfyNotifier) Notify(ctx context.Context, a TriggeredAlert) error {
	if n.topic == "" {
		return nil
	}
	url := n.server + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(a.Message)))
	if err != nil {
		return fmt.Errorf("ntfy: build request: %w", err)
	}
	req.Header.Set("Title", "mkt Alert: "+a.Rule.Symbol)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy %s: status %d", url, resp.StatusCode)
	}
	return nil
}

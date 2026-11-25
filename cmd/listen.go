package cmd

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// listenGate decides whether the read API may start on addr with token. It
// holds every refusal the --listen surface makes before binding:
//   - --require-token with no token;
//   - --enable-webhook with no token, on any address, because the webhook
//     injects alerts into the notifier fan-out;
//   - a non-loopback bind with no token (checkListenSafety).
func listenGate(addr, token string, opts backendOpts) error {
	if opts.requireToken && token == "" {
		return fmt.Errorf("--require-token set but --listen-token is empty")
	}
	if opts.enableWebhook && token == "" {
		return fmt.Errorf("--enable-webhook requires --listen-token: the inbound webhook injects alerts into the notifier fan-out and must never be unauthenticated, even on loopback")
	}
	return checkListenSafety(addr, token)
}

// checkListenSafety fails closed on an unsafe HTTP bind. A non-loopback
// address (which includes the all-interfaces form ":9999" / 0.0.0.0) with
// no token would expose /webhook/tradingview — which injects alerts that
// fan out to desktop / push / webhook destinations — and /alerts, which
// leaks configured destinations, to anyone reachable. Only an explicit
// loopback host is allowed without a token; everything else must set
// --listen-token. Returns an error the caller surfaces to refuse startup:
// an unsafe bind must fail closed, not warn and serve.
func checkListenSafety(addr, token string) error {
	if token != "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = "" // unparseable → treat as non-loopback and require a token
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	return fmt.Errorf("refusing to bind --listen %s without a listen token: "+
		"the TradingView webhook can inject alerts and /alerts leaks configured "+
		"destinations to anyone reachable. Bind to 127.0.0.1:<port> for local use, "+
		"or set MKT_LISTEN_TOKEN / --listen-token-file to serve on %s", addr, addr)
}

// listenTokenFromSources returns the first token found in the out-of-band
// sources, or "" when none is configured.
func listenTokenFromSources(cmd *cobra.Command) (string, error) {
	path, _ := cmd.Flags().GetString("listen-token-file")
	from := "--listen-token-file"
	if path == "" {
		path, from = os.Getenv(EnvListenTokenFile), EnvListenTokenFile
	}
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read listen token from %s (%s): %w", path, from, err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("listen token file %s (%s) is empty", path, from)
		}
		return token, nil
	}
	return strings.TrimSpace(os.Getenv(EnvListenToken)), nil
}

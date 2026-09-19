package alert

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// notifyHelperTimeout bounds one desktop helper process even when the caller
// hands over a context with no deadline. A helper blocked on a stalled
// notification daemon holds the desktop destination's single delivery
// goroutine, and every later trigger queues behind it.
const notifyHelperTimeout = 5 * time.Second

// helperWaitDelay bounds the wait for the helper's stderr pipe once the
// deadline has killed the helper itself. A helper that leaves a child
// holding the write end never produces the EOF the copy expects, so this
// is what bounds the call and not merely the process.
const helperWaitDelay = time.Second

// helperStderrLimit caps how much of a failed helper's stderr reaches the
// wrapped error. The helper chooses how much it writes and the result is
// logged, so the bound belongs here.
const helperStderrLimit = 256

// Notifier delivers a triggered alert to a destination such as a desktop
// notification, webhook, or mobile push service. Implementations should
// respect the context deadline. The engine logs and isolates errors so
// one failing destination cannot block the others.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, a TriggeredAlert) error
}

// DesktopNotifier emits a terminal bell and hands the alert to the host's
// desktop notification helper. A host with no helper installed returns an
// error the engine logs; the terminal bell and the other destinations still
// carry the alert.
type DesktopNotifier struct{}

// NewDesktopNotifier returns a Notifier for the local desktop.
func NewDesktopNotifier() DesktopNotifier {
	return DesktopNotifier{}
}

// Name implements Notifier.
func (DesktopNotifier) Name() string { return "desktop" }

// Notify implements Notifier.
func (DesktopNotifier) Notify(ctx context.Context, a TriggeredAlert) error {
	fmt.Print("\a")
	title := fmt.Sprintf("mkt Alert: %s", a.Rule.Symbol)
	return deliverDesktop(ctx, title, a.Message)
}

// runNotifyHelper runs a desktop notification helper to completion under a
// bounded deadline. Callers pass argv elements and never a shell word, so no
// symbol or alert message reaching this function can become a command.
func runNotifyHelper(ctx context.Context, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, notifyHelperTimeout)
	defer cancel()

	var stderr bytes.Buffer
	// #nosec G204 -- name is a per-platform constant and every element of
	// args is an argv element, so no interpolated value is parsed by a shell.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	cmd.WaitDelay = helperWaitDelay
	if err := cmd.Run(); err != nil {
		if detail := trimHelperOutput(stderr.String()); detail != "" {
			return fmt.Errorf("%s: %w: %s", name, err, detail)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// trimHelperOutput reduces helper output to one bounded, valid-UTF-8 line
// suitable for an error message.
func trimHelperOutput(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > helperStderrLimit {
		s = strings.ToValidUTF8(s[:helperStderrLimit], "")
	}
	return s
}

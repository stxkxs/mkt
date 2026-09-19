package alert

import "context"

// deliverDesktop posts a Linux notification through notify-send, the
// libnotify client every desktop notification daemon accepts.
func deliverDesktop(ctx context.Context, title, message string) error {
	return runNotifyHelper(ctx, "notify-send", notifySendArgs(title, message)...)
}

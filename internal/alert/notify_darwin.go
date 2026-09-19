package alert

import "context"

// deliverDesktop posts a macOS notification through osascript, the one
// notification entry point that does not require a bundled application.
func deliverDesktop(ctx context.Context, title, message string) error {
	return runNotifyHelper(ctx, "osascript", appleScriptNotifyArgs(title, message)...)
}

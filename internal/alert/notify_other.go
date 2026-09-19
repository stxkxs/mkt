//go:build !darwin && !linux && !windows

package alert

import "context"

// deliverDesktop is a no-op where the platform has no desktop notification
// service. The terminal bell still carries the alert, and an error here
// would be logged once per trigger on a host that can never deliver one.
func deliverDesktop(_ context.Context, _, _ string) error { return nil }

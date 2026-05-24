package alert

import (
	"context"
	"fmt"

	"github.com/gen2brain/beeep"
)

// Notifier delivers a triggered alert to a destination such as a desktop
// notification, webhook, or mobile push service. Implementations should
// respect the context deadline. The engine logs and isolates errors so
// one failing destination cannot block the others.
type Notifier interface {
	Name() string
	Notify(ctx context.Context, a TriggeredAlert) error
}

// DesktopNotifier emits a terminal bell and a system notification via the
// beeep library. The context is accepted to satisfy Notifier but beeep
// itself is synchronous and cannot be cancelled.
type DesktopNotifier struct{}

// NewDesktopNotifier returns a Notifier for the local desktop.
func NewDesktopNotifier() DesktopNotifier {
	return DesktopNotifier{}
}

// Name implements Notifier.
func (DesktopNotifier) Name() string { return "desktop" }

// Notify implements Notifier.
func (DesktopNotifier) Notify(_ context.Context, a TriggeredAlert) error {
	fmt.Print("\a")
	title := fmt.Sprintf("mkt Alert: %s", a.Rule.Symbol)
	return beeep.Notify(title, a.Message, "")
}

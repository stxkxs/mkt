package alert

import (
	"fmt"

	"github.com/gen2brain/beeep"
)

// Notify sends a desktop notification and terminal bell.
func Notify(a TriggeredAlert) {
	// Terminal bell
	fmt.Print("\a")

	// Desktop notification via beeep
	title := fmt.Sprintf("mkt Alert: %s", a.Rule.Symbol)
	_ = beeep.Notify(title, a.Message, "")
}

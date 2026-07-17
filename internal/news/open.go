package news

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
)

// openDisabled, when set, turns OpenURL into a no-op. `mkt serve` sets it so
// a remote SSH user pressing Enter on a headline can't spawn a browser on
// the *server* host (a remote-triggered process-spawn primitive).
var openDisabled atomic.Bool

// SetOpenDisabled toggles host-side browser opening. Serve mode disables it.
func SetOpenDisabled(v bool) { openDisabled.Store(v) }

// OpenURL opens a URL in the default browser. URLs are restricted to http(s)
// to prevent feed-supplied links from being parsed as flags by open/xdg-open.
// A no-op when opening is disabled (serve mode).
func OpenURL(url string) error {
	if openDisabled.Load() {
		return nil
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open non-http(s) url: %q", url)
	}
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return exec.Command("open", "--", url).Start()
	}
}

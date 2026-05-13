package news

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenURL opens a URL in the default browser. URLs are restricted to http(s)
// to prevent feed-supplied links from being parsed as flags by open/xdg-open.
func OpenURL(url string) error {
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

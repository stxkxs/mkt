package news

import (
	"os/exec"
	"runtime"
)

// OpenURL opens a URL in the default browser.
func OpenURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return exec.Command("open", url).Start()
	}
}

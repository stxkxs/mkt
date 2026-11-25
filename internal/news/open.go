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
	name, args := opener(runtime.GOOS, url)
	return spawn(name, args...)
}

// opener returns the URL handler command for goos.
//
//   - macOS: open(1), with "--" so nothing after it is read as a flag.
//   - Windows: url.dll's FileProtocolHandler, which takes the URL as one
//     argument. `cmd /c start` would hand it to cmd.exe, where & and ^ in
//     a query string are command syntax.
//   - Linux and the BSDs: xdg-open.
func opener(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"--", url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// spawn runs the platform's URL handler. It is a variable so a test can
// assert what reaches argv, and that a refused URL reaches it not at all,
// without starting a process. The handler is waited for in the background
// so it does not linger as a zombie once it exits.
var spawn = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

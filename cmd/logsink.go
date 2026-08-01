package cmd

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/stxkxs/mkt/internal/config"
)

// logFileName is where the dashboard's log output goes while the TUI owns
// the terminal.
const logFileName = "mkt.log"

// captureLog redirects the standard logger away from stderr for as long as
// the TUI is on screen, and returns a function that restores it.
//
// Every provider, poller and notifier in the data plane runs on its own
// goroutine and reports failures through the standard logger. Bubbletea
// draws into the alternate screen and tracks what it believes each cell
// holds, so a stray write to stderr does not scroll — it lands in the middle
// of the frame and stays there until something redraws over it, which for a
// static panel may be never. A backfill that cannot reach its provider
// should not be able to shred the dashboard.
//
// Output goes to ~/.config/mkt/mkt.log (0600, truncated per run so it cannot
// grow without bound). Nothing is printed about it at startup — a healthy run
// logs nothing, and a line of chrome on every launch is worse than a file the
// docs point at. If the file cannot be opened the logger is silenced rather
// than left pointing at the screen: losing a log line is recoverable, a
// corrupted dashboard the user cannot read is not.
//
// `mkt serve` deliberately does not call this: there the process's stderr is
// the operator's console, not any session's terminal, so its logs belong on
// stderr where the operator can see them.
func captureLog() func() {
	prevOut := log.Writer()
	prevFlags := log.Flags()
	prevPrefix := log.Prefix()
	restore := func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
		log.SetPrefix(prevPrefix)
	}

	path := filepath.Join(config.ConfigDir(), logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.SetOutput(io.Discard)
		return restore
	}

	log.SetOutput(f)
	return func() {
		restore()
		_ = f.Close()
	}
}

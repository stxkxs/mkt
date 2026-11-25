// Package atomicfile moves a fully written temp file over its destination.
//
// A rename is the step that makes a write atomic, and it is the one step
// Windows can refuse for reasons outside the writer's control: while another
// process holds the destination open without FILE_SHARE_DELETE (an editor, a
// virus scanner, a reader polling the file), the rename fails with
// ERROR_ACCESS_DENIED or ERROR_SHARING_VIOLATION until that handle closes.
// Replace retries exactly those errors for a bounded time; on every other
// platform, and for every other error, the first result is returned.
package atomicfile

import (
	"os"
	"time"
)

// retryBudget bounds the total time Replace sleeps between attempts. Handles
// held by scanners and readers are released within milliseconds; a handle
// still open after this long belongs to something that will not let go, and
// the caller's error path is the right place to report it.
const retryBudget = 2 * time.Second

// maxDelay caps a single backoff step so a handle released early is noticed
// promptly.
const maxDelay = 100 * time.Millisecond

// Replace renames tmp over path, retrying while the platform reports the
// destination as transiently in use.
func Replace(tmp, path string) error {
	return replace(tmp, path, os.Rename, isTransient, time.Sleep, retryBudget)
}

func replace(
	tmp, path string,
	rename func(string, string) error,
	transient func(error) bool,
	sleep func(time.Duration),
	budget time.Duration,
) error {
	delay := time.Millisecond
	var slept time.Duration
	for {
		err := rename(tmp, path)
		if err == nil || !transient(err) || slept >= budget {
			return err
		}
		sleep(delay)
		slept += delay
		delay = min(2*delay, maxDelay)
	}
}

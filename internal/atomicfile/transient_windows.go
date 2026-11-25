//go:build windows

package atomicfile

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isTransient reports whether a rename failed because another process holds
// the destination open, which clears once that handle closes.
func isTransient(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

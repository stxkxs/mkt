//go:build !windows

package atomicfile

// isTransient reports whether a rename error clears on its own. POSIX rename
// replaces a destination regardless of open handles, so no error does.
func isTransient(error) bool { return false }

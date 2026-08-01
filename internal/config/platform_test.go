package config

import (
	"runtime"
	"testing"
)

// requireUnixPerms skips a test that asserts Unix mode bits.
//
// Windows has no POSIX permission model: Go's os.Chmod there only toggles the
// read-only attribute, so a file written 0600 reports 0666 and a directory
// created 0700 reports 0777. The 0600/0700 assertions in this package are
// real and load-bearing on Linux and macOS — the config holds holdings and
// webhook secrets — but on Windows they describe something the OS does not
// implement. Access control there is an ACL question, which is out of scope
// for these tests rather than something to fake.
func requireUnixPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("windows does not implement Unix permission bits (os.Chmod only toggles read-only)")
	}
}

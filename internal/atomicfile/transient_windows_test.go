//go:build windows

package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsTransientMatchesOnlyInUseErrors(t *testing.T) {
	wrap := func(errno windows.Errno) error {
		return fmt.Errorf("replace: %w", &os.LinkError{Op: "rename", Old: "a", New: "b", Err: errno})
	}
	cases := []struct {
		err  error
		want bool
	}{
		{wrap(windows.ERROR_ACCESS_DENIED), true},
		{wrap(windows.ERROR_SHARING_VIOLATION), true},
		{wrap(windows.ERROR_PATH_NOT_FOUND), false},
		{errors.New("plain"), false},
	}
	for _, c := range cases {
		if got := isTransient(c.err); got != c.want {
			t.Errorf("isTransient(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

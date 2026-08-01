package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stxkxs/mkt/internal/config"
)

// isolateHome redirects the config directory under home and returns it.
//
// Setting HOME alone is not portable: os.UserHomeDir reads $HOME on Unix but
// %USERPROFILE% on Windows, so a HOME-only test ran against the developer's
// real config directory there — which is how the Windows CI leg first failed,
// on state leaked between tests that all believed they were isolated.
// config.DirEnv is the explicit override; HOME and USERPROFILE are still set
// because unrelated paths (the SSH host-key default, ~ expansion, tildePath)
// resolve a home directory of their own.
func isolateHome(t *testing.T, home string) string {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".config", "mkt")
	t.Setenv(config.DirEnv, dir)
	return dir
}

// TestIsolateHomeRedirectsConfigDir pins the property every other test in
// this package depends on: after isolateHome, config.ConfigDir points inside
// the temp dir on every platform.
func TestIsolateHomeRedirectsConfigDir(t *testing.T) {
	home := t.TempDir()
	want := isolateHome(t, home)

	if got := config.ConfigDir(); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

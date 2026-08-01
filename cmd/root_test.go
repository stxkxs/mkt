package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// listenFlagCmd builds a command carrying the same listen flags rootCmd
// declares, so resolveListenToken can be exercised without starting the
// dashboard its RunE would otherwise launch.
func listenFlagCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().String("listen", "", "")
	c.Flags().String("listen-token", "", "")
	c.Flags().String("listen-token-file", "", "")
	return c
}

// clearListenEnv unsets every out-of-band source so a case only sees what it
// sets itself.
func clearListenEnv(t *testing.T) {
	t.Helper()
	t.Setenv(EnvListen, "")
	t.Setenv(EnvListenToken, "")
	t.Setenv(EnvListenTokenFile, "")
	if err := os.Unsetenv(EnvListen); err != nil {
		t.Fatalf("unset %s: %v", EnvListen, err)
	}
	if err := os.Unsetenv(EnvListenToken); err != nil {
		t.Fatalf("unset %s: %v", EnvListenToken, err)
	}
	if err := os.Unsetenv(EnvListenTokenFile); err != nil {
		t.Fatalf("unset %s: %v", EnvListenTokenFile, err)
	}
}

// tokenFile writes a token file and returns its path.
func tokenFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

// TestResolveListenTokenFromEnv covers the reason these sources exist: a
// token on argv is readable by every other user on the host through ps and
// /proc/<pid>/cmdline.
func TestResolveListenTokenFromEnv(t *testing.T) {
	clearListenEnv(t)
	t.Setenv(EnvListenToken, "from-env")

	cmd := listenFlagCmd()
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen-token"); got != "from-env" {
		t.Errorf("listen-token = %q, want from-env", got)
	}
}

// TestResolveListenTokenFromFileFlag checks --listen-token-file, and that the
// trailing newline every editor adds is trimmed rather than becoming part of
// the bearer token.
func TestResolveListenTokenFromFileFlag(t *testing.T) {
	clearListenEnv(t)
	path := tokenFile(t, "  from-file\n")

	cmd := listenFlagCmd()
	if err := cmd.Flags().Set("listen-token-file", path); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen-token"); got != "from-file" {
		t.Errorf("listen-token = %q, want from-file", got)
	}
}

// TestResolveListenTokenFromFileEnv covers the environment form of the same
// thing.
func TestResolveListenTokenFromFileEnv(t *testing.T) {
	clearListenEnv(t)
	t.Setenv(EnvListenTokenFile, tokenFile(t, "env-file-token\n"))

	cmd := listenFlagCmd()
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen-token"); got != "env-file-token" {
		t.Errorf("listen-token = %q, want env-file-token", got)
	}
}

// TestResolveListenTokenPrecedence pins the documented order:
// --listen-token > --listen-token-file > MKT_LISTEN_TOKEN_FILE > MKT_LISTEN_TOKEN.
func TestResolveListenTokenPrecedence(t *testing.T) {
	clearListenEnv(t)
	t.Setenv(EnvListenToken, "env-token")
	t.Setenv(EnvListenTokenFile, tokenFile(t, "env-file-token"))

	t.Run("explicit flag wins", func(t *testing.T) {
		cmd := listenFlagCmd()
		if err := cmd.Flags().Set("listen-token", "argv-token"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if err := resolveListenToken(cmd, nil); err != nil {
			t.Fatalf("resolveListenToken: %v", err)
		}
		if got, _ := cmd.Flags().GetString("listen-token"); got != "argv-token" {
			t.Errorf("listen-token = %q, want argv-token", got)
		}
	})

	t.Run("file flag beats both env vars", func(t *testing.T) {
		cmd := listenFlagCmd()
		if err := cmd.Flags().Set("listen-token-file", tokenFile(t, "flag-file-token")); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if err := resolveListenToken(cmd, nil); err != nil {
			t.Fatalf("resolveListenToken: %v", err)
		}
		if got, _ := cmd.Flags().GetString("listen-token"); got != "flag-file-token" {
			t.Errorf("listen-token = %q, want flag-file-token", got)
		}
	})

	t.Run("token file env beats token env", func(t *testing.T) {
		cmd := listenFlagCmd()
		if err := resolveListenToken(cmd, nil); err != nil {
			t.Fatalf("resolveListenToken: %v", err)
		}
		if got, _ := cmd.Flags().GetString("listen-token"); got != "env-file-token" {
			t.Errorf("listen-token = %q, want env-file-token", got)
		}
	})
}

// TestResolveListenTokenFileErrors checks a token source that was configured
// but cannot be used fails loudly. Falling through to "no token" would serve
// an unauthenticated bind on a host the operator believed was protected.
func TestResolveListenTokenFileErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		clearListenEnv(t)
		cmd := listenFlagCmd()
		if err := cmd.Flags().Set("listen-token-file", filepath.Join(t.TempDir(), "absent")); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		err := resolveListenToken(cmd, nil)
		if err == nil {
			t.Fatal("expected an error for a missing token file")
		}
		if !strings.Contains(err.Error(), "--listen-token-file") {
			t.Errorf("error does not name the source: %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		clearListenEnv(t)
		cmd := listenFlagCmd()
		if err := cmd.Flags().Set("listen-token-file", tokenFile(t, "   \n")); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		err := resolveListenToken(cmd, nil)
		if err == nil {
			t.Fatal("expected an error for an empty token file")
		}
		if !strings.Contains(err.Error(), "is empty") {
			t.Errorf("error = %v", err)
		}
	})
}

// TestResolveListenFallsBackToEnv covers the address itself, so a service
// manager can configure the bind without touching argv either.
func TestResolveListenFallsBackToEnv(t *testing.T) {
	clearListenEnv(t)
	t.Setenv(EnvListen, "127.0.0.1:9999")

	cmd := listenFlagCmd()
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen"); got != "127.0.0.1:9999" {
		t.Errorf("listen = %q, want 127.0.0.1:9999", got)
	}
}

// TestResolveListenPrefersTheExplicitFlag checks the env var is a fallback,
// not an override.
func TestResolveListenPrefersTheExplicitFlag(t *testing.T) {
	clearListenEnv(t)
	t.Setenv(EnvListen, "127.0.0.1:1111")

	cmd := listenFlagCmd()
	if err := cmd.Flags().Set("listen", "127.0.0.1:2222"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen"); got != "127.0.0.1:2222" {
		t.Errorf("listen = %q, want the explicit flag to win", got)
	}
}

// TestResolveListenTokenWithNothingConfiguredIsNotAnError keeps the common
// case quiet: no listen surface, no token, nothing to resolve.
func TestResolveListenTokenWithNothingConfiguredIsNotAnError(t *testing.T) {
	clearListenEnv(t)
	cmd := listenFlagCmd()
	if err := resolveListenToken(cmd, nil); err != nil {
		t.Fatalf("resolveListenToken: %v", err)
	}
	if got, _ := cmd.Flags().GetString("listen-token"); got != "" {
		t.Errorf("listen-token = %q, want empty", got)
	}
}

// ─────────────────────────── usage-dump suppression ───────────────────────────

// TestRuntimeFailurePrintsNoUsage is the regression test for every validation
// failure dumping the whole command tree, which buried the one line the user
// needed.
func TestRuntimeFailurePrintsNoUsage(t *testing.T) {
	seedConfig(t, brokenConfig)

	out, errOut, err := runCLI(t, nil, "config", "validate")
	if err == nil {
		t.Fatal("expected validate to fail on a broken config")
	}
	if strings.Contains(out+errOut, "Usage:") || strings.Contains(out+errOut, "Available Commands:") {
		t.Errorf("a runtime failure printed a usage dump:\n%s%s", out, errOut)
	}
}

// TestFlagErrorStillShowsUsage is the other side of it: a mistyped flag *is*
// a usage mistake, and silencing usage everywhere would have made that case
// worse.
func TestFlagErrorStillShowsUsage(t *testing.T) {
	_, _, err := runCLI(t, nil, "position", "--not-a-flag")
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "Usage:") {
		t.Errorf("a flag error lost its usage text: %v", err)
	}
}

// TestRepairIsDiscoverable checks the recovery path every refusal message
// points at actually appears in --help. A command the user is told to run
// and cannot find is worse than no suggestion.
func TestRepairIsDiscoverable(t *testing.T) {
	out, _, err := runCLI(t, nil, "config", "--help")
	if err != nil {
		t.Fatalf("config --help: %v", err)
	}
	if !strings.Contains(out, "repair") {
		t.Errorf("`mkt config --help` does not mention repair:\n%s", out)
	}
}

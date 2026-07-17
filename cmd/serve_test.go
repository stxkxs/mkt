package cmd

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/ssh"
	"github.com/stxkxs/mkt/internal/config"
	gossh "golang.org/x/crypto/ssh"
)

// genKeyLine returns a valid authorized_keys line for a fresh ed25519 key.
func genKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub)))
}

func TestLoadAuthorizedKeysInline(t *testing.T) {
	l1, l2 := genKeyLine(t), genKeyLine(t)
	keys, err := loadAuthorizedKeys(config.ServeConfig{AuthorizedKeys: []string{l1, l2}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	want, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(l1))
	if !ssh.KeysEqual(keys[0], want) {
		t.Error("first key does not round-trip through the allowlist")
	}
}

func TestLoadAuthorizedKeysSkipsBlanksAndComments(t *testing.T) {
	line := genKeyLine(t)
	keys, err := loadAuthorizedKeys(config.ServeConfig{
		AuthorizedKeys: []string{"", "   ", "# a comment", line},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1 (blanks/comments skipped)", len(keys))
	}
}

func TestLoadAuthorizedKeysRejectsBadKey(t *testing.T) {
	if _, err := loadAuthorizedKeys(config.ServeConfig{AuthorizedKeys: []string{"not-a-real-key"}}); err == nil {
		t.Error("expected an error for a malformed key")
	}
}

func TestLoadAuthorizedKeysFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")
	body := genKeyLine(t) + "\n# comment\n" + genKeyLine(t) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, err := loadAuthorizedKeys(config.ServeConfig{AuthorizedKeysFile: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2 from file", len(keys))
	}
}

func TestLoadAuthorizedKeysEmptyIsEmpty(t *testing.T) {
	keys, err := loadAuthorizedKeys(config.ServeConfig{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("got %d keys, want 0 (runServe refuses to start on empty)", len(keys))
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("got %q, want x", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"":              "",
		"/abs/path":     "/abs/path",
		"~":             home,
		"~/.config/mkt": filepath.Join(home, ".config", "mkt"),
	}
	for in, want := range cases {
		if got := expandPath(in); got != want {
			t.Errorf("expandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

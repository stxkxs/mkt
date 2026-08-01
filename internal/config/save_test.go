package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The user's real file from the bug report: two watchlist symbols, one
// portfolio, one alert — and a bad indent on line 9 that makes the whole
// thing unparseable.
const brokenUserConfig = `watchlist:
  - VTI
  - AAPL
theme: nord
portfolios:
  - name: Retirement
    holdings:
      - symbol: VTI
       quantity: 500
        cost_basis: 210.00
alerts:
  - symbol: AAPL
    condition: above
    value: 400
    enabled: true
`

// The same file, without the indentation mistake.
const validUserConfig = `watchlist:
  - VTI
  - AAPL
theme: nord
portfolios:
  - name: Retirement
    holdings:
      - symbol: VTI
        quantity: 500
        cost_basis: 210.00
alerts:
  - symbol: AAPL
    condition: above
    value: 400
    enabled: true
notes:
  VTI: total market, the boring core
webhook_url: https://example.invalid/hook
`

// isolate points the config dir at a fresh tempdir and returns the config
// path, so a test never touches the developer's real ~/.config/mkt.
func isolate(t *testing.T) string {
	t.Helper()
	dir := isolateAt(t, t.TempDir())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "config.yaml")
}

// isolateAt redirects the config dir under home and returns the config dir.
//
// Setting HOME alone is not enough: os.UserHomeDir reads $HOME on Unix but
// %USERPROFILE% on Windows, so a HOME-only test ran against the developer's
// real config directory there — which is exactly how the Windows CI leg
// started failing on cross-contaminated state. DirEnv is the portable
// override; HOME and USERPROFILE are still set because unrelated code paths
// (SSH host key defaults, ~ expansion) resolve a home directory of their own.
func isolateAt(t *testing.T, home string) string {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".config", "mkt")
	t.Setenv(DirEnv, dir)
	return dir
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ─── LoadWithResult: the three outcomes ───────────────────────────────────

func TestLoadWithResultAbsentFile(t *testing.T) {
	path := isolate(t)
	if err := os.Remove(filepath.Dir(path)); err != nil { // start with no dir at all
		t.Fatal(err)
	}

	res, err := LoadWithResult()
	if err != nil {
		t.Fatalf("LoadWithResult: %v", err)
	}
	if res.Degraded {
		t.Error("a missing config is not degraded")
	}
	if res.Err != nil {
		t.Errorf("Err on a missing config: %v", res.Err)
	}
	if res.Path != path {
		t.Errorf("Path = %q, want %q", res.Path, path)
	}
	if len(res.Config.Watchlist) != len(DefaultWatchlist) {
		t.Errorf("watchlist: got %d symbols, want the %d defaults", len(res.Config.Watchlist), len(DefaultWatchlist))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("first run must write %s: %v", path, err)
	}
}

func TestLoadWithResultValidFile(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	res, err := LoadWithResult()
	if err != nil {
		t.Fatalf("LoadWithResult: %v", err)
	}
	if res.Degraded {
		t.Fatalf("a file that parses is not degraded: %v", res.Err)
	}
	if got := res.Config.Theme; got != "nord" {
		t.Errorf("theme = %q, want nord", got)
	}
	if len(res.Config.Portfolios) != 1 || res.Config.Portfolios[0].Name != "Retirement" {
		t.Errorf("portfolios: got %+v, want the user's Retirement", res.Config.Portfolios)
	}
}

// The headline bug: a config with one bad indent used to load as the
// defaults, silently, and the next write persisted those defaults over the
// user's file. LoadWithResult must say so.
func TestLoadWithResultDegradedFile(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, brokenUserConfig)

	res, err := LoadWithResult()
	if err != nil {
		t.Fatalf("LoadWithResult must not fail on a broken file: %v", err)
	}
	if !res.Degraded {
		t.Fatal("a config that does not parse must be reported as degraded")
	}
	if res.Err == nil {
		t.Error("Degraded result must carry the parse error")
	}
	// Best effort: YAML reports where the parser gave up, which for a bad
	// indent is the head of the enclosing block (line 7, `holdings:`) rather
	// than the offending line itself. Close enough to point the user at it.
	if res.Line != 7 {
		t.Errorf("Line = %d, want 7 (where YAML reports the break), err = %v", res.Line, res.Err)
	}
	if res.Path != path {
		t.Errorf("Path = %q, want %q", res.Path, path)
	}
	// The dashboard still starts, on defaults.
	if len(res.Config.Watchlist) != len(DefaultWatchlist) {
		t.Errorf("degraded load should fall back to the default watchlist, got %d symbols", len(res.Config.Watchlist))
	}
	// And the broken file is left exactly as the user wrote it.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != brokenUserConfig {
		t.Errorf("a degraded load rewrote the user's file:\n%s", raw)
	}
}

// Load keeps its old signature and ignores degradation, so callers that do
// not write are unaffected.
func TestLoadIgnoresDegradation(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, brokenUserConfig)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load on a broken config: %v", err)
	}
	if cfg == nil || len(cfg.Watchlist) == 0 {
		t.Fatal("Load must still return a usable config")
	}
}

// ─── SaveSafely: degraded protection ──────────────────────────────────────

// The end-to-end reproduction of the data loss: load a broken config (which
// yields defaults), then save. The user's 243-byte file must survive.
func TestSaveSafelyRefusesToOverwriteDegradedConfig(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, brokenUserConfig)

	cfg, err := Load() // returns defaults — this is the trap
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddSymbol("TSLA")

	rep, err := SaveSafely(cfg, SaveOptions{AssumeYes: true})
	if !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("SaveSafely over a degraded config: got %v, want ErrWouldDestroy", err)
	}
	if rep.Wrote {
		t.Error("report says it wrote; it must not have")
	}
	if len(rep.Removed) == 0 {
		t.Error("report must say what would have been lost")
	}

	var de *DestroyError
	if !errors.As(err, &de) {
		t.Fatalf("error must be a *DestroyError, got %T", err)
	}
	if de.Reason != ReasonDegraded || !de.Degraded {
		t.Errorf("reason = %q degraded = %v, want %q / true", de.Reason, de.Degraded, ReasonDegraded)
	}
	if de.Line == 0 {
		t.Error("DestroyError must point at a line so the user can find the break")
	}
	if de.Path != path {
		t.Errorf("DestroyError.Path = %q, want %q", de.Path, path)
	}
	if !strings.Contains(de.Error(), "--force") {
		t.Errorf("the message must tell the user how to proceed:\n%s", de.Error())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != brokenUserConfig {
		t.Fatalf("the user's file was modified:\n%s", raw)
	}
}

// Save() is the non-interactive wrapper every existing caller uses; it must
// inherit the degraded protection.
func TestSaveInheritsDegradedProtection(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, brokenUserConfig)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(cfg); !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("Save over a degraded config: got %v, want ErrWouldDestroy", err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != brokenUserConfig {
		t.Error("Save modified a degraded config file")
	}
}

// --force is the escape hatch, and it still takes a backup on the way past.
func TestSaveSafelyForceOverwritesDegradedAndBacksUp(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, brokenUserConfig)

	cfg := &Config{Watchlist: []string{"AAPL"}, Theme: "nord", PollInterval: "15s", SparklineLen: 60}
	rep, err := SaveSafely(cfg, SaveOptions{Force: true, AssumeYes: true})
	if err != nil {
		t.Fatalf("SaveSafely --force: %v", err)
	}
	if !rep.Wrote {
		t.Error("--force must write")
	}
	if rep.BackupPath == "" {
		t.Fatal("--force must still back the broken file up")
	}
	backup, err := os.ReadFile(rep.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != brokenUserConfig {
		t.Errorf("the backup does not hold the original file:\n%s", backup)
	}
	if got, _ := os.ReadFile(path); string(got) == brokenUserConfig {
		t.Error("--force did not actually write")
	}
}

// ─── SaveSafely: the removal prompt ───────────────────────────────────────

// dropAll builds a config that keeps nothing from validUserConfig.
func dropAll() *Config {
	return &Config{Watchlist: []string{"AAPL"}, Theme: "nord", PollInterval: "15s", SparklineLen: 60}
}

func TestSaveSafelyPromptAccepted(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	var out bytes.Buffer
	rep, err := SaveSafely(dropAll(), SaveOptions{In: strings.NewReader("y\n"), Out: &out})
	if err != nil {
		t.Fatalf("SaveSafely with an accepted prompt: %v", err)
	}
	if !rep.Wrote {
		t.Error("an accepted prompt must write")
	}
	if rep.BackupPath == "" {
		t.Error("a destructive write must leave a backup")
	}
	prompt := out.String()
	for _, want := range []string{`portfolio "Retirement"`, "500 VTI", "alert rule (AAPL above 400)", "note for VTI", "cleared webhook_url", "Continue?"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSaveSafelyPromptDeclined(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	var out bytes.Buffer
	rep, err := SaveSafely(dropAll(), SaveOptions{In: strings.NewReader("n\n"), Out: &out})
	if !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("declined prompt: got %v, want ErrWouldDestroy", err)
	}
	if rep.Wrote {
		t.Error("a declined prompt must not write")
	}
	var de *DestroyError
	if errors.As(err, &de) && de.Reason != ReasonDeclined {
		t.Errorf("reason = %q, want %q", de.Reason, ReasonDeclined)
	}
	if got, _ := os.ReadFile(path); string(got) != validUserConfig {
		t.Errorf("a declined write still modified the file:\n%s", got)
	}
}

// Silence (EOF, e.g. stdin redirected from /dev/null) is a "no", not a hang.
func TestSaveSafelyPromptEOFDeclines(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	var out bytes.Buffer
	if _, err := SaveSafely(dropAll(), SaveOptions{In: strings.NewReader(""), Out: &out}); !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("EOF at the prompt: got %v, want ErrWouldDestroy", err)
	}
	if got, _ := os.ReadFile(path); string(got) != validUserConfig {
		t.Error("EOF at the prompt still modified the file")
	}
}

// A "yes" typed with stray whitespace or in capitals still means yes.
func TestSaveSafelyPromptAnswers(t *testing.T) {
	for _, tc := range []struct {
		answer string
		accept bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  YES  \n", true},
		{"n\n", false},
		{"\n", false},
		{"maybe\n", false},
	} {
		t.Run(strings.TrimSpace(tc.answer), func(t *testing.T) {
			path := isolate(t)
			writeConfigFile(t, path, validUserConfig)
			var out bytes.Buffer
			_, err := SaveSafely(dropAll(), SaveOptions{In: strings.NewReader(tc.answer), Out: &out})
			if tc.accept && err != nil {
				t.Fatalf("answer %q: %v", tc.answer, err)
			}
			if !tc.accept && !errors.Is(err, ErrWouldDestroy) {
				t.Fatalf("answer %q: got %v, want ErrWouldDestroy", tc.answer, err)
			}
		})
	}
}

// Nobody at the keyboard: refuse rather than block on a read that will never
// be answered, and point at --yes.
func TestSaveSafelyRefusesWithoutATerminal(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	restore := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = restore })

	var out bytes.Buffer
	rep, err := SaveSafely(dropAll(), SaveOptions{Out: &out}) // In == nil -> os.Stdin
	if !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("non-TTY destructive write: got %v, want ErrWouldDestroy", err)
	}
	if rep.Wrote {
		t.Error("must not write without confirmation")
	}
	var de *DestroyError
	if !errors.As(err, &de) {
		t.Fatalf("want *DestroyError, got %T", err)
	}
	if de.Reason != ReasonNoPrompt {
		t.Errorf("reason = %q, want %q", de.Reason, ReasonNoPrompt)
	}
	if !strings.Contains(de.Error(), "--yes") {
		t.Errorf("the message must point at --yes:\n%s", de.Error())
	}
	if out.Len() != 0 {
		t.Errorf("nothing should be prompted when there is nobody to answer: %q", out.String())
	}
	if got, _ := os.ReadFile(path); string(got) != validUserConfig {
		t.Error("the file was modified")
	}
}

// A purely additive write asks nothing, even with no terminal.
func TestSaveSafelyAdditiveWriteNeedsNoConfirmation(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)

	restore := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = restore })

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AddSymbol("TSLA") {
		t.Fatal("AddSymbol(TSLA) should have added")
	}

	var out bytes.Buffer
	rep, err := SaveSafely(cfg, SaveOptions{Out: &out})
	if err != nil {
		t.Fatalf("additive SaveSafely: %v", err)
	}
	if len(rep.Removed) != 0 {
		t.Errorf("an additive write drops nothing, got %v", rep.Removed)
	}
	if out.Len() != 0 {
		t.Errorf("an additive write must not prompt: %q", out.String())
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Portfolios) != 1 || reloaded.Portfolios[0].Name != "Retirement" {
		t.Errorf("the user's portfolio did not survive: %+v", reloaded.Portfolios)
	}
	if reloaded.Notes["VTI"] == "" {
		t.Error("the user's note did not survive")
	}
	if reloaded.WebhookURL == "" {
		t.Error("the user's webhook did not survive")
	}
	found := false
	for _, s := range reloaded.Watchlist {
		if s == "TSLA" {
			found = true
		}
	}
	if !found {
		t.Errorf("TSLA was not added: %v", reloaded.Watchlist)
	}
}

// ─── SaveSafely: mechanics ────────────────────────────────────────────────

func TestSaveSafelyFirstWriteTakesNoBackup(t *testing.T) {
	path := isolate(t)
	rep, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true})
	if err != nil {
		t.Fatalf("SaveSafely: %v", err)
	}
	if rep.BackupPath != "" {
		t.Errorf("nothing to back up on a first write, got %q", rep.BackupPath)
	}
	if !rep.Wrote {
		t.Error("Wrote should be true")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
}

// Re-saving an unchanged config is a no-op as far as backups go; otherwise
// every `mkt config show`-adjacent write would burn a backup slot.
func TestSaveSafelyIdenticalWriteTakesNoBackup(t *testing.T) {
	isolate(t)
	cfg := dropAll()
	if _, err := SaveSafely(cfg, SaveOptions{AssumeYes: true}); err != nil {
		t.Fatal(err)
	}
	rep, err := SaveSafely(cfg, SaveOptions{AssumeYes: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.BackupPath != "" {
		t.Errorf("an identical rewrite should not back up, got %q", rep.BackupPath)
	}
}

func TestSaveSafelyWrites0600(t *testing.T) {
	requireUnixPerms(t)
	path := isolate(t)
	if _, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config.yaml perm = %o, want 0600 (holds secrets)", perm)
	}
}

// A failed write must leave the previous config byte-for-byte intact and
// drop no debris — the whole point of writing through a temp file and a
// rename. A read-only directory is the cheapest way to fail the write.
func TestWriteAtomicFailsCleanlyInReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not gate file creation the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop writes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeConfigFile(t, path, validUserConfig)

	if err := os.Chmod(dir, 0o500); err != nil { // r-x: no new files
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := writeAtomic(path, []byte("watchlist: [TSLA]\n"), 0o600); err == nil {
		t.Fatal("writeAtomic into a read-only directory should fail")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the config file disappeared: %v", err)
	}
	if string(got) != validUserConfig {
		t.Errorf("the previous config was damaged:\n%s", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.yaml" {
			t.Errorf("debris left behind: %s", e.Name())
		}
	}
}

// SaveSafely surfaces a directory it cannot create instead of pretending the
// write happened.
func TestSaveSafelyReportsUnwritableConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not gate directory creation the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop mkdir")
	}
	home := t.TempDir()
	isolateAt(t, home)
	// ~/.config exists but is read-only, so ~/.config/mkt cannot be created.
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, ".config"), 0o700) })

	rep, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true})
	if err == nil {
		t.Fatal("SaveSafely should fail when the config dir cannot be created")
	}
	if rep != nil && rep.Wrote {
		t.Error("Wrote must be false on a failed write")
	}
}

func TestWriteAtomicReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("old contents that are much longer than the new ones"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("writeAtomic left debris: %v", entries)
	}
}

// The whole file is replaced in one rename, so a reader either sees the old
// bytes or the new ones — never a prefix of the new ones.
func TestWriteAtomicNeverTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := strings.Repeat("watchlist: [AAPL]\n", 500)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	// On Unix the strongest available proof is to hold the original open
	// across the write: rename swaps the directory entry, so the descriptor
	// still sees the whole original file. That is exactly the property that
	// makes a mid-write crash survivable — the old bytes are intact until the
	// new file is complete.
	//
	// Windows has no equivalent. There is no inode to keep alive, and
	// MoveFileEx cannot replace a target another handle has open, so the
	// rename fails rather than succeeding invisibly. Assert the weaker but
	// still meaningful property there: the replacement is all-or-nothing and
	// the file is never observed as a truncated prefix.
	if runtime.GOOS != "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		if err := writeAtomic(path, []byte("watchlist: [TSLA]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		held, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		if string(held) != original {
			t.Error("the replaced file was modified in place instead of being renamed over")
		}
		return
	}

	if err := writeAtomic(path, []byte("watchlist: [TSLA]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "watchlist: [TSLA]\n" {
		t.Errorf("contents = %q, want the fully replaced file", got)
	}
	if strings.HasPrefix(string(got), original[:64]) {
		t.Error("the file was truncated in place rather than replaced")
	}
}

// ─── SaveSafely: round-trip through the safe path ─────────────────────────

// Everything Save persists must survive a rewrite, now that the bytes are
// produced here rather than by viper's WriteConfig.
func TestSaveSafelyRoundTripsEverySection(t *testing.T) {
	isolate(t)
	yes, no := true, false
	original := &Config{
		Watchlist:  []string{"AAPL", "BTC-USD"},
		Watchlists: []Watchlist{{Name: "Tech", Symbols: []string{"AAPL", "MSFT"}}},
		Portfolios: []Portfolio{{
			Name:         "Core",
			Holdings:     []Holding{{Symbol: "AAPL", Name: "Apple", Quantity: 10, CostBasis: 150}},
			Transactions: []Transaction{{Type: "buy", Symbol: "AAPL", Quantity: 10, Price: 150, Time: "2026-01-02"}},
			TaxMethod:    "fifo",
		}},
		Alerts:        []AlertRule{{Symbol: "AAPL", Condition: "above", Value: 400, Enabled: true}},
		PollInterval:  "20s",
		SparklineLen:  90,
		Theme:         "gruvbox",
		WebhookURL:    "https://example.invalid/hook",
		NtfyTopic:     "mkt",
		NtfyServer:    "https://ntfy.invalid",
		PushoverUser:  "u",
		PushoverToken: "t",
		EDGARTickers:  []string{"AAPL"},
		Notes:         map[string]string{"AAPL": "flagship"},
		Serve:         ServeConfig{Addr: "127.0.0.1:2222", AuthorizedKeys: []string{"ssh-ed25519 AAAAC3 you@host"}},
		DesktopNotify: &no,
		Notifications: &yes,
		Providers:     Providers{Binance: &no},
		NewsFeeds:     []NewsFeed{{Name: "Feed", URL: "https://example.invalid/rss"}},
	}
	if _, err := SaveSafely(original, SaveOptions{AssumeYes: true}); err != nil {
		t.Fatalf("SaveSafely: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(got.Portfolios) != 1 || len(got.Portfolios[0].Transactions) != 1 || got.Portfolios[0].TaxMethod != "fifo" {
		t.Errorf("portfolio round-trip: %+v", got.Portfolios)
	}
	if got.Notes["AAPL"] != "flagship" {
		t.Errorf("notes round-trip: %+v", got.Notes)
	}
	if got.Serve.Addr != "127.0.0.1:2222" || len(got.Serve.AuthorizedKeys) != 1 {
		t.Errorf("serve round-trip: %+v", got.Serve)
	}
	if got.DesktopNotify == nil || *got.DesktopNotify {
		t.Errorf("desktop_notify round-trip: %v", got.DesktopNotify)
	}
	if got.Notifications == nil || !*got.Notifications {
		t.Errorf("notifications round-trip: %v", got.Notifications)
	}
	if got.Providers.BinanceOn() {
		t.Error("providers.binance round-trip: should be off")
	}
	if len(got.NewsFeeds) != 1 || got.NewsFeeds[0].Name != "Feed" {
		t.Errorf("news_feeds round-trip: %+v", got.NewsFeeds)
	}
	if got.PushoverToken != "t" || got.NtfyServer != "https://ntfy.invalid" {
		t.Errorf("secrets round-trip: %+v", got)
	}

	// Saving what we just loaded is a no-op: nothing is reported as dropped.
	rep, err := SaveSafely(got, SaveOptions{})
	if err != nil {
		t.Fatalf("re-saving a freshly loaded config must not need confirmation: %v", err)
	}
	if len(rep.Removed) != 0 {
		t.Errorf("load→save reported losses: %v", rep.Removed)
	}
}

// ─── error surface ────────────────────────────────────────────────────────

func TestDestroyErrorUnwrapsParseError(t *testing.T) {
	t.Parallel()
	inner := errors.New("yaml: line 9: bad indent")
	de := &DestroyError{Path: "/tmp/config.yaml", Reason: ReasonDegraded, Degraded: true, Line: 9, Err: inner}
	if !errors.Is(de, ErrWouldDestroy) {
		t.Error("a DestroyError must match ErrWouldDestroy")
	}
	if !errors.Is(de, inner) {
		t.Error("a DestroyError must unwrap to the parse error")
	}
	if !strings.Contains(de.Error(), "line 9") {
		t.Errorf("message should name the line:\n%s", de.Error())
	}
}

func TestYAMLErrorLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"viper wrapped", errors.New("While parsing config: yaml: line 9: did not find expected key"), 9},
		{"unmarshal list", errors.New("yaml: unmarshal errors:\n  line 12: cannot unmarshal"), 12},
		{"no position", errors.New("permission denied"), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := yamlErrorLine(tc.err); got != tc.want {
				t.Errorf("yamlErrorLine(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// ─── remaining failure paths ──────────────────────────────────────────────

// A config path that is not a regular file is a broken installation, not an
// invitation to write over it.
func TestSaveSafelyWhenConfigPathIsADirectory(t *testing.T) {
	path := isolate(t)
	if err := os.MkdirAll(filepath.Join(path, "surprise"), 0o700); err != nil {
		t.Fatal(err)
	}
	rep, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true, Force: true})
	if err == nil {
		t.Fatal("SaveSafely should fail when the config path is a directory")
	}
	if rep != nil && rep.Wrote {
		t.Error("Wrote must be false")
	}
}

// A rename that cannot land leaves no temp file behind.
func TestWriteAtomicRenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("x"), 0o600); err == nil {
		t.Fatal("renaming over a non-empty directory should fail")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Errorf("debris left behind: %v", entries)
	}
}

// parseFile must reject a file whose shape does not fit the config, not
// silently return half of it.
func TestParseFileRejectsWrongShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// watchlist as a mapping, not a list — parses as YAML, fails to decode.
	writeConfigFile(t, path, "watchlist:\n  a: 1\n  b: 2\n")
	if _, _, err := parseFile(path); err == nil {
		t.Fatal("parseFile should reject a config it cannot decode")
	}
}

// A config whose structure is valid YAML but wrong for mkt is still a
// degraded file: refuse the write rather than replacing it with defaults.
func TestSaveSafelyRefusesUndecodableConfig(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, "watchlist:\n  a: 1\n  b: 2\n")
	_, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true})
	if !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("got %v, want ErrWouldDestroy", err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "a: 1") {
		t.Errorf("the file was modified:\n%s", got)
	}
}

func TestDestroyErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *DestroyError
		want []string
	}{
		{
			"degraded without a line",
			&DestroyError{Path: "/c.yaml", Reason: ReasonDegraded, Degraded: true, Err: errors.New("boom")},
			[]string{"/c.yaml", "does not parse", "boom", "--force"},
		},
		{
			"declined lists what was at stake",
			&DestroyError{Path: "/c.yaml", Reason: ReasonDeclined, Removed: []string{`portfolio "Retirement" (1 holding, 500 VTI)`}},
			[]string{"declined", `portfolio "Retirement"`},
		},
		{
			"unknown reason still says why it stopped",
			&DestroyError{Path: "/c.yaml"},
			[]string{"destroy user data"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("message missing %q:\n%s", want, msg)
				}
			}
			if !errors.Is(tc.err, ErrWouldDestroy) {
				t.Error("must match ErrWouldDestroy")
			}
		})
	}
}

// The prompt writes to Out and reads from In; a reader that errors is not a
// yes.
func TestConfirmReadErrorDeclines(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := confirm(SaveOptions{In: errReader{}, Out: &out}, "/c.yaml", []string{"something"}, false)
	if !errors.Is(err, ErrWouldDestroy) {
		t.Fatalf("got %v, want ErrWouldDestroy", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no input device") }

// A HOME we cannot create ~/.config/mkt under is a hard failure — better a
// clear error than a dashboard silently running on defaults it can never
// persist.
func TestLoadFailsWhenConfigDirCannotBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not gate directory creation the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop mkdir")
	}
	home := t.TempDir()
	isolateAt(t, home)
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail when the config dir cannot be created")
	}
	if _, err := LoadWithResult(); err == nil {
		t.Fatal("LoadWithResult should fail when the config dir cannot be created")
	}
}

// Seeding a fresh install is a write like any other; a failure is reported,
// not swallowed into a half-written file.
func TestWriteSeedReportsWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not gate file creation the same way on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop writes")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := writeSeed(filepath.Join(dir, "config.yaml"), map[string]any{"theme": "nord"})
	if err == nil {
		t.Fatal("writeSeed into a read-only directory should fail")
	}
}

// A line number too large to be a line number is no line number at all.
func TestYAMLErrorLineOverflow(t *testing.T) {
	t.Parallel()
	err := errors.New("yaml: line 99999999999999999999999: broken")
	if got := yamlErrorLine(err); got != 0 {
		t.Errorf("yamlErrorLine = %d, want 0 for an unrepresentable line", got)
	}
}

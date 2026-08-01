package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pinClock makes backup timestamps deterministic and lets a test manufacture
// a history of backups without sleeping.
func pinClock(t *testing.T, at time.Time) func(time.Duration) {
	t.Helper()
	now := at
	restore := backupClock
	backupClock = func() time.Time { return now }
	t.Cleanup(func() { backupClock = restore })
	return func(d time.Duration) { now = now.Add(d) }
}

func TestBackupCopiesTheFile(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)
	pinClock(t, time.Date(2026, 8, 1, 13, 45, 30, 0, time.Local))

	dest, err := Backup(path)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if want := path + ".bak.20260801-134530"; dest != want {
		t.Errorf("backup path = %q, want %q", dest, want)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validUserConfig {
		t.Errorf("backup contents:\n%s", got)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("backup perm = %o, want 0600 (it holds the same secrets)", perm)
	}
}

func TestBackupOfMissingFileFails(t *testing.T) {
	path := isolate(t)
	if _, err := Backup(path); err == nil {
		t.Fatal("backing up a file that is not there should fail")
	}
}

// Two writes inside the same second must not clobber each other's backup.
func TestBackupSameSecondDoesNotClobber(t *testing.T) {
	path := isolate(t)
	pinClock(t, time.Date(2026, 8, 1, 13, 45, 30, 0, time.Local))

	writeConfigFile(t, path, "watchlist: [AAPL]\n")
	first, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	writeConfigFile(t, path, "watchlist: [TSLA]\n")
	second, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two backups in the same second reused one path")
	}
	if got, _ := os.ReadFile(first); string(got) != "watchlist: [AAPL]\n" {
		t.Errorf("the first backup was overwritten: %s", got)
	}
	if got, _ := os.ReadFile(second); string(got) != "watchlist: [TSLA]\n" {
		t.Errorf("second backup: %s", got)
	}
}

func TestListBackupsNewestFirst(t *testing.T) {
	path := isolate(t)
	advance := pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))

	var want []string
	for i := range 4 {
		writeConfigFile(t, path, fmt.Sprintf("sparkline_len: %d\n", i))
		p, err := Backup(path)
		if err != nil {
			t.Fatal(err)
		}
		want = append([]string{p}, want...) // newest first
		advance(time.Minute)
	}

	got, err := ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d backups, want %d", len(got), len(want))
	}
	for i, b := range got {
		if b.Path != want[i] {
			t.Errorf("backup[%d] = %s, want %s", i, b.Path, want[i])
		}
		if b.Size == 0 {
			t.Errorf("backup[%d] has size 0", i)
		}
		if b.Taken.IsZero() {
			t.Errorf("backup[%d] has no timestamp", i)
		}
	}
	if !got[0].Taken.After(got[len(got)-1].Taken) {
		t.Error("backups are not newest-first")
	}
}

func TestListBackupsIgnoresUnrelatedFiles(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)
	pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))
	if _, err := Backup(path); err != nil {
		t.Fatal(err)
	}
	// Neighbors that must not be mistaken for backups.
	for _, name := range []string{"config.yaml.bak.not-a-timestamp", "config.yaml.swp", "history.json"} {
		writeConfigFile(t, filepath.Join(filepath.Dir(path), name), "x")
	}

	got, err := ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d backups, want 1: %+v", len(got), got)
	}
}

func TestListBackupsWithNoConfigDir(t *testing.T) {
	path := isolate(t)
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	got, err := ListBackups()
	if err != nil {
		t.Fatalf("ListBackups on a missing dir should be empty, not an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d backups, want 0", len(got))
	}
}

// Backups cannot grow without bound; the oldest go first.
func TestBackupPrunesToMaxBackups(t *testing.T) {
	path := isolate(t)
	advance := pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))

	total := MaxBackups + 5
	var newest string
	for i := range total {
		writeConfigFile(t, path, fmt.Sprintf("sparkline_len: %d\n", i))
		p, err := Backup(path)
		if err != nil {
			t.Fatal(err)
		}
		newest = p
		advance(time.Minute)
	}

	got, err := ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxBackups {
		t.Fatalf("kept %d backups, want %d", len(got), MaxBackups)
	}
	if got[0].Path != newest {
		t.Errorf("newest backup = %s, want %s", got[0].Path, newest)
	}
	// The pruned ones are the oldest, so the survivors hold the last
	// MaxBackups bodies.
	for i, b := range got {
		want := fmt.Sprintf("sparkline_len: %d\n", total-1-i)
		if raw, _ := os.ReadFile(b.Path); string(raw) != want {
			t.Errorf("backup[%d] = %q, want %q", i, raw, want)
		}
	}
}

func TestRestoreBackupRoundTrip(t *testing.T) {
	path := isolate(t)
	advance := pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))

	writeConfigFile(t, path, validUserConfig)
	snapshot, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	advance(time.Minute)

	// Blow the config away with something else entirely.
	if _, err := SaveSafely(dropAll(), SaveOptions{AssumeYes: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) == validUserConfig {
		t.Fatal("the test did not actually replace the config")
	}
	advance(time.Minute)

	if err := RestoreBackup(snapshot); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validUserConfig {
		t.Errorf("restored config:\n%s", got)
	}

	// And the config we replaced is itself recoverable — restore is not a
	// one-way door either.
	backups, err := ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range backups {
		if raw, _ := os.ReadFile(b.Path); strings.Contains(string(raw), "watchlist:") && !strings.Contains(string(raw), "Retirement") {
			found = true
		}
	}
	if !found {
		t.Error("RestoreBackup did not back up the config it replaced")
	}

	// The restored file reloads cleanly, with the user's data.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Portfolios) != 1 || cfg.Portfolios[0].Name != "Retirement" {
		t.Errorf("restored config does not load: %+v", cfg.Portfolios)
	}
}

func TestRestoreBackupWithNoLiveConfig(t *testing.T) {
	path := isolate(t)
	pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))
	writeConfigFile(t, path, validUserConfig)
	snapshot, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if err := RestoreBackup(snapshot); err != nil {
		t.Fatalf("RestoreBackup with nothing to replace: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != validUserConfig {
		t.Errorf("restored config:\n%s", got)
	}
}

func TestRestoreMissingBackupFails(t *testing.T) {
	path := isolate(t)
	if err := RestoreBackup(path + ".bak.20200101-000000"); err == nil {
		t.Fatal("restoring a backup that is not there should fail")
	}
}

func TestRestoredConfigIs0600(t *testing.T) {
	path := isolate(t)
	pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))
	writeConfigFile(t, path, validUserConfig)
	snapshot, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(snapshot); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("restored config perm = %o, want 0600", perm)
	}
}

func TestParseBackupStamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"20260801-134530", true},
		{"20260801-134530-1", true}, // same-second disambiguator
		{"20260801", false},
		{"not-a-timestamp", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseBackupStamp(tc.in)
			if ok != tc.ok {
				t.Fatalf("parseBackupStamp(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if ok && got.IsZero() {
				t.Errorf("parseBackupStamp(%q) returned the zero time", tc.in)
			}
		})
	}
}

// Every destructive write leaves a recoverable copy, and the config dir does
// not grow without bound while doing it.
func TestRepeatedDestructiveWritesStayBounded(t *testing.T) {
	path := isolate(t)
	advance := pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))
	writeConfigFile(t, path, validUserConfig)

	for i := range MaxBackups + 3 {
		cfg := dropAll()
		cfg.SparklineLen = 60 + i
		if _, err := SaveSafely(cfg, SaveOptions{AssumeYes: true}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		advance(time.Minute)
	}
	backups, err := ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != MaxBackups {
		t.Errorf("after %d writes there are %d backups, want %d", MaxBackups+3, len(backups), MaxBackups)
	}
}

// A hundred backups inside one second is pathological; report it instead of
// looping forever looking for a free name.
func TestBackupGivesUpAfterTooManyCollisions(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, validUserConfig)
	at := time.Date(2026, 8, 1, 13, 45, 30, 0, time.Local)
	pinClock(t, at)

	base := path + backupInfix + at.Format(backupStamp)
	writeConfigFile(t, base, "x")
	for i := 1; i <= 99; i++ {
		writeConfigFile(t, fmt.Sprintf("%s-%d", base, i), "x")
	}
	if _, err := Backup(path); err == nil {
		t.Fatal("Backup should give up rather than spin")
	}
}

// A restore that cannot land must not lose the file it was replacing.
func TestRestoreBackupIntoUnwritableDirFails(t *testing.T) {
	path := isolate(t)
	pinClock(t, time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local))
	writeConfigFile(t, path, validUserConfig)
	snapshot, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	// Point the restore at a config dir that cannot be created.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, ".config"), 0o700) })

	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory does not stop mkdir")
	}
	if err := RestoreBackup(snapshot); err == nil {
		t.Fatal("RestoreBackup should fail when the config dir cannot be created")
	}
}

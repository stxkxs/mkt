package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MaxBackups caps how many timestamped backups are kept beside config.yaml.
// Every write that changes the file leaves one behind, so an unbounded count
// would slowly fill the config dir; the oldest are pruned first. Ten covers a
// bad afternoon of edits — enough history to walk back a mistake, few enough
// that `ls ~/.config/mkt` stays readable.
const MaxBackups = 10

const (
	// backupInfix separates the config filename from its backup timestamp:
	// config.yaml.bak.YYYYMMDD-HHMMSS.
	backupInfix = ".bak."
	// backupStamp is the timestamp layout, in local time so the filename
	// matches the clock the user was looking at.
	backupStamp = "20060102-150405"
)

// backupClock is indirected so tests can pin the timestamp.
var backupClock = time.Now

// BackupInfo describes one timestamped copy of the config file.
type BackupInfo struct {
	Path  string
	Taken time.Time
	Size  int64
}

// Backup copies path to a timestamped sibling (config.yaml.bak.YYYYMMDD-HHMMSS,
// mode 0600) and returns the backup's path. Backups older than the newest
// MaxBackups are pruned afterwards.
//
// The copy is written atomically, so an interrupted backup never leaves a
// truncated file that later looks like a valid restore point.
func Backup(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s for backup: %w", path, err)
	}

	base := path + backupInfix + backupClock().Format(backupStamp)
	dest := base
	// Two writes inside the same second must not clobber each other.
	for i := 1; ; i++ {
		if _, statErr := os.Lstat(dest); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if i > 99 {
			return "", fmt.Errorf("back up %s: too many backups in the same second", path)
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}

	if err := writeAtomic(dest, raw, 0o600); err != nil {
		return "", fmt.Errorf("write backup %s: %w", dest, err)
	}
	pruneBackups(path)
	return dest, nil
}

// ListBackups returns the backups of the current config file, newest first.
// A missing config dir is not an error — it just means there are none.
func ListBackups() ([]BackupInfo, error) {
	return listBackups(configPath())
}

// listBackups enumerates the backups beside path. It scans the directory
// rather than globbing so a home directory containing glob metacharacters
// cannot silently return nothing.
func listBackups(path string) ([]BackupInfo, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config dir %s: %w", dir, err)
	}

	prefix := filepath.Base(path) + backupInfix
	var out []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		taken, ok := parseBackupStamp(strings.TrimPrefix(e.Name(), prefix))
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			Path:  filepath.Join(dir, e.Name()),
			Taken: taken,
			Size:  info.Size(),
		})
	}
	// Newest first, with the path as a stable tie-break for the collision
	// suffixes that share a second.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Taken.Equal(out[j].Taken) {
			return out[i].Taken.After(out[j].Taken)
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}

// parseBackupStamp reads the timestamp off a backup suffix, tolerating the
// "-1", "-2" … disambiguators appended to same-second collisions.
func parseBackupStamp(suffix string) (time.Time, bool) {
	if len(suffix) < len(backupStamp) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(backupStamp, suffix[:len(backupStamp)], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// pruneBackups deletes all but the newest MaxBackups backups of path. Best
// effort: a backup that cannot be removed is left alone rather than failing
// the write that triggered the prune.
func pruneBackups(path string) {
	backups, err := listBackups(path)
	if err != nil || len(backups) <= MaxBackups {
		return
	}
	for _, b := range backups[MaxBackups:] {
		_ = os.Remove(b.Path)
	}
}

// RestoreBackup replaces the live config with the contents of backupPath.
// The file being replaced is itself backed up first, so a restore of the
// wrong snapshot is undoable — nothing here is a one-way door.
func RestoreBackup(backupPath string) error {
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup %s: %w", backupPath, err)
	}

	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)

	dest := configPath()
	if _, err := os.Stat(dest); err == nil {
		if _, err := Backup(dest); err != nil {
			return fmt.Errorf("back up current config before restore: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dest, err)
	}

	if err := writeAtomic(dest, raw, 0o600); err != nil {
		return fmt.Errorf("restore %s: %w", dest, err)
	}
	tightenPerms()
	return nil
}

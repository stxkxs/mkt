package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// recordBackupInfix separates a recording's filename from its backup
	// timestamp, matching the config backup naming (foo.ndjson.bak.<stamp>).
	recordBackupInfix = ".bak."
	// recordBackupStamp is the timestamp layout, in local time so the
	// filename matches the clock the user was looking at.
	recordBackupStamp = "20060102-150405"
	// maxRecordBackups caps how many superseded recordings are kept beside
	// the live one, mirroring config.MaxBackups. Recordings can be large, so
	// an unbounded count would quietly fill a disk.
	maxRecordBackups = 10
)

// recordClock is indirected so tests can pin the timestamp.
var recordClock = time.Now

// preserveRecording protects an existing MKT_RECORD target from the sink's
// truncating open.
//
// recording.NewSink opens the path with O_TRUNC, so every launch used to
// destroy the previous capture — including the long-running `mkt daemon`
// case, where the recording is the whole point. Rather than silently
// overwrite, the existing file is copied to a timestamped sibling first and
// the copy is named on stderr, so the data is recoverable and the user is
// told where it went. An empty or missing target needs no protection.
//
// Returns the backup path, or "" when nothing needed preserving.
func preserveRecording(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("stat recording %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("recording target %s is a directory", path)
	}
	if info.Size() == 0 {
		return "", nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read recording %s for backup: %w", path, err)
	}

	base := path + recordBackupInfix + recordClock().Format(recordBackupStamp)
	dest := base
	// Two launches inside the same second must not clobber each other.
	for i := 1; ; i++ {
		if _, statErr := os.Lstat(dest); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if i > 99 {
			return "", fmt.Errorf("back up recording %s: too many backups in the same second", path)
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}

	// 0o600: a recording is a fingerprint of which symbols are watched.
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		return "", fmt.Errorf("write recording backup %s: %w", dest, err)
	}
	pruneRecordBackups(path)
	return dest, nil
}

// pruneRecordBackups deletes all but the newest maxRecordBackups backups of
// path. Best effort: one that cannot be removed is left alone rather than
// failing the launch that triggered the prune.
func pruneRecordBackups(path string) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := filepath.Base(path) + recordBackupInfix
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(e.Name(), prefix)
		if len(suffix) < len(recordBackupStamp) {
			continue
		}
		if _, perr := time.ParseInLocation(recordBackupStamp, suffix[:len(recordBackupStamp)], time.Local); perr != nil {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) <= maxRecordBackups {
		return
	}
	// The stamp is lexicographically ordered, so a plain reverse sort is
	// newest-first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, n := range names[maxRecordBackups:] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

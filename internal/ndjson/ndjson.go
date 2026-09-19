// Package ndjson bounds the newline-delimited JSON files mkt appends to for
// the lifetime of a process: the triggered-alert history and the portfolio
// equity curve.
//
// Both are read with a "keep the newest N" bound and written with a plain
// append, so without compaction the file grows without limit while the
// bound quietly discards most of what was read. A daemon marking twelve
// portfolios every five minutes writes ~3,500 lines a day, all but the
// newest N of which are read at startup only to be thrown away.
package ndjson

import (
	"bufio"
	"os"
	"path/filepath"
)

// maxLineBytes bounds a single record. A line longer than this is a
// corrupt file rather than a record, and refusing to buffer it keeps a
// damaged file from exhausting memory during compaction.
const maxLineBytes = 8 << 20

// Compact rewrites path keeping only its newest keep lines.
//
// It runs only once the file has grown to twice keep, so the amortized cost
// of an append stays constant rather than file-sized. The rewrite goes
// through a temp file in the same directory and a rename, so a crash
// part-way leaves the original intact — a truncated history is worse than a
// long one. Mode 0600 is preserved: these files carry holdings and alert
// metadata.
//
// A file at or under the threshold is left untouched, and Compact reports
// no error for a file that does not exist.
func Compact(path string, keep int) error {
	if keep <= 0 {
		return nil
	}
	lines, err := readLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(lines) <= keep*2 {
		return nil
	}
	return writeLines(path, lines[len(lines)-keep:])
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func writeLines(path string, lines []string) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// On any failure the temp file is closed and removed; on success the
	// rename has already consumed it and the remove is a no-op.
	defer func() {
		if err != nil {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()

	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	for _, l := range lines {
		if _, err = w.WriteString(l + "\n"); err != nil {
			return err
		}
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

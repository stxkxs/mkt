package ndjson

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeN(t *testing.T, path string, n int) {
	t.Helper()
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, `{"i":%d}`+"\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimRight(string(b), "\n"), "\n"))
}

func TestCompactKeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.ndjson")
	writeN(t, path, 500)

	if err := Compact(path, 100); err != nil {
		t.Fatal(err)
	}
	if got := lineCount(t, path); got != 100 {
		t.Fatalf("kept %d lines, want 100", got)
	}
	b, _ := os.ReadFile(path)
	first, last := strings.Split(string(b), "\n")[0], `{"i":499}`
	if first != `{"i":400}` {
		t.Errorf("first retained line = %s, want {\"i\":400}", first)
	}
	if !strings.Contains(string(b), last) {
		t.Errorf("newest line was dropped")
	}
}

// Rewriting on every append would make a constant-time write file-sized, so
// a file inside the hysteresis band is left alone.
func TestCompactLeavesShortFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.ndjson")
	writeN(t, path, 150)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compact(path, 100); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if before.Size() != after.Size() {
		t.Fatalf("file inside the band was rewritten: %d -> %d", before.Size(), after.Size())
	}
}

// These files carry holdings and alert metadata; the rewrite must not widen
// their mode.
func TestCompactPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.ndjson")
	writeN(t, path, 500)

	if err := Compact(path, 10); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

func TestCompactMissingFileIsNotAnError(t *testing.T) {
	if err := Compact(filepath.Join(t.TempDir(), "absent"), 10); err != nil {
		t.Fatalf("missing file should be a no-op, got %v", err)
	}
}

func TestCompactZeroKeepIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.ndjson")
	writeN(t, path, 500)
	if err := Compact(path, 0); err != nil {
		t.Fatal(err)
	}
	if got := lineCount(t, path); got != 500 {
		t.Fatalf("keep=0 trimmed the file to %d lines", got)
	}
}

// A compaction that leaves a temp file behind would accumulate one per
// call in the user's config directory.
func TestCompactLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.ndjson")
	writeN(t, path, 500)

	if err := Compact(path, 10); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries after compaction, want 1", len(entries))
	}
}

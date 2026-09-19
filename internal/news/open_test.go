package news

import (
	"runtime"
	"strings"
	"sync"
	"testing"
)

func withOpenDisabled(t *testing.T, v bool) {
	t.Helper()
	prev := openDisabled.Load()
	SetOpenDisabled(v)
	t.Cleanup(func() { SetOpenDisabled(prev) })
}

// spawnRecorder stands in for the platform URL handler. A nil error is what
// exec.Cmd.Start returns once the handler binary is launched, so asserting
// on OpenURL's error alone cannot tell a refusal from a successful spawn —
// the argv is the only evidence that separates them.
type spawnRecorder struct {
	mu    sync.Mutex
	calls [][]string
}

func (s *spawnRecorder) run(name string, args ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, append([]string{name}, args...))
	return nil
}

func (s *spawnRecorder) recorded() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func withRecordedSpawn(t *testing.T) *spawnRecorder {
	t.Helper()
	rec := &spawnRecorder{}
	prev := spawn
	spawn = rec.run
	t.Cleanup(func() { spawn = prev })
	return rec
}

// Serve mode hands the news tab to a remote SSH user. Opening a headline
// there would spawn a browser on the server host, which is a process-spawn
// primitive the remote user does not otherwise have.
func TestOpenURLIsInertWhenDisabled(t *testing.T) {
	rec := withRecordedSpawn(t)
	withOpenDisabled(t, true)

	if err := OpenURL("https://example.test/story"); err != nil {
		t.Fatalf("disabled open should be a silent no-op, got %v", err)
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("the kill switch let %v reach the platform handler", got)
	}
}

// The accepted path is what the two refusals above are measured against:
// unless a permitted URL demonstrably reaches argv, a refusal proves
// nothing. The separator is load-bearing — without it a link beginning with
// a dash is read by open(1) as a flag rather than a URL.
func TestOpenURLPassesAcceptedURLToHandler(t *testing.T) {
	rec := withRecordedSpawn(t)
	withOpenDisabled(t, false)

	const link = "https://example.test/story"
	if err := OpenURL(link); err != nil {
		t.Fatalf("OpenURL(%q): %v", link, err)
	}
	got := rec.recorded()
	if len(got) != 1 {
		t.Fatalf("got %d spawns, want 1: %v", len(got), got)
	}
	want := []string{"open", "--", link}
	if runtime.GOOS == "linux" {
		want = []string{"xdg-open", link}
	}
	if len(got[0]) != len(want) {
		t.Fatalf("argv = %v, want %v", got[0], want)
	}
	for i := range want {
		if got[0][i] != want[i] {
			t.Fatalf("argv = %v, want %v", got[0], want)
		}
	}
}

// A link is feed-supplied. `open` and `xdg-open` read a leading dash as a
// flag and a non-http scheme as a handler to dispatch, so the scheme check
// runs before the URL reaches argv.
func TestOpenURLRejectsNonHTTPSchemes(t *testing.T) {
	rec := withRecordedSpawn(t)
	withOpenDisabled(t, false)
	for _, link := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"-froot",
		"",
		"ftp://example.test/payload",
		"HTTPS://example.test/story",
	} {
		err := OpenURL(link)
		if err == nil {
			t.Errorf("OpenURL(%q) was accepted", link)
			continue
		}
		if !strings.Contains(err.Error(), "refusing to open") {
			t.Errorf("OpenURL(%q): got %v", link, err)
		}
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("a refused URL still reached the platform handler: %v", got)
	}
}

package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var errBusy = errors.New("busy")

func isBusy(err error) bool { return errors.Is(err, errBusy) }

// renameAfter fails with errBusy for the first n calls, then succeeds.
func renameAfter(n int, calls *int) func(string, string) error {
	return func(string, string) error {
		*calls++
		if *calls <= n {
			return errBusy
		}
		return nil
	}
}

func TestReplaceRetriesTransientErrorsUntilTheRenameSucceeds(t *testing.T) {
	calls := 0
	var sleeps []time.Duration
	err := replace("tmp", "dst", renameAfter(4, &calls), isBusy,
		func(d time.Duration) { sleeps = append(sleeps, d) }, time.Second)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if calls != 5 {
		t.Errorf("rename called %d times, want 5", calls)
	}
	want := []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond, 8 * time.Millisecond}
	if len(sleeps) != len(want) {
		t.Fatalf("sleeps = %v, want %v", sleeps, want)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Errorf("sleep %d = %v, want %v", i, sleeps[i], want[i])
		}
	}
}

func TestReplaceGivesUpOnceTheBudgetIsSpent(t *testing.T) {
	calls := 0
	var slept time.Duration
	budget := 500 * time.Millisecond
	err := replace("tmp", "dst", renameAfter(1<<30, &calls), isBusy,
		func(d time.Duration) {
			if d > maxDelay {
				t.Errorf("single sleep %v exceeds maxDelay %v", d, maxDelay)
			}
			slept += d
		}, budget)
	if !errors.Is(err, errBusy) {
		t.Fatalf("err = %v, want the last rename error", err)
	}
	if slept < budget || slept >= budget+maxDelay {
		t.Errorf("slept %v, want at least the %v budget and less than one extra step", slept, budget)
	}
}

func TestReplaceReturnsOtherErrorsAtOnce(t *testing.T) {
	permanent := errors.New("no such directory")
	calls := 0
	err := replace("tmp", "dst",
		func(string, string) error { calls++; return permanent },
		isBusy,
		func(time.Duration) { t.Error("slept before returning a permanent error") },
		time.Second)
	if !errors.Is(err, permanent) || calls != 1 {
		t.Fatalf("err = %v after %d calls, want the permanent error after 1", err, calls)
	}
}

func TestReplaceMovesTheFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "new.tmp")
	dst := filepath.Join(dir, "file")
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(tmp, dst); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Fatalf("dst = %q, %v; want \"new\"", got, err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file still present: %v", err)
	}
}

// A reader holding the destination open is the case Replace exists for. On
// Windows the first rename attempts are refused until the reader closes; on
// other platforms the first attempt succeeds. Either way the file is
// replaced.
func TestReplaceSucceedsWhileAReaderHoldsTheDestination(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "new.tmp")
	dst := filepath.Join(dir, "file")
	if err := os.WriteFile(dst, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = reader.Close()
		close(released)
	}()
	if err := Replace(tmp, dst); err != nil {
		t.Fatalf("Replace with a reader open: %v", err)
	}
	<-released
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Fatalf("dst = %q, %v; want \"new\"", got, err)
	}
}

package alert

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleTriggered(symbol string, ts time.Time) TriggeredAlert {
	return TriggeredAlert{
		Rule:      Rule{Symbol: symbol, Condition: CondAbove, Value: 50000, Enabled: true},
		Price:     51000,
		Message:   symbol + " crossed",
		Timestamp: ts,
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.ndjson")
	h := NewHistoryFile(path, 0)

	now := time.Now().UTC()
	inputs := []TriggeredAlert{
		sampleTriggered("BTC", now),
		sampleTriggered("ETH", now.Add(time.Second)),
		sampleTriggered("AAPL", now.Add(2*time.Second)),
	}
	for _, a := range inputs {
		if err := h.Append(a); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := h.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != len(inputs) {
		t.Fatalf("got %d, want %d", len(got), len(inputs))
	}
	for i, a := range got {
		if a.Rule.Symbol != inputs[i].Rule.Symbol {
			t.Errorf("[%d] symbol: got %q want %q", i, a.Rule.Symbol, inputs[i].Rule.Symbol)
		}
		if !a.Timestamp.Equal(inputs[i].Timestamp) {
			t.Errorf("[%d] timestamp mismatch", i)
		}
	}
}

func TestHistoryTrimToMax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.ndjson")
	h := NewHistoryFile(path, 2)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = h.Append(sampleTriggered("BTC", now.Add(time.Duration(i)*time.Second)))
	}

	got, err := h.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (trimmed)", len(got))
	}
	// Should be the last two — timestamps now+3s and now+4s
	if !got[0].Timestamp.Equal(now.Add(3 * time.Second)) {
		t.Errorf("trim kept wrong tail; first timestamp = %v", got[0].Timestamp)
	}
}

func TestHistoryMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	h := NewHistoryFile(filepath.Join(dir, "nope.ndjson"), 0)
	got, err := h.LoadAll()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty for missing file, got %d", len(got))
	}
}

func TestHistoryMalformedLineSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.ndjson")
	// Write a mix of valid and garbage lines directly.
	valid, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	_, _ = valid.WriteString(`{"Rule":{"Symbol":"BTC","Condition":"above","Value":1,"Enabled":true},"Price":100,"Message":"m","Timestamp":"2026-01-01T00:00:00Z"}` + "\n")
	_, _ = valid.WriteString("not json\n")
	_, _ = valid.WriteString(`{"Rule":{"Symbol":"ETH","Condition":"above","Value":1,"Enabled":true},"Price":200,"Message":"m","Timestamp":"2026-01-01T00:00:01Z"}` + "\n")
	valid.Close()

	h := NewHistoryFile(path, 0)
	got, err := h.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("malformed line should be skipped, got %d valid", len(got))
	}
}

func TestHistoryNotifier(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.ndjson")
	h := NewHistoryFile(path, 0)
	n := NewHistoryNotifier(h)
	if n.Name() != "history" {
		t.Errorf("Name() = %q, want history", n.Name())
	}
	if err := n.Notify(context.Background(), sampleTriggered("BTC", time.Now().UTC())); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	got, _ := h.LoadAll()
	if len(got) != 1 {
		t.Fatalf("notifier should append 1 record, got %d", len(got))
	}
}

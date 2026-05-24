package portfolio

import (
	"path/filepath"
	"testing"
	"time"
)

func TestEquityRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "equity.ndjson")
	e := NewEquityFile(path, 0)

	now := time.Now().UTC()
	inputs := []EquityMark{
		{Time: now, PortfolioName: "Tech", Value: 100000},
		{Time: now.Add(time.Minute), PortfolioName: "Tech", Value: 101500},
		{Time: now.Add(time.Minute), PortfolioName: "Crypto", Value: 50000},
	}
	for _, m := range inputs {
		if err := e.Append(m); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := e.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(got) != len(inputs) {
		t.Fatalf("got %d, want %d", len(got), len(inputs))
	}
	for i := range got {
		if got[i].PortfolioName != inputs[i].PortfolioName || got[i].Value != inputs[i].Value {
			t.Errorf("[%d] mismatch: got %+v", i, got[i])
		}
	}
}

func TestEquityLoadByName(t *testing.T) {
	dir := t.TempDir()
	e := NewEquityFile(filepath.Join(dir, "e.ndjson"), 0)
	now := time.Now().UTC()
	_ = e.Append(EquityMark{Time: now, PortfolioName: "A", Value: 100})
	_ = e.Append(EquityMark{Time: now, PortfolioName: "B", Value: 200})
	_ = e.Append(EquityMark{Time: now, PortfolioName: "A", Value: 105})

	got, err := e.LoadByName()
	if err != nil {
		t.Fatalf("LoadByName: %v", err)
	}
	if len(got["A"]) != 2 || len(got["B"]) != 1 {
		t.Errorf("bucket counts wrong: A=%d B=%d", len(got["A"]), len(got["B"]))
	}
}

func TestEquityTrimToMax(t *testing.T) {
	dir := t.TempDir()
	e := NewEquityFile(filepath.Join(dir, "e.ndjson"), 2)
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = e.Append(EquityMark{Time: now.Add(time.Duration(i) * time.Second), PortfolioName: "P", Value: float64(i)})
	}
	got, _ := e.LoadAll()
	if len(got) != 2 {
		t.Fatalf("want 2 trimmed, got %d", len(got))
	}
	if got[0].Value != 3 || got[1].Value != 4 {
		t.Errorf("trim kept wrong tail: %+v", got)
	}
}

func TestEquityMissingFile(t *testing.T) {
	e := NewEquityFile(filepath.Join(t.TempDir(), "nope.ndjson"), 0)
	got, err := e.LoadAll()
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %d", len(got))
	}
}

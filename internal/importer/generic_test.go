package importer

import (
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/portfolio"
)

const sampleGeneric = `date,type,symbol,quantity,price,fee,note
2026-01-15,buy,AAPL,100,150,5,initial position
2026-02-20,sell,AAPL,50,180,5,
2026-03-10,dividend,AAPL,50,0.25,0,Q1 dividend
,malformed,,,,,
`

func TestGenericDetect(t *testing.T) {
	if !(Generic{}).Detect("date,type,symbol,quantity,price,fee,note") {
		t.Error("Detect should match well-formed header")
	}
	if (Generic{}).Detect("foo,bar,baz") {
		t.Error("Detect should reject unrelated header")
	}
}

func TestGenericParse(t *testing.T) {
	got, err := (Generic{}).Parse(strings.NewReader(sampleGeneric))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 txs (malformed skipped), got %d", len(got))
	}
	if got[0].Type != portfolio.TxBuy || got[0].Symbol != "AAPL" || got[0].Quantity != 100 {
		t.Errorf("row 0: %+v", got[0])
	}
	if got[1].Type != portfolio.TxSell || got[1].Quantity != 50 {
		t.Errorf("row 1: %+v", got[1])
	}
	if got[2].Type != portfolio.TxDividend {
		t.Errorf("row 2: want dividend, got %v", got[2].Type)
	}
}

func TestGenericMissingColumn(t *testing.T) {
	csv := "date,type,symbol\n2026-01-01,buy,X\n"
	_, err := (Generic{}).Parse(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
}

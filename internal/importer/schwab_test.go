package importer

import (
	"strings"
	"testing"

	"github.com/stxkxs/mkt/internal/portfolio"
)

const sampleSchwab = `"Date","Action","Symbol","Description","Quantity","Price","Fees & Comm","Amount"
"01/15/2026","Buy","AAPL","APPLE INC","100","$150.00","$4.95","-$15,004.95"
"02/20/2026","Sell","AAPL","APPLE INC","50","$180.00","$4.95","$8,995.05"
"03/10/2026","Reinvest Dividend","AAPL","APPLE INC","0.5","$182.00","$0.00","$91.00"
"04/01/2026","Wire Funds","","","","","","-$10,000.00"
`

func TestSchwabDetect(t *testing.T) {
	header := `"Date","Action","Symbol","Description","Quantity","Price","Fees & Comm","Amount"`
	if !(Schwab{}).Detect(header) {
		t.Error("Detect should match Schwab header")
	}
	if (Schwab{}).Detect("date,type,symbol,quantity,price") {
		t.Error("Detect should not match generic header")
	}
}

func TestSchwabParse(t *testing.T) {
	got, err := (Schwab{}).Parse(strings.NewReader(sampleSchwab))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// 4 rows; last is "Wire Funds" with no action match → skipped
	if len(got) != 3 {
		t.Fatalf("want 3 txs, got %d", len(got))
	}
	if got[0].Type != portfolio.TxBuy {
		t.Errorf("row 0 type = %v", got[0].Type)
	}
	if got[0].Quantity != 100 || got[0].Price != 150 || got[0].Fee != 4.95 {
		t.Errorf("row 0 amounts: %+v", got[0])
	}
	if got[1].Type != portfolio.TxSell {
		t.Errorf("row 1 type = %v", got[1].Type)
	}
	if got[2].Type != portfolio.TxDividend {
		t.Errorf("row 2 type = %v", got[2].Type)
	}
}

func TestDetectChoosesRightFormat(t *testing.T) {
	gen := `date,type,symbol,quantity,price
2026-01-01,buy,AAPL,1,100
`
	sch := `"Date","Action","Symbol","Description","Quantity","Price","Fees & Comm","Amount"
"01/15/2026","Buy","AAPL","APPLE INC","100","$150.00","$4.95","-$15,004.95"
`
	tests := []struct {
		name string
		body string
		want string
	}{
		{"generic", gen, "generic"},
		{"schwab", sch, "schwab"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, _, err := Detect(strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if f.Name() != tc.want {
				t.Errorf("got %q want %q", f.Name(), tc.want)
			}
		})
	}
}

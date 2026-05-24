package importer

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"

	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/portfolio"
)

// Schwab parses Charles Schwab transaction CSV exports.
// Columns: "Date","Action","Symbol","Description","Quantity","Price","Fees & Comm","Amount".
type Schwab struct{}

func (Schwab) Name() string { return "schwab" }

func (Schwab) Detect(header string) bool {
	cols := splitCSVHeader(header)
	wanted := []string{"date", "action", "symbol", "quantity", "price"}
	hits := 0
	for _, w := range wanted {
		if hasCol(cols, w) {
			hits++
		}
	}
	// Schwab is the only format here that uses "action" + "fees & comm".
	if hasCol(cols, "action") && hasCol(cols, "fees & comm") {
		return true
	}
	return hits >= 4 && hasCol(cols, "action")
}

func (Schwab) Parse(r io.Reader) ([]portfolio.Transaction, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("schwab: read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	idx := indexHeader(rows[0])
	for _, want := range []string{"date", "action", "symbol", "quantity", "price"} {
		if _, ok := idx[want]; !ok {
			return nil, fmt.Errorf("schwab: missing column %q", want)
		}
	}

	var out []portfolio.Transaction
	for i, row := range rows[1:] {
		tx, ok := parseSchwabRow(row, idx)
		if !ok {
			log.Printf("schwab: row %d: skipped", i+2)
			continue
		}
		out = append(out, tx)
	}
	return out, nil
}

func parseSchwabRow(row []string, idx map[string]int) (portfolio.Transaction, bool) {
	get := func(k string) string {
		i, ok := idx[k]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	action := strings.ToLower(get("action"))
	var txType portfolio.TxType
	switch {
	case strings.HasPrefix(action, "buy"):
		txType = portfolio.TxBuy
	case strings.HasPrefix(action, "sell"):
		txType = portfolio.TxSell
	case strings.Contains(action, "dividend") || strings.Contains(action, "reinvest"):
		txType = portfolio.TxDividend
	default:
		return portfolio.Transaction{}, false
	}
	qty, err := strconv.ParseFloat(stripNumeric(get("quantity")), 64)
	if err != nil {
		return portfolio.Transaction{}, false
	}
	price, err := strconv.ParseFloat(stripNumeric(get("price")), 64)
	if err != nil {
		return portfolio.Transaction{}, false
	}
	fee, _ := strconv.ParseFloat(stripNumeric(get("fees & comm")), 64)
	return portfolio.Transaction{
		Type:     txType,
		Symbol:   strings.ToUpper(get("symbol")),
		Quantity: qty,
		Price:    price,
		Time:     config.ParseTime(get("date")),
		Fee:      fee,
		Note:     get("description"),
	}, true
}

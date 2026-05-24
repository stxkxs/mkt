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

// Generic parses a minimal CSV format with columns:
//
//	date,type,symbol,quantity,price,fee,note
//
// `type` is buy|sell|dividend (case-insensitive). `note` and `fee` are
// optional. Headers must match these names (any order).
type Generic struct{}

func (Generic) Name() string { return "generic" }

func (Generic) Detect(header string) bool {
	cols := splitCSVHeader(header)
	required := []string{"date", "type", "symbol", "quantity", "price"}
	for _, want := range required {
		if !hasCol(cols, want) {
			return false
		}
	}
	return true
}

func (Generic) Parse(r io.Reader) ([]portfolio.Transaction, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate variable row lengths
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("generic: read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	idx := indexHeader(rows[0])
	required := []string{"date", "type", "symbol", "quantity", "price"}
	for _, want := range required {
		if _, ok := idx[want]; !ok {
			return nil, fmt.Errorf("generic: missing column %q", want)
		}
	}

	var out []portfolio.Transaction
	for i, row := range rows[1:] {
		tx, ok := parseGenericRow(row, idx)
		if !ok {
			log.Printf("generic: row %d: skipped", i+2)
			continue
		}
		out = append(out, tx)
	}
	return out, nil
}

func parseGenericRow(row []string, idx map[string]int) (portfolio.Transaction, bool) {
	get := func(k string) string {
		i, ok := idx[k]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	typeStr := strings.ToLower(get("type"))
	var txType portfolio.TxType
	switch typeStr {
	case "buy":
		txType = portfolio.TxBuy
	case "sell":
		txType = portfolio.TxSell
	case "dividend", "div":
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
	fee, _ := strconv.ParseFloat(stripNumeric(get("fee")), 64) // optional
	return portfolio.Transaction{
		Type:     txType,
		Symbol:   strings.ToUpper(get("symbol")),
		Quantity: qty,
		Price:    price,
		Time:     config.ParseTime(get("date")),
		Fee:      fee,
		Note:     get("note"),
	}, true
}

func splitCSVHeader(h string) []string {
	cr := csv.NewReader(strings.NewReader(h))
	cr.FieldsPerRecord = -1
	row, _ := cr.Read()
	for i := range row {
		row[i] = strings.ToLower(strings.TrimSpace(row[i]))
	}
	return row
}

func hasCol(cols []string, want string) bool {
	for _, c := range cols {
		if c == want {
			return true
		}
	}
	return false
}

func indexHeader(headerRow []string) map[string]int {
	idx := make(map[string]int, len(headerRow))
	for i, h := range headerRow {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return idx
}

func stripNumeric(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "(", "-") // negatives in parens
	s = strings.ReplaceAll(s, ")", "")
	return s
}

// Package importer parses broker-export CSV files into portfolio
// transactions. New broker formats implement the Format interface.
package importer

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/stxkxs/mkt/internal/portfolio"
)

// Format parses one broker / generic CSV format.
type Format interface {
	Name() string
	// Detect returns true if the first non-empty header line of the file
	// matches this format's expected schema.
	Detect(headerLine string) bool
	// Parse reads the entire CSV and returns the recognized transactions.
	// Malformed rows should be skipped (with a log) rather than aborting
	// the whole file; format-level errors (unknown columns, IO failures)
	// return an error.
	Parse(r io.Reader) ([]portfolio.Transaction, error)
}

// All returns every registered format.
func All() []Format {
	return []Format{Generic{}, Schwab{}}
}

// ByName returns the format with the given name, or nil.
func ByName(name string) Format {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, f := range All() {
		if strings.EqualFold(f.Name(), name) {
			return f
		}
	}
	return nil
}

// ErrUnknownFormat means Detect failed for every registered format.
var ErrUnknownFormat = errors.New("importer: no registered format matched the CSV header")

// Detect reads the first non-empty line of r (consuming it) and returns
// the matching format. The returned reader is positioned at the start of
// the next line — callers should pass it to Parse without seeking.
//
// Because we read the first line to inspect it, the caller cannot reuse
// the original io.Reader; we re-emit the consumed header back into the
// stream via a multiReader so Parse sees the whole file.
func Detect(r io.Reader) (Format, io.Reader, error) {
	br := bufio.NewReader(r)
	var header string
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(trimmed) != "" {
			header = trimmed
			// Push the line back into the stream by prepending it.
			combined := io.MultiReader(strings.NewReader(header+"\n"), br)
			for _, f := range All() {
				if f.Detect(header) {
					return f, combined, nil
				}
			}
			return nil, combined, fmt.Errorf("%w: %q", ErrUnknownFormat, header)
		}
		if err != nil {
			if err == io.EOF {
				return nil, nil, fmt.Errorf("importer: empty file")
			}
			return nil, nil, fmt.Errorf("importer: %w", err)
		}
	}
}

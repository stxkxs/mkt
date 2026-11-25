package symbol

import (
	"reflect"
	"testing"
)

func TestCanonicalSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil stays nil", nil, nil},
		{"nothing survives is nil", []string{"", "  "}, nil},
		{"canonicalizes", []string{"btc", "aapl"}, []string{"BTC-USD", "AAPL"}},
		{"dedupes across spellings", []string{"btc", "BTC-USD", "BTCUSDT"}, []string{"BTC-USD"}},
		{"dedupes across case", []string{"aapl", "AAPL", " aapl "}, []string{"AAPL"}},
		{"drops blanks", []string{"AAPL", "", "  "}, []string{"AAPL"}},
		{"preserves first-seen order", []string{"c", "b", "a", "b"}, []string{"C", "B", "A"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalSet(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("CanonicalSet(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

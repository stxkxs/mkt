package format

import "testing"

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "AAPL", 10, "AAPL"},
		{"exact", "AAPL", 4, "AAPL"},
		{"cut", "Berkshire Hathaway", 9, "Berkshir…"},
		{"zero", "AAPL", 0, ""},
		{"negative", "AAPL", -1, ""},
		{"one", "AAPL", 1, "…"},
		{"multibyte", "Société Générale", 8, "Société…"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.in, tt.max); got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestViewportStart(t *testing.T) {
	tests := []struct {
		name                   string
		cursor, total, visible int
		want                   int
	}{
		{"all fit", 3, 5, 10, 0},
		{"exactly fit", 9, 10, 10, 0},
		{"cursor at top", 0, 20, 5, 0},
		{"cursor inside first window", 4, 20, 5, 0},
		{"cursor past window", 7, 20, 5, 3},
		{"cursor at end", 19, 20, 5, 15},
		{"zero visible", 5, 20, 0, 0},
		{"empty list", 0, 0, 5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ViewportStart(tt.cursor, tt.total, tt.visible); got != tt.want {
				t.Errorf("ViewportStart(%d, %d, %d) = %d, want %d",
					tt.cursor, tt.total, tt.visible, got, tt.want)
			}
		})
	}
}

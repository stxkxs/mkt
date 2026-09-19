package textsafe

import (
	"strings"
	"testing"
	"unicode"
)

// unicodeIsHostile reports the runes Clean promises to remove: anything
// that can drive the terminal, and anything that can reorder what is shown
// relative to what is stored.
func unicodeIsHostile(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

func TestCleanStripsANSI(t *testing.T) {
	// A status body that repositions the cursor and clears the screen.
	in := "rate limited \x1b[2J\x1b[H\x1b[31mBUY NOW\x1b[0m"
	got := Clean(in)
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("ESC survived: %q", got)
	}
	if got != "rate limited [2J[H[31mBUY NOW[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanDropsBidiOverrides(t *testing.T) {
	// U+202E renders the following text right-to-left, so a stored string
	// can display as something other than what it is.
	got := Clean("gpj.exe\u202etxt")
	if strings.ContainsRune(got, '\u202e') {
		t.Fatalf("bidi override survived: %q", got)
	}
}

func TestCleanFoldsWhitespaceRuns(t *testing.T) {
	if got := Clean("a\t\t\nb   \r\nc"); got != "a b c" {
		t.Fatalf("got %q, want %q", got, "a b c")
	}
}

func TestCleanRepairsInvalidUTF8(t *testing.T) {
	got := Clean("ok\xff\xfe")
	if !strings.HasPrefix(got, "ok") {
		t.Fatalf("got %q", got)
	}
	for _, r := range got {
		if r == '\ufffd' {
			return
		}
	}
	t.Fatalf("invalid bytes were not replaced: %q", got)
}

func TestCleanIsIdempotent(t *testing.T) {
	in := "x\x1b[0m \u200b  y\n"
	once := Clean(in)
	if twice := Clean(once); twice != once {
		t.Fatalf("not idempotent: %q then %q", once, twice)
	}
}

func TestTruncateCountsRunes(t *testing.T) {
	// Four runes, twelve bytes: a byte-counting truncate would split one.
	got := Truncate("日本語です", 4)
	if got != "日本語で…" {
		t.Fatalf("got %q", got)
	}
	if Truncate("abc", 10) != "abc" {
		t.Fatal("short string was modified")
	}
	if Truncate("abc", 0) != "" {
		t.Fatal("non-positive n should yield empty")
	}
}

func FuzzClean(f *testing.F) {
	f.Add("plain")
	f.Add("\x1b[2Jwipe")
	f.Add("\u202ereorder")
	f.Add("\xff\xfe")
	f.Fuzz(func(t *testing.T, s string) {
		got := Clean(s)
		for _, r := range got {
			if unicodeIsHostile(r) {
				t.Fatalf("Clean left a hostile rune U+%04X in %q", r, got)
			}
		}
		if twice := Clean(got); twice != got {
			t.Fatalf("Clean is not idempotent on %q", s)
		}
	})
}

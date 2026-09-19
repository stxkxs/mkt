// Package textsafe strips terminal-hostile characters out of strings that
// originate outside this process and end up somewhere a control character
// carries meaning — a Bubbletea frame, an on-disk history line, or an MCP
// tool result an agent reads as context.
//
// It is the permissive half of a pair. A field mkt defines the contract for
// (an inbound webhook body) is validated and rejected on any control
// character, because a caller that sends one is violating the contract and
// can be told so. A string mkt merely relays — an upstream error body, an
// RSS headline — has no contract to enforce and no caller to reject, so it
// is sanitized to something printable instead of discarded.
package textsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Clean returns s with everything that can move a terminal cursor or
// disguise the rendered text removed.
//
// Tabs, newlines and carriage returns fold to spaces; any other control
// character (ESC included, so no ANSI/CSI/OSC sequence survives) and any
// Unicode format character (bidi overrides render text in an order other
// than the one stored) are dropped. Invalid UTF-8 bytes become U+FFFD
// rather than failing, since the caller has nothing to fall back to. Runs
// of whitespace collapse so a payload cannot pad a line out to the width of
// the terminal.
func Clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Truncate shortens s to at most n runes, appending an ellipsis when it
// cuts. Counting runes rather than bytes keeps a multi-byte character from
// being split into an invalid fragment.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n]) + "…"
}

package format

import (
	"unicode"
	"unicode/utf8"
)

// AppendTyped appends the printable characters of a key press's text to s.
// The text is what the key typed, not the key's name: a space bar press is
// named "space" but types " ", and a named key such as "up" types nothing.
func AppendTyped(s, text string) string {
	for _, r := range text {
		if unicode.IsPrint(r) && !unicode.IsControl(r) {
			s += string(r)
		}
	}
	return s
}

// DropLastRune removes the last character of s, whole: trimming a byte
// would leave half of a multi-byte character behind.
func DropLastRune(s string) string {
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

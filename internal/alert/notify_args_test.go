package alert

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// hostileValues are the inputs a symbol or an alert message could carry into
// a helper's own script language. Each entry must survive interpolation as
// data: the literal a helper parses has to read back byte for byte, and the
// program around it has to keep the shape the builder gave it.
var hostileValues = []struct {
	name string
	in   string
}{
	{"plain", "AAPL crossed 190.00"},
	{"double quote", `he said "buy"`},
	{"single quote", "it's above 190"},
	{"backslash", `C:\Users\mkt\alerts`},
	{"trailing backslash", `ends with \`},
	{"dollar", "$HOME is $(id) and ${PATH}"},
	{"newline", "line one\nline two"},
	{"carriage return", "line one\r\nline two"},
	{"tab", "col\tcol"},
	{"backtick", "a `b` c"},
	{"semicolon", "a; rm -rf /"},
	{"applescript break", `" & (do shell script "id") & "`},
	{"powershell break", `'; Start-Process calc.exe; '`},
	{"typographic quote", "it\u2019s \u2018quoted\u2019 \u201aoddly\u201b"},
	{"leading dash", "--help"},
	{"mixed", "a\"b'c\\d\ne$f\tg\u2019h"},
	{"empty", ""},
	{"unicode", "\u20ac 1.234,56 \u2014 \u65e5\u672c\u8a9e"},
}

// TestEscapeAppleScriptRoundTrips pins the escaping by reading the literal
// back with AppleScript's own rules: the value the helper parses is the
// value that was interpolated, and no value opens a second statement.
func TestEscapeAppleScriptRoundTrips(t *testing.T) {
	for _, tc := range hostileValues {
		t.Run(tc.name, func(t *testing.T) {
			args := appleScriptNotifyArgs("mkt Alert: "+tc.in, tc.in)
			if len(args) != 2 || args[0] != "-e" {
				t.Fatalf("argv = %q, want [-e <script>]", args)
			}
			script := args[1]
			if strings.ContainsAny(script, "\n\r") {
				t.Fatalf("script carries a raw newline: %q", script)
			}
			lits := appleScriptLiterals(t, script)
			if len(lits) != 2 {
				t.Fatalf("literal count = %d (%q), want 2", len(lits), lits)
			}
			if lits[0] != tc.in {
				t.Errorf("message literal = %q, want %q", lits[0], tc.in)
			}
			if want := "mkt Alert: " + tc.in; lits[1] != want {
				t.Errorf("title literal = %q, want %q", lits[1], want)
			}
			if n := strings.Count(script, "display notification"); n != 1 {
				t.Errorf("statement count = %d, want 1: %q", n, script)
			}
		})
	}
}

// TestQuotePowerShellRoundTrips pins the escaping by reading the literal
// back with PowerShell's own rules for a single-quoted string.
func TestQuotePowerShellRoundTrips(t *testing.T) {
	for _, tc := range hostileValues {
		t.Run(tc.name, func(t *testing.T) {
			quoted := quotePowerShell(tc.in)
			got, rest := readPowerShellLiteral(t, quoted)
			if got != tc.in {
				t.Errorf("literal = %q, want %q", got, tc.in)
			}
			if rest != "" {
				t.Errorf("literal left %q outside the quotes", rest)
			}
		})
	}
}

// TestPowerShellToastArgs pins the toast program's shape: the values land in
// the two text nodes and nowhere else, whatever they contain.
func TestPowerShellToastArgs(t *testing.T) {
	for _, tc := range hostileValues {
		t.Run(tc.name, func(t *testing.T) {
			title := "mkt Alert: " + tc.in
			args := powerShellToastArgs(title, tc.in)
			if len(args) != 6 {
				t.Fatalf("argv = %q, want 6 elements", args)
			}
			if args[len(args)-2] != "-Command" {
				t.Fatalf("argv[%d] = %q, want -Command", len(args)-2, args[len(args)-2])
			}
			for _, flag := range []string{"-NoProfile", "-NonInteractive"} {
				if !slices.Contains(args[:len(args)-1], flag) {
					t.Errorf("argv %q is missing %s", args, flag)
				}
			}
			lits := powerShellLiterals(t, args[len(args)-1])
			want := []string{"Stop", "text", title, tc.in, "mkt"}
			if len(lits) != len(want) {
				t.Fatalf("literals = %q, want %d of them", lits, len(want))
			}
			for i := range want {
				if lits[i] != want[i] {
					t.Errorf("literal %d = %q, want %q", i, lits[i], want[i])
				}
			}
		})
	}
}

// TestNotifySendArgs pins that notify-send receives the values as argv
// elements, unaltered, behind the -- that ends option parsing.
func TestNotifySendArgs(t *testing.T) {
	for _, tc := range hostileValues {
		t.Run(tc.name, func(t *testing.T) {
			title := "mkt Alert: " + tc.in
			args := notifySendArgs(title, tc.in)
			want := []string{"--", title, tc.in}
			if len(args) != len(want) {
				t.Fatalf("argv = %q, want %q", args, want)
			}
			for i := range want {
				if args[i] != want[i] {
					t.Errorf("argv[%d] = %q, want %q", i, args[i], want[i])
				}
			}
		})
	}
}

// TestEscapeAppleScriptEscapes pins the individual escape sequences, so a
// round trip through a matching bug in the reader cannot hide a change.
func TestEscapeAppleScriptEscapes(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{`a"b`, `a\"b`},
		{`a\b`, `a\\b`},
		{"a\nb", `a\nb`},
		{"a\rb", `a\rb`},
		{"a\tb", `a\tb`},
		{`\"`, `\\\"`},
		{"'", "'"},
		{"$PATH", "$PATH"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := escapeAppleScript(tc.in); got != tc.want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestQuotePowerShellDoubling pins the doubled quote characters, including
// the typographic variants PowerShell accepts as delimiters.
func TestQuotePowerShellDoubling(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"it's", "'it''s'"},
		{`C:\x`, `'C:\x'`},
		{"$x", "'$x'"},
		{`"x"`, `'"x"'`},
		{"\u2019", "'\u2019\u2019'"},
		{"\u2018", "'\u2018\u2018'"},
		{"\u201a", "'\u201a\u201a'"},
		{"\u201b", "'\u201b\u201b'"},
		{"", "''"},
	}
	for _, tc := range cases {
		if got := quotePowerShell(tc.in); got != tc.want {
			t.Errorf("quotePowerShell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRunNotifyHelperMissingBinary pins that an absent helper is a reportable
// error rather than a panic. Resolution fails before any process is started.
func TestRunNotifyHelperMissingBinary(t *testing.T) {
	err := runNotifyHelper(context.Background(), "mkt-no-such-notify-helper", "--", "t", "m")
	if err == nil {
		t.Fatal("missing helper returned no error")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap exec.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "mkt-no-such-notify-helper") {
		t.Errorf("err = %v, want it to name the helper", err)
	}
}

// TestRunNotifyHelperReturnsOnDeadline pins that the bound covers the whole
// call. The helper leaves a child holding the stderr pipe open after the
// deadline kills it, which is the shape that keeps the copy from reaching
// EOF; a call that outlasts the bound parks the desktop destination's one
// delivery goroutine for the life of the process.
func TestRunNotifyHelperReturnsOnDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- runNotifyHelper(ctx, "/bin/sh", "-c", "sleep 120 & exec sleep 120") }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a killed helper returned no error")
		}
	case <-time.After(notifyHelperTimeout + helperWaitDelay + 2*time.Second):
		t.Fatal("runNotifyHelper outlasted its deadline")
	}
}

// TestTrimHelperOutput pins the bound on helper stderr reaching an error.
func TestTrimHelperOutput(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"whitespace", "  \n\t ", ""},
		{"single line", " boom ", "boom"},
		{"first line only", "boom\nstack\ntrace", "boom"},
		{"crlf", "boom\r\nmore", "boom"},
		{"bounded", strings.Repeat("x", helperStderrLimit+50), strings.Repeat("x", helperStderrLimit)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimHelperOutput(tc.in); got != tc.want {
				t.Errorf("trimHelperOutput(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrimHelperOutputKeepsValidUTF8 pins that truncation never leaves half
// a rune in an error message.
func TestTrimHelperOutputKeepsValidUTF8(t *testing.T) {
	in := strings.Repeat("\u65e5", helperStderrLimit)
	got := trimHelperOutput(in)
	if len(got) > helperStderrLimit {
		t.Errorf("len = %d, want <= %d", len(got), helperStderrLimit)
	}
	if !strings.HasPrefix(in, got) {
		t.Errorf("result %q is not a prefix of the input", got)
	}
	for i, r := range got {
		if r == '\uFFFD' {
			t.Fatalf("invalid rune at byte %d of %q", i, got)
		}
	}
}

// TestDesktopNotifierName pins the destination name the engine logs under.
func TestDesktopNotifierName(t *testing.T) {
	if got := NewDesktopNotifier().Name(); got != "desktop" {
		t.Errorf("Name() = %q, want %q", got, "desktop")
	}
}

// TestDesktopNotifierImplementsNotifier pins the interface the engine holds.
func TestDesktopNotifierImplementsNotifier(t *testing.T) {
	var _ Notifier = NewDesktopNotifier()
}

// appleScriptLiterals reads every string literal in script using
// AppleScript's rules, and fails the test on anything AppleScript would
// reject — an unterminated literal, an unknown escape, a raw newline.
func appleScriptLiterals(t *testing.T, script string) []string {
	t.Helper()
	var out []string
	for i := 0; i < len(script); {
		if script[i] != '"' {
			i++
			continue
		}
		i++
		var b strings.Builder
		closed := false
		for i < len(script) {
			c := script[i]
			switch {
			case c == '\\':
				if i+1 >= len(script) {
					t.Fatalf("trailing backslash in %q", script)
				}
				esc, ok := map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', '\\': '\\', '"': '"'}[script[i+1]]
				if !ok {
					t.Fatalf("unknown escape \\%c in %q", script[i+1], script)
				}
				b.WriteByte(esc)
				i += 2
			case c == '"':
				i++
				closed = true
			case c == '\n' || c == '\r':
				t.Fatalf("raw newline inside a literal in %q", script)
			default:
				b.WriteByte(c)
				i++
			}
			if closed {
				break
			}
		}
		if !closed {
			t.Fatalf("unterminated literal in %q", script)
		}
		out = append(out, b.String())
	}
	return out
}

// powerShellLiterals reads every single-quoted literal in script using
// PowerShell's rules.
func powerShellLiterals(t *testing.T, script string) []string {
	t.Helper()
	var out []string
	for {
		i := strings.IndexFunc(script, isPowerShellQuote)
		if i < 0 {
			return out
		}
		value, rest := readPowerShellLiteral(t, script[i:])
		out = append(out, value)
		script = rest
	}
}

// readPowerShellLiteral reads the single-quoted literal that s opens with and
// returns its value and whatever follows the closing quote.
func readPowerShellLiteral(t *testing.T, s string) (string, string) {
	t.Helper()
	rs := []rune(s)
	if len(rs) == 0 || !isPowerShellQuote(rs[0]) {
		t.Fatalf("literal does not open with a quote: %q", s)
	}
	var b strings.Builder
	for i := 1; i < len(rs); i++ {
		if !isPowerShellQuote(rs[i]) {
			b.WriteRune(rs[i])
			continue
		}
		if i+1 < len(rs) && rs[i+1] == rs[i] {
			b.WriteRune(rs[i])
			i++
			continue
		}
		return b.String(), string(rs[i+1:])
	}
	t.Fatalf("unterminated literal in %q", s)
	return "", ""
}

package alert

import "strings"

// appleScriptNotifyArgs builds the osascript argv for a desktop notification.
// The single -e element is an AppleScript program rather than a shell word,
// so the two interpolated values are escaped for AppleScript.
func appleScriptNotifyArgs(title, message string) []string {
	script := `display notification "` + escapeAppleScript(message) +
		`" with title "` + escapeAppleScript(title) + `"`
	return []string{"-e", script}
}

// escapeAppleScript renders s as the body of an AppleScript string literal.
// AppleScript has no raw-string form and rejects a literal newline inside a
// quoted literal, so a control character becomes its escape sequence; any
// character left unescaped would close the literal and let the rest of the
// value be read as AppleScript statements.
func escapeAppleScript(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// notifySendArgs builds the notify-send argv. Both values travel as argv
// elements, so they need no escaping; -- terminates option parsing so a
// value beginning with - stays a value.
func notifySendArgs(title, message string) []string {
	return []string{"--", title, message}
}

// powerShellToastArgs builds the powershell.exe argv for a Windows toast.
// -NoProfile keeps a user profile from running first and -NonInteractive
// keeps a prompting cmdlet from parking the process until the deadline.
func powerShellToastArgs(title, message string) []string {
	return []string{
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-Command", powerShellToastScript(title, message),
	}
}

// powerShellToastScript builds the PowerShell program that raises the toast.
// CreateTextNode carries each value as an XML text node, so the value needs
// no XML escaping of its own; the quoting it does need is PowerShell's.
func powerShellToastScript(title, message string) string {
	const (
		manager  = `[Windows.UI.Notifications.ToastNotificationManager]`
		toast    = `[Windows.UI.Notifications.ToastNotification]`
		template = `[Windows.UI.Notifications.ToastTemplateType]::ToastText02`
	)
	return strings.Join([]string{
		`$ErrorActionPreference = 'Stop'`,
		`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null`,
		`$doc = ` + manager + `::GetTemplateContent(` + template + `)`,
		`$text = $doc.GetElementsByTagName('text')`,
		`$text.Item(0).AppendChild($doc.CreateTextNode(` + quotePowerShell(title) + `)) > $null`,
		`$text.Item(1).AppendChild($doc.CreateTextNode(` + quotePowerShell(message) + `)) > $null`,
		manager + `::CreateToastNotifier('mkt').Show(` + toast + `::new($doc))`,
	}, "\n")
}

// quotePowerShell renders s as a complete PowerShell single-quoted literal,
// delimiters included. PowerShell expands neither $ nor a backslash escape
// inside one, and reads a doubled quote as one literal quote character.
func quotePowerShell(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	b.WriteByte('\'')
	for _, r := range s {
		b.WriteRune(r)
		if isPowerShellQuote(r) {
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// isPowerShellQuote reports whether r closes a PowerShell single-quoted
// literal. PowerShell accepts four typographic quotes wherever it accepts
// U+0027, so a value carrying one of them closes the literal just as an
// apostrophe does.
func isPowerShellQuote(r rune) bool {
	switch r {
	case '\'', '‘', '’', '‚', '‛':
		return true
	}
	return false
}

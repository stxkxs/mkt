package alert

import "context"

// deliverDesktop posts a Windows toast through powershell.exe, which reaches
// the WinRT notification API without a compiled Windows dependency.
func deliverDesktop(ctx context.Context, title, message string) error {
	return runNotifyHelper(ctx, "powershell", powerShellToastArgs(title, message)...)
}

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stxkxs/mkt/internal/config"
	"github.com/stxkxs/mkt/internal/symbol"
	"github.com/stxkxs/mkt/internal/tui/theme"
	"gopkg.in/yaml.v3"
)

func init() {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := config.LoadWithResult()
			if err != nil {
				return err
			}
			warnDegradedConfig(cmd.ErrOrStderr(), res)
			data, err := yaml.Marshal(res.Config)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(data))
			if res.Degraded {
				return errSilent
			}
			return nil
		},
	}

	setCmd := &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := config.LoadWithResult()
			if err != nil {
				return err
			}
			cfg := res.Config

			switch args[0] {
			case "poll_interval":
				if d, err := time.ParseDuration(args[1]); err != nil || d <= 0 {
					return fmt.Errorf("invalid poll_interval %q: must be a positive duration (e.g. 15s, 1m)", args[1])
				}
				cfg.PollInterval = args[1]
			case "theme":
				if !validTheme(args[1]) {
					return fmt.Errorf("unknown theme %q (valid: %v)", args[1], theme.ThemeNames)
				}
				cfg.Theme = args[1]
			default:
				return fmt.Errorf("unknown config key: %s", args[0])
			}

			if err := saveConfig(cmd, cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓ set %s = %s\n", args[0], args[1])
			return nil
		},
	}

	addSymbolCmd := &cobra.Command{
		Use:   "add [symbol...]",
		Short: "Add symbols to watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := config.LoadWithResult()
			if err != nil {
				return err
			}
			cfg := res.Config
			// Messages are buffered rather than printed as we go: the write
			// can still be refused, and "Added TSLA" above "your file has NOT
			// been modified" would be a lie.
			var msgs []string
			for _, sym := range args {
				canon := symbol.Canonical(sym)
				if canon == "" {
					return fmt.Errorf("not a usable symbol: %q", sym)
				}
				if cfg.AddSymbol(sym) {
					msgs = append(msgs, fmt.Sprintf("  ✓ added %s", describeSymbolArg(sym, canon)))
				} else {
					msgs = append(msgs, fmt.Sprintf("  · %s already in watchlist", canon))
				}
			}
			if err := saveConfig(cmd, cfg); err != nil {
				return err
			}
			for _, m := range msgs {
				fmt.Fprintln(cmd.OutOrStdout(), m)
			}
			return nil
		},
	}

	removeSymbolCmd := &cobra.Command{
		Use:   "remove [symbol...]",
		Short: "Remove symbols from watchlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := config.LoadWithResult()
			if err != nil {
				return err
			}
			cfg := res.Config
			var msgs []string
			for _, sym := range args {
				canon := symbol.Canonical(sym)
				if cfg.RemoveSymbol(sym) {
					msgs = append(msgs, fmt.Sprintf("  ✓ removed %s", canon))
				} else {
					msgs = append(msgs, fmt.Sprintf("  · %s not in watchlist", canon))
				}
			}
			if err := saveConfig(cmd, cfg); err != nil {
				return err
			}
			for _, m := range msgs {
				fmt.Fprintln(cmd.OutOrStdout(), m)
			}
			return nil
		},
	}

	configCmd.AddCommand(showCmd, setCmd, addSymbolCmd, removeSymbolCmd, validateCommand(), repairCommand())
	addWriteSafetyFlags(configCmd)
	rootCmd.AddCommand(configCmd)
}

// describeSymbolArg names a symbol the way the user typed it, appending the
// canonical spelling when normalization changed it — so `mkt config add btc`
// says "BTC-USD (from btc)" rather than silently storing something else.
func describeSymbolArg(typed, canonical string) string {
	if strings.EqualFold(strings.TrimSpace(typed), canonical) {
		return canonical
	}
	return fmt.Sprintf("%s (normalized from %q)", canonical, typed)
}

// ─────────────────────────────── write safety ───────────────────────────────

// addWriteSafetyFlags registers the two escape hatches shared by every
// command that can write config.yaml. They are persistent so they work
// both on the group (`mkt config --yes add TSLA`) and on the leaf
// (`mkt config add TSLA --yes`).
func addWriteSafetyFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool("force", false, "write even when config.yaml on disk does not parse; implies --yes (a backup is taken first)")
	cmd.PersistentFlags().BoolP("yes", "y", false, "skip the confirmation prompt when a write drops data; required for unattended use")
}

// saveOptions reads the write-safety flags off cmd. A command that never
// registered them gets the paranoid defaults, which is the right answer.
func saveOptions(cmd *cobra.Command) config.SaveOptions {
	force, _ := cmd.Flags().GetBool("force")
	yes, _ := cmd.Flags().GetBool("yes")

	// Leave In nil when it is the real stdin so SaveSafely applies its own
	// is-there-a-terminal check. Handing it os.Stdin unconditionally would
	// defeat that and make an unattended `mkt config set` in a pipeline block
	// forever on a prompt nobody can answer — the exact hang the ReasonNoPrompt
	// refusal exists to prevent. Tests (and any future non-stdin caller)
	// substitute a reader and get the prompt.
	var in io.Reader
	if r := cmd.InOrStdin(); r != os.Stdin {
		in = r
	}
	return config.SaveOptions{
		Force: force,
		// --force is the "I know what this costs, do it" hatch, so it implies
		// --yes: a user who has already been shown the refusal and chosen to
		// override it should not then be asked the same question again.
		AssumeYes: yes || force,
		In:        in,
		Out:       cmd.ErrOrStderr(),
	}
}

// warnDegradedConfig prints the "your file did not parse, these are
// defaults" banner. Every read-only command shows it: the whole point of the
// bug this fixes is that a broken file looked healthy.
//
// Write commands deliberately do not call it before writing. Every write
// path ends in either a refusal or a removal report, and both already name
// the parse failure, its line and what it costs — the banner as well says
// the same thing twice and pushes the part that matters off the top of the
// screen. A write command that can finish *without* writing (a dry run) does
// call it, because on that path nothing else would.
func warnDegradedConfig(w io.Writer, res *config.LoadResult) {
	if res == nil || !res.Degraded {
		return
	}
	fmt.Fprintf(w, "\n  ⚠  %s did not parse%s\n", tildePath(res.Path), lineSuffix(res.Line))
	if detail := parseDetail(res.Err); detail != "" {
		fmt.Fprintf(w, "       %s\n", detail)
	}
	fmt.Fprintf(w, "     mkt is using built-in defaults, NOT your settings, and will refuse to write.\n")
	fmt.Fprintf(w, "     Fix the file, or recover an earlier copy with: mkt config repair --list\n\n")
}

// saveConfig persists cfg through config.SaveSafely and renders the outcome
// the way the user asked for it: the backup path on every backed-up write,
// and an actionable refusal — naming exactly what would have been lost —
// when the write would destroy data.
//
// A refusal is reported here in full and returned as errSilent, so the
// process exits non-zero without a second, terser copy of the message.
func saveConfig(cmd *cobra.Command, cfg *config.Config) error {
	opts := saveOptions(cmd)
	rep, err := config.SaveSafely(cfg, opts)
	if err != nil {
		var de *config.DestroyError
		if errors.As(err, &de) {
			reportRefusal(cmd.ErrOrStderr(), de)
			return errSilent
		}
		return err
	}
	if rep == nil {
		return nil
	}
	if rep.BackupPath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  ✓ backed up → %s\n", tildePath(rep.BackupPath))
	}
	// A write that dropped data must say so. SaveSafely's own prompt already
	// listed it when it asked, but --force and --yes skip that prompt — and
	// those are exactly the paths where the loss would otherwise be silent,
	// which is the one thing this whole surface exists to prevent.
	if len(rep.Removed) > 0 && opts.AssumeYes {
		reportRemovals(cmd.ErrOrStderr(), rep.Removed)
	}
	return nil
}

// reportRemovals records what a forced write actually dropped, and names the
// backup command that can walk it back.
func reportRemovals(w io.Writer, removed []string) {
	fmt.Fprintf(w, "\n  ⚠  this write removed data from your config:\n")
	for _, r := range removed {
		fmt.Fprintf(w, "       - %s\n", shortenHome(r))
	}
	fmt.Fprintf(w, "\n     Recover it with: mkt config repair --from-backup 1\n\n")
}

// reportRefusal renders a refused write. The shape is deliberate: what
// would be lost first (that is what the user cares about), then why, then
// the reassurance that nothing happened, then the two ways forward.
func reportRefusal(w io.Writer, de *config.DestroyError) {
	fmt.Fprintln(w)
	if len(de.Removed) > 0 {
		fmt.Fprintf(w, "  ⚠  This write would remove data from your config:\n")
		for _, r := range de.Removed {
			fmt.Fprintf(w, "       - %s\n", shortenHome(r))
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "  ⚠  This write was refused.\n\n")
	}

	base := filepath.Base(de.Path)
	switch de.Reason {
	case config.ReasonDegraded:
		fmt.Fprintf(w, "     Cause: %s failed to parse%s\n", base, lineSuffix(de.Line))
		if detail := parseDetail(de.Err); detail != "" {
			fmt.Fprintf(w, "            %s\n", detail)
		}
		fmt.Fprintf(w, "            so mkt loaded defaults instead of your file.\n\n")
		fmt.Fprintf(w, "  Your file has NOT been modified.\n")
		if de.Line > 0 {
			fmt.Fprintf(w, "  Fix line %d, or re-run with --force to overwrite.\n", de.Line)
		} else {
			fmt.Fprintf(w, "  Fix %s, or re-run with --force to overwrite.\n", tildePath(de.Path))
		}
		fmt.Fprintf(w, "  To recover an earlier copy: mkt config repair --list\n")
	case config.ReasonNoPrompt:
		fmt.Fprintf(w, "     Cause: this write drops data and there is no terminal to confirm on.\n\n")
		fmt.Fprintf(w, "  Your file has NOT been modified.\n")
		fmt.Fprintf(w, "  Re-run with --yes to write anyway (a timestamped backup is taken first).\n")
	case config.ReasonDeclined:
		fmt.Fprintf(w, "  Your file has NOT been modified.\n")
	default:
		fmt.Fprintf(w, "  Your file has NOT been modified.\n")
	}
	fmt.Fprintln(w)
}

// lineSuffix renders " (line 9)" for a known position and nothing for an
// error that carried none.
func lineSuffix(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprintf(" (line %d)", line)
}

// parseDetail reduces a YAML/viper parse error to the part worth showing.
// Viper prefixes its own wrapper and yaml.v3 repeats the line number we
// already printed, so both are trimmed.
func parseDetail(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	for _, prefix := range []string{"While parsing config: ", "while parsing config: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	msg = strings.TrimPrefix(msg, "yaml: ")
	if rest, ok := strings.CutPrefix(msg, "line "); ok {
		if i := strings.Index(rest, ": "); i > 0 {
			if _, convErr := strconv.Atoi(rest[:i]); convErr == nil {
				msg = rest[i+2:]
			}
		}
	}
	return strings.TrimSpace(msg)
}

// shortenHome rewrites the home directory wherever it appears inside a
// message. The removal descriptions come from the config package, which
// embeds absolute paths in prose ("everything in /Users/…/config.yaml — the
// file does not parse"), and a bare absolute path in the middle of a
// sentence is both wider than the terminal and unlike every other path mkt
// prints. tildePath handles a path that is the whole string; this handles
// one buried in text.
func shortenHome(s string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return s
	}
	return strings.ReplaceAll(s, home+string(os.PathSeparator), "~"+string(os.PathSeparator))
}

// tildePath shortens a path under the user's home directory to ~/… so the
// paths mkt prints are the ones the user would type back.
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}

// ──────────────────────────────── repair ────────────────────────────────

// repairCommand builds `mkt config repair`, the recovery path every refusal
// message points at. Backups are written by every config write that changes
// the file, so there is almost always something to walk back to.
func repairCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "List and restore timestamped config backups",
		Long: `Recover from a config.yaml that was broken by hand or overwritten.

Every write that changes the file leaves a timestamped copy beside it
(config.yaml.bak.YYYYMMDD-HHMMSS); the newest ` + strconv.Itoa(config.MaxBackups) + ` are kept.

  mkt config repair --list                 show the available backups
  mkt config repair --from-backup 1        restore the Nth backup from --list
  mkt config repair --from-backup <path>   restore a specific file

Restoring is not a one-way door: the config being replaced is itself backed
up first, so restoring the wrong snapshot can be undone the same way.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			from, _ := cmd.Flags().GetString("from-backup")
			backups, err := config.ListBackups()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()

			if from == "" {
				printBackups(out, backups)
				return nil
			}

			path, err := resolveBackupRef(from, backups)
			if err != nil {
				return err
			}
			if err := config.RestoreBackup(path); err != nil {
				return err
			}
			// RestoreBackup copies the file it replaced first; that copy is
			// now the newest backup, so name it — it is the undo.
			if after, lerr := config.ListBackups(); lerr == nil && len(after) > 0 && after[0].Path != path {
				fmt.Fprintf(out, "  ✓ backed up → %s\n", tildePath(after[0].Path))
			}
			fmt.Fprintf(out, "  ✓ restored config from %s\n", tildePath(path))

			// A restore that puts a still-broken file back is worth saying out
			// loud rather than leaving the user to discover it on next run.
			if res, lerr := config.LoadWithResult(); lerr == nil && res.Degraded {
				warnDegradedConfig(cmd.ErrOrStderr(), res)
				return errSilent
			}
			return nil
		},
	}
	cmd.Flags().Bool("list", false, "list available backups, newest first (the default when --from-backup is absent)")
	cmd.Flags().String("from-backup", "", "restore this backup: a path, or the 1-based index shown by --list")
	return cmd
}

// printBackups renders the backup table, newest first, with the age and
// size that let a user tell "the one from before I broke it" apart from
// "the one taken a second ago by the write that broke it".
func printBackups(w io.Writer, backups []config.BackupInfo) {
	if len(backups) == 0 {
		fmt.Fprintf(w, "No backups found beside %s\n", tildePath(configFilePath()))
		fmt.Fprintf(w, "Backups are written automatically by every config write that changes the file.\n")
		return
	}
	fmt.Fprintf(w, "%-4s %-19s %8s %8s  %s\n", "#", "TAKEN", "AGE", "SIZE", "PATH")
	now := time.Now()
	for i, b := range backups {
		fmt.Fprintf(w, "%-4d %-19s %8s %8s  %s\n",
			i+1,
			b.Taken.Format("2006-01-02 15:04:05"),
			shortDuration(now.Sub(b.Taken)),
			fmt.Sprintf("%dB", b.Size),
			tildePath(b.Path),
		)
	}
	fmt.Fprintf(w, "\nRestore with: mkt config repair --from-backup 1\n")
}

// resolveBackupRef turns a --from-backup value into a path. A bare integer
// is the 1-based index from --list; anything else is taken as a path.
func resolveBackupRef(ref string, backups []config.BackupInfo) (string, error) {
	if n, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil {
		if n < 1 || n > len(backups) {
			return "", fmt.Errorf("no backup #%d: there %s %d (run `mkt config repair --list`)",
				n, plural(len(backups), "is", "are"), len(backups))
		}
		return backups[n-1].Path, nil
	}
	path := ref
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("backup %s: %w", ref, err)
	}
	return path, nil
}

// plural picks the singular or plural form for n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// shortDuration renders an age the way a person reads a backup list: the
// largest unit that is still meaningful, one term only.
func shortDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// sortedNames returns the keys of m in a stable order, for deterministic
// output in the commands that report per-portfolio results.
func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

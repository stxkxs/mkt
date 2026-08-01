package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stxkxs/mkt/internal/config"
)

// ─────────────────────────────── test harness ───────────────────────────────

// runCLI drives the real command tree exactly as main does, capturing both
// streams. The tree is a package global built in init(), so every flag is
// reset first — otherwise a --force in one test would still be set in the
// next one.
func runCLI(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetFlags(rootCmd)

	var outBuf, errBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&errBuf)
	if stdin != nil {
		rootCmd.SetIn(stdin)
	} else {
		rootCmd.SetIn(os.Stdin)
	}
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetIn(nil)
		rootCmd.SetArgs(nil)
		resetFlags(rootCmd)
	})

	err = rootCmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// resetFlags restores every flag in the tree to its default and clears
// Changed, so command state cannot leak between test cases.
func resetFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// seedConfig points HOME at a fresh tempdir and writes body to config.yaml,
// returning the config path.
func seedConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := isolateHome(t, home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// watchlistHas reports whether the persisted watchlist still carries sym.
// Grepping the raw YAML would false-positive on the alert rule for the same
// symbol, so this reloads and looks at the list itself.
func watchlistHas(t *testing.T, sym string) bool {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	return slices.Contains(cfg.Watchlist, sym)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// goodConfig is a small, valid config carrying the exact data the user lost:
// a named portfolio and an alert rule.
const goodConfig = `watchlist:
  - AAPL
  - BTC-USD
poll_interval: 15s
sparkline_len: 60
theme: tokyonight
portfolios:
  - name: Retirement
    holdings:
      - symbol: VTI
        quantity: 500
        cost_basis: 180
alerts:
  - symbol: AAPL
    condition: above
    value: 400
    enabled: true
`

// brokenConfig is goodConfig with one line indented wrong — the single
// character that made `config validate` print "Config OK" and the next
// `config add` destroy the file.
const brokenConfig = `watchlist:
  - AAPL
  - BTC-USD
poll_interval: 15s
portfolios:
  - name: Retirement
    holdings:
      - symbol: VTI
       quantity: 500
        cost_basis: 180
`

// ────────────────────────────── the headline bug ──────────────────────────────

// TestValidateReportsParseFailureInsteadOfConfigOK is the regression test for
// the bug that started all of this: a bad indent made validate report the
// defaults as if they were the user's config.
func TestValidateReportsParseFailureInsteadOfConfigOK(t *testing.T) {
	seedConfig(t, brokenConfig)

	out, errOut, err := runCLI(t, nil, "config", "validate")
	if err == nil {
		t.Fatal("validate exited zero on a config that does not parse")
	}
	if strings.Contains(out, "Config OK") {
		t.Errorf("validate reported OK for an unparseable file:\n%s", out)
	}
	if !strings.Contains(errOut, "does not parse") {
		t.Errorf("no parse failure reported:\n%s", errOut)
	}
	if !strings.Contains(errOut, "line ") {
		t.Errorf("no line number reported:\n%s", errOut)
	}
	if !strings.Contains(errOut, "mkt config repair") {
		t.Errorf("no pointer at the recovery path:\n%s", errOut)
	}
}

// TestConfigAddRefusesToDestroyDegradedFile is the other half: the write
// that used to grow the file from 243 to 22500 bytes must now be refused,
// the file left byte-identical, and the exit code non-zero.
func TestConfigAddRefusesToDestroyDegradedFile(t *testing.T) {
	path := seedConfig(t, brokenConfig)
	before := readFile(t, path)

	out, errOut, err := runCLI(t, nil, "config", "add", "TSLA")
	if err == nil {
		t.Fatal("config add exited zero over a degraded config")
	}
	if after := readFile(t, path); after != before {
		t.Errorf("config was modified despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	msg := out + errOut
	for _, want := range []string{
		"would remove data from your config",
		"failed to parse",
		"loaded defaults instead of your file",
		"Your file has NOT been modified",
		"--force",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "added TSLA") {
		t.Errorf("claimed to have added a symbol it did not add:\n%s", msg)
	}
}

// TestConfigAddForceBacksUpThenWrites checks the documented escape hatch:
// --force writes, but only after leaving a timestamped copy behind, and it
// says where that copy is.
func TestConfigAddForceBacksUpThenWrites(t *testing.T) {
	path := seedConfig(t, brokenConfig)
	original := readFile(t, path)

	// A symbol the built-in defaults do not already carry: --force writes
	// over a file mkt could not read, so the config it writes is the defaults
	// plus this addition.
	const sym = "KO"
	out, errOut, err := runCLI(t, nil, "config", "add", sym, "--force")
	if err != nil {
		t.Fatalf("config add --force: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "backed up →") {
		t.Errorf("no backup path reported:\n%s", out)
	}
	if !strings.Contains(out, "added "+sym) {
		t.Errorf("no confirmation of the add:\n%s", out)
	}

	backups, err := config.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backups, want 1", len(backups))
	}
	if got := readFile(t, backups[0].Path); got != original {
		t.Errorf("backup does not hold the original bytes:\n%s", got)
	}
	if !watchlistHas(t, sym) {
		t.Errorf("%s was not written:\n%s", sym, readFile(t, path))
	}
}

// TestConfigAddForceNamesWhatItDestroyed is the other half of --force. The
// escape hatch may not be silent: a forced write over an unreadable file
// drops everything that file held, and the user has to be told what, and
// where the copy of it is.
func TestConfigAddForceNamesWhatItDestroyed(t *testing.T) {
	seedConfig(t, brokenConfig)

	out, errOut, err := runCLI(t, nil, "config", "add", "KO", "--force")
	if err != nil {
		t.Fatalf("config add --force: %v\n%s", err, errOut)
	}
	both := out + errOut
	if !strings.Contains(both, "this write removed data from your config") {
		t.Errorf("a forced write dropped data silently:\n%s", both)
	}
	if !strings.Contains(both, "mkt config repair --from-backup 1") {
		t.Errorf("no recovery path offered after a destructive write:\n%s", both)
	}
}

// TestConfigRemoveAssumeYesNamesWhatItDestroyed covers the scriptable path:
// -y skips SaveSafely's own prompt, so the command has to record the loss
// itself or nothing ever does.
func TestConfigRemoveAssumeYesNamesWhatItDestroyed(t *testing.T) {
	seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, nil, "config", "remove", "AAPL", "-y")
	if err != nil {
		t.Fatalf("config remove -y: %v\n%s", err, errOut)
	}
	if !strings.Contains(out+errOut, "this write removed data from your config") {
		t.Errorf("-y dropped data silently:\n%s", out+errOut)
	}
}

// TestConfigRemoveInteractiveDoesNotRepeatTheList checks the confirmation
// path stays quiet afterwards: SaveSafely already showed the list when it
// asked, and printing it a second time trains the user to skim past it.
func TestConfigRemoveInteractiveDoesNotRepeatTheList(t *testing.T) {
	seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, strings.NewReader("y\n"), "config", "remove", "AAPL")
	if err != nil {
		t.Fatalf("config remove: %v\n%s", err, errOut)
	}
	if strings.Contains(out+errOut, "this write removed data from your config") {
		t.Errorf("the removal list was printed twice:\n%s", out+errOut)
	}
}

// TestConfigRemoveRefusesWithoutConfirmation covers the ordinary (non-
// degraded) destructive write: dropping a watchlist symbol needs a yes.
func TestConfigRemoveRefusesWithoutConfirmation(t *testing.T) {
	path := seedConfig(t, goodConfig)
	before := readFile(t, path)

	out, errOut, err := runCLI(t, strings.NewReader("n\n"), "config", "remove", "AAPL")
	if err == nil {
		t.Fatal("declined write reported success")
	}
	if after := readFile(t, path); after != before {
		t.Error("config changed after the user declined")
	}
	if !strings.Contains(out+errOut, "Your file has NOT been modified") {
		t.Errorf("no reassurance that nothing happened:\n%s", out+errOut)
	}
}

func TestConfigRemoveProceedsOnYes(t *testing.T) {
	path := seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, strings.NewReader("y\n"), "config", "remove", "AAPL")
	if err != nil {
		t.Fatalf("config remove: %v\n%s", err, errOut)
	}
	if watchlistHas(t, "AAPL") {
		t.Errorf("AAPL was not removed from the watchlist:\n%s", readFile(t, path))
	}
	if !strings.Contains(out, "backed up →") {
		t.Errorf("no backup reported for a destructive write:\n%s", out)
	}
}

// TestConfigRemoveAssumeYesSkipsPrompt is the scriptable path: -y writes
// without asking.
func TestConfigRemoveAssumeYesSkipsPrompt(t *testing.T) {
	path := seedConfig(t, goodConfig)

	if _, errOut, err := runCLI(t, nil, "config", "remove", "AAPL", "-y"); err != nil {
		t.Fatalf("config remove -y: %v\n%s", err, errOut)
	}
	if watchlistHas(t, "AAPL") {
		t.Errorf("AAPL was not removed from the watchlist:\n%s", readFile(t, path))
	}
}

// TestConfigAddIsAdditiveAndNeedsNoConfirmation guards the common case: a
// purely additive write must not prompt or refuse.
func TestConfigAddIsAdditiveAndNeedsNoConfirmation(t *testing.T) {
	path := seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, nil, "config", "add", "TSLA")
	if err != nil {
		t.Fatalf("config add: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "added TSLA") {
		t.Errorf("unexpected output:\n%s", out)
	}
	body := readFile(t, path)
	for _, want := range []string{"TSLA", "AAPL", "Retirement"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing from config after an additive write:\n%s", want, body)
		}
	}
}

// TestConfigAddNormalizesSymbol checks the CLI stores what the dashboard
// subscribes, and says so when it rewrote the input.
func TestConfigAddNormalizesSymbol(t *testing.T) {
	path := seedConfig(t, goodConfig)

	out, errOut, err := runCLI(t, nil, "config", "add", "eth")
	if err != nil {
		t.Fatalf("config add eth: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "ETH-USD") {
		t.Errorf("canonical spelling not reported:\n%s", out)
	}
	if !strings.Contains(readFile(t, path), "ETH-USD") {
		t.Error("ETH-USD not stored")
	}
}

func TestConfigSetRejectsBadValues(t *testing.T) {
	seedConfig(t, goodConfig)

	if _, _, err := runCLI(t, nil, "config", "set", "poll_interval", "banana"); err == nil {
		t.Error("accepted a non-duration poll_interval")
	}
	if _, _, err := runCLI(t, nil, "config", "set", "theme", "vaporwave"); err == nil {
		t.Error("accepted an unknown theme")
	}
}

// ──────────────────────────────── repair ────────────────────────────────

func TestConfigRepairListsAndRestores(t *testing.T) {
	path := seedConfig(t, goodConfig)
	original := readFile(t, path)

	// A backed-up write, so there is something to walk back to.
	if _, errOut, err := runCLI(t, nil, "config", "add", "TSLA"); err != nil {
		t.Fatalf("config add: %v\n%s", err, errOut)
	}
	if !strings.Contains(readFile(t, path), "TSLA") {
		t.Fatal("setup: TSLA was not written")
	}

	out, _, err := runCLI(t, nil, "config", "repair", "--list")
	if err != nil {
		t.Fatalf("config repair --list: %v", err)
	}
	if !strings.Contains(out, "TAKEN") || !strings.Contains(out, "config.yaml.bak.") {
		t.Errorf("backup listing looks wrong:\n%s", out)
	}

	out, errOut, err := runCLI(t, nil, "config", "repair", "--from-backup", "1")
	if err != nil {
		t.Fatalf("config repair --from-backup 1: %v\n%s", err, errOut)
	}
	if !strings.Contains(out, "restored config from") {
		t.Errorf("restore not reported:\n%s", out)
	}
	if got := readFile(t, path); got != original {
		t.Errorf("restore did not bring back the original:\ngot:\n%s\nwant:\n%s", got, original)
	}
	// The restore is itself undoable: the replaced file was backed up.
	backups, _ := config.ListBackups()
	if len(backups) < 2 {
		t.Errorf("restore did not back up the file it replaced (%d backups)", len(backups))
	}
}

func TestConfigRepairEmptyListIsNotAnError(t *testing.T) {
	seedConfig(t, goodConfig)

	out, _, err := runCLI(t, nil, "config", "repair", "--list")
	if err != nil {
		t.Fatalf("config repair --list: %v", err)
	}
	if !strings.Contains(out, "No backups found") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestConfigRepairRejectsUnknownIndex(t *testing.T) {
	seedConfig(t, goodConfig)

	if _, _, err := runCLI(t, nil, "config", "repair", "--from-backup", "7"); err == nil {
		t.Error("accepted an index with no backup behind it")
	}
}

// TestConfigRepairRecoversFromABrokenFile is the whole recovery story end to
// end: break the file, watch the write get refused, restore, write again.
func TestConfigRepairRecoversFromABrokenFile(t *testing.T) {
	path := seedConfig(t, goodConfig)

	// One good write, so a backup of the healthy file exists.
	if _, errOut, err := runCLI(t, nil, "config", "add", "TSLA"); err != nil {
		t.Fatalf("config add: %v\n%s", err, errOut)
	}
	// The user then hand-edits and breaks it.
	if err := os.WriteFile(path, []byte(brokenConfig), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := runCLI(t, nil, "config", "add", "NVDA"); err == nil {
		t.Fatal("write over the broken file was not refused")
	}

	// Recover.
	if _, errOut, err := runCLI(t, nil, "config", "repair", "--from-backup", "1"); err != nil {
		t.Fatalf("config repair: %v\n%s", err, errOut)
	}
	if _, errOut, err := runCLI(t, nil, "config", "add", "NVDA"); err != nil {
		t.Fatalf("config add after repair: %v\n%s", err, errOut)
	}
	body := readFile(t, path)
	for _, want := range []string{"NVDA", "Retirement"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing after recovery:\n%s", want, body)
		}
	}
}

// ─────────────────────────────── small pieces ───────────────────────────────

func TestParseDetailStripsRedundantPrefixes(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"While parsing config: yaml: line 9: did not find expected key", "did not find expected key"},
		{"yaml: did not find expected key", "did not find expected key"},
		{"some other failure", "some other failure"},
	}
	for _, tt := range tests {
		if got := parseDetail(errString(tt.in)); got != tt.want {
			t.Errorf("parseDetail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := parseDetail(nil); got != "" {
		t.Errorf("parseDetail(nil) = %q, want empty", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestTildePath(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)

	// tildePath abbreviates with the host's own separator — "~/.config/..."
	// on Unix, "~\.config\..." on Windows — because the result is printed
	// back to a user who will paste it into their own shell. Build the
	// expectation the same way rather than hardcoding a slash.
	want := "~" + string(os.PathSeparator) + filepath.Join(".config", "mkt", "config.yaml")
	if got := tildePath(filepath.Join(home, ".config", "mkt", "config.yaml")); got != want {
		t.Errorf("tildePath = %q, want %q", got, want)
	}

	outside := filepath.Join(string(os.PathSeparator), "etc", "mkt.yaml")
	if got := tildePath(outside); got != outside {
		t.Errorf("tildePath rewrote a path outside home: %q", got)
	}
}

func TestReportRefusalNamesEveryLostItem(t *testing.T) {
	var buf bytes.Buffer
	reportRefusal(&buf, &config.DestroyError{
		Path:     "/home/u/.config/mkt/config.yaml",
		Reason:   config.ReasonDegraded,
		Degraded: true,
		Line:     9,
		Err:      errString("yaml: line 9: did not find expected key"),
		Removed: []string{
			`portfolio "Retirement" (1 holding, 500 VTI)`,
			"1 alert rule (AAPL above 400)",
		},
	})
	got := buf.String()
	for _, want := range []string{
		`portfolio "Retirement" (1 holding, 500 VTI)`,
		"1 alert rule (AAPL above 400)",
		"Cause: config.yaml failed to parse (line 9)",
		"did not find expected key",
		"Your file has NOT been modified.",
		"Fix line 9, or re-run with --force to overwrite.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestReportRefusalNoTerminalSaysPassYes covers the unattended case: the
// remedy must be --yes, and it must never be "wait".
func TestReportRefusalNoTerminalSaysPassYes(t *testing.T) {
	var buf bytes.Buffer
	reportRefusal(&buf, &config.DestroyError{
		Path:    "/home/u/.config/mkt/config.yaml",
		Reason:  config.ReasonNoPrompt,
		Removed: []string{"1 watchlist symbol (AAPL)"},
	})
	got := buf.String()
	if !strings.Contains(got, "no terminal to confirm on") {
		t.Errorf("cause not explained:\n%s", got)
	}
	if !strings.Contains(got, "--yes") {
		t.Errorf("no --yes remedy offered:\n%s", got)
	}
}

func TestShortDuration(t *testing.T) {
	tests := []struct {
		secs int
		want string
	}{
		{5, "5s"}, {90, "1m"}, {3700, "1h"}, {90000, "1d"},
	}
	for _, tt := range tests {
		if got := shortDuration(time.Duration(tt.secs) * time.Second); got != tt.want {
			t.Errorf("shortDuration(%ds) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// ErrWouldDestroy is returned when a write was refused to protect user data.
// Every refusal wraps it, so callers can test with errors.Is and then type-
// assert to *DestroyError for the detail worth printing.
var ErrWouldDestroy = errors.New("config write refused: it would destroy user data")

// SaveOptions controls how far SaveSafely is allowed to go when the write
// would cost the user something. The defaults (zero value) are the paranoid
// ones: refuse a degraded file, and ask before dropping anything.
type SaveOptions struct {
	Force     bool      // allow writing over a degraded (unparseable) config
	AssumeYes bool      // skip the interactive confirmation
	In        io.Reader // prompt input; nil -> os.Stdin
	Out       io.Writer // prompt output; nil -> os.Stderr
}

// SaveReport describes what a SaveSafely call did, or would have done. It is
// returned even on refusal, so a caller can render Removed without having to
// recompute the diff.
type SaveReport struct {
	BackupPath string   // "" if nothing was backed up
	Removed    []string // human-readable descriptions of user data this write drops
	Wrote      bool
}

// DestroyError is the concrete error behind every ErrWouldDestroy. It carries
// enough structure for the CLI to print an actionable message: which file,
// which line broke, what would have been lost, and why we stopped.
type DestroyError struct {
	Path     string   // the config file we refused to write
	Reason   string   // why we stopped: see the Reason* constants
	Removed  []string // what the write would have dropped
	Degraded bool     // true when the on-disk file does not parse
	Line     int      // best-effort 1-based line of the parse error; 0 if unknown
	Err      error    // the parse error, when Degraded
}

// Reasons a write can be refused. The CLI switches on these to pick the
// right remedy to suggest.
const (
	ReasonDegraded = "degraded"    // the file on disk does not parse
	ReasonDeclined = "declined"    // the user answered no at the prompt
	ReasonNoPrompt = "no-terminal" // nobody is there to answer the prompt
)

func (e *DestroyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "refusing to write %s: ", e.Path)
	switch e.Reason {
	case ReasonDegraded:
		if e.Line > 0 {
			fmt.Fprintf(&b, "the file on disk does not parse (line %d): %v", e.Line, e.Err)
		} else {
			fmt.Fprintf(&b, "the file on disk does not parse: %v", e.Err)
		}
		b.WriteString("\n  writing now would replace its contents with defaults")
		b.WriteString("\n  fix the file, or pass --force to overwrite it (a backup is taken either way)")
	case ReasonNoPrompt:
		b.WriteString("this write drops data and there is no terminal to confirm on")
		b.WriteString("\n  pass --yes to write anyway (a backup is taken first)")
	case ReasonDeclined:
		b.WriteString("declined")
	default:
		b.WriteString("it would destroy user data")
	}
	for _, r := range e.Removed {
		fmt.Fprintf(&b, "\n  - %s", r)
	}
	return b.String()
}

// Is reports every DestroyError as an ErrWouldDestroy so callers can match
// the sentinel without caring about the reason.
func (e *DestroyError) Is(target error) bool { return target == ErrWouldDestroy }

// Unwrap exposes the underlying YAML parse error on a degraded refusal.
func (e *DestroyError) Unwrap() error { return e.Err }

// SaveSafely writes the config, refusing rather than silently destroying
// anything the user cannot get back.
//
// The sequence is:
//
//  1. re-read and re-parse the file currently on disk;
//  2. if it does not parse and Force is not set, refuse — writing would
//     replace a file we cannot even read with whatever cfg happens to hold
//     (which, after a degraded Load, is the defaults);
//  3. diff the on-disk config against cfg and describe everything the write
//     would drop; if that list is non-empty and AssumeYes is not set, ask,
//     refusing instead of hanging when there is no terminal to ask on;
//  4. take a timestamped backup when the file exists and differs;
//  5. write atomically — temp file in the same directory, 0600, fsync,
//     rename — so a crash mid-write can never truncate the config.
//
// A report accompanies every refusal so the caller can show Removed without
// recomputing it; it is nil only when the write never got far enough to know
// what was at stake (an unusable config dir, an unencodable config).
func SaveSafely(cfg *Config, o SaveOptions) (*SaveReport, error) {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)

	path := configPath()
	next, nextKeys, err := encodeConfig(cfg)
	if err != nil {
		return nil, err
	}

	rep := &SaveReport{}

	current, readErr := os.ReadFile(path)
	exists := readErr == nil
	if !exists && !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("read config %s: %w", path, readErr)
	}

	var parseErr error
	if exists {
		onDisk, onDiskKeys, perr := parseFile(path)
		if perr != nil {
			// We cannot enumerate what is in there, so the honest answer is
			// "all of it".
			parseErr = perr
			rep.Removed = []string{fmt.Sprintf("everything in %s — the file does not parse, so its contents cannot be read", path)}
		} else {
			rep.Removed = removals(onDisk, cfg, onDiskKeys, nextKeys)
		}
	}

	if parseErr != nil && !o.Force {
		return rep, &DestroyError{
			Path:     path,
			Reason:   ReasonDegraded,
			Removed:  rep.Removed,
			Degraded: true,
			Line:     yamlErrorLine(parseErr),
			Err:      parseErr,
		}
	}

	if len(rep.Removed) > 0 && !o.AssumeYes {
		if err := confirm(o, path, rep.Removed, parseErr != nil); err != nil {
			return rep, err
		}
	}

	if exists && !bytes.Equal(current, next) {
		backupPath, err := Backup(path)
		if err != nil {
			return rep, err
		}
		rep.BackupPath = backupPath
	}

	if err := writeAtomic(path, next, 0o600); err != nil {
		return rep, err
	}
	rep.Wrote = true
	tightenPerms()
	return rep, nil
}

// parseFile decodes the config file exactly as Load does but without
// injecting any defaults, so the result is strictly what the user has on
// disk. The settings map is returned alongside so the diff can also notice
// whole top-level sections a write would drop. Used to work out what a write
// would cost — never to produce a Config the app runs on.
func parseFile(path string) (*Config, map[string]any, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, nil, err
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, nil, err
	}
	cfg.normalize()
	return &cfg, v.AllSettings(), nil
}

// confirm asks the user whether to go ahead with a write that drops data.
// It returns nil to proceed and an ErrWouldDestroy to stop. When there is
// nobody to ask it stops rather than blocking on a read that will never be
// answered — an unattended `mkt config set` in a script must fail loudly,
// not hang.
func confirm(o SaveOptions, path string, removed []string, degraded bool) error {
	out := o.Out
	if out == nil {
		out = os.Stderr
	}
	in := o.In
	if in == nil {
		if !stdinIsTerminal() {
			return &DestroyError{Path: path, Reason: ReasonNoPrompt, Removed: removed, Degraded: degraded}
		}
		in = os.Stdin
	}

	var prompt strings.Builder
	fmt.Fprintf(&prompt, "mkt: this write drops data that is in %s:\n", path)
	for _, r := range removed {
		fmt.Fprintf(&prompt, "  - %s\n", r)
	}
	prompt.WriteString("A timestamped backup is written first.\n")
	prompt.WriteString("Continue? [y/N]: ")
	_, _ = io.WriteString(out, prompt.String())

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		// EOF with nothing typed (redirected from /dev/null, closed pipe):
		// treat silence as "no".
		return &DestroyError{Path: path, Reason: ReasonDeclined, Removed: removed, Degraded: degraded}
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return &DestroyError{Path: path, Reason: ReasonDeclined, Removed: removed, Degraded: degraded}
}

// stdinIsTerminal reports whether os.Stdin looks interactive enough to put a
// prompt on. golang.org/x/term would be the precise answer, but it is not a
// direct dependency of this module and a character-device check is enough
// here: the failure mode we must avoid is blocking forever on a pipe or a
// file, and neither of those is a character device. /dev/null passes the
// check but answers EOF immediately, which confirm reads as "no".
//
// Indirected through a var so tests can drive both branches.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// writeAtomic replaces path with data in one step: a temp file in the same
// directory (so the rename cannot cross a filesystem boundary), chmod, write,
// fsync, rename, then fsync the directory so the rename itself is durable. A
// crash at any point leaves either the old file or the new one, never a
// truncated mix of the two.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".mkt-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config in %s: %w", dir, err)
	}
	tmp := f.Name()
	// Removing the temp after a successful rename is a no-op; on any error
	// path it is the cleanup.
	defer func() { _ = os.Remove(tmp) }()

	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	// Best effort: the rename is already visible, this only makes it survive
	// a power cut. Not all platforms allow opening a directory for sync.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

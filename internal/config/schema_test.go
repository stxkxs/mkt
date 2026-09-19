package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// A config file as written before `schema_version` existed.
const unversionedConfig = `watchlist:
  - VTI
  - AAPL
watchlists:
  - name: Crypto Majors
    symbols: [BTC-USD, ETH-USD]
theme: nord
poll_interval: 30s
sparkline_len: 40
portfolios:
  - name: Retirement
    tax_method: fifo
    holdings:
      - symbol: VTI
        quantity: 500
        cost_basis: 210.00
alerts:
  - symbol: AAPL
    condition: above
    value: 400
    enabled: true
notes:
  VTI: total market, the boring core
webhook_url: https://example.invalid/hook
`

// The version a reader infers for an absent field must be the one this build
// writes, or a file loaded before the field existed would take a different
// path through the code than the same file saved once.
func TestUnversionedFileReadsAsCurrentSchema(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, unversionedConfig)
	unversioned, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if unversioned.SchemaVersion != 0 {
		t.Fatalf("SchemaVersion = %d for a file with no schema_version, want 0", unversioned.SchemaVersion)
	}

	versioned := writeAndLoad(t, "schema_version: 1\n"+unversionedConfig)
	if versioned.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", versioned.SchemaVersion, SchemaVersion)
	}

	// Everything the app runs on must be identical; only the recorded
	// version distinguishes the two files.
	versioned.SchemaVersion = 0
	if !reflect.DeepEqual(unversioned, versioned) {
		t.Errorf("declaring schema_version changed the loaded config:\n without = %+v\n with    = %+v", unversioned, versioned)
	}
}

// Loading must never rewrite a file that already exists. A pre-version file
// stays byte-for-byte as the user left it until they ask for a write.
func TestLoadLeavesUnversionedFileOnDisk(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, unversionedConfig)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != unversionedConfig {
		t.Errorf("Load rewrote the config file:\n%s", after)
	}
}

// Gaining the version field is an addition, so the write that adds it drops
// nothing and needs no confirmation — SaveOptions{} here is the paranoid
// zero value, which refuses rather than prompting when there is no terminal.
func TestSaveAddsSchemaVersionWithoutDroppingAnything(t *testing.T) {
	path := isolate(t)
	writeConfigFile(t, path, unversionedConfig)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	rep, err := SaveSafely(cfg, SaveOptions{})
	if err != nil {
		t.Fatalf("SaveSafely refused a purely additive write: %v", err)
	}
	if len(rep.Removed) > 0 {
		t.Errorf("SaveSafely reported %v as dropped", rep.Removed)
	}
	if !rep.Wrote {
		t.Fatal("SaveSafely did not write")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "schema_version: 1") {
		t.Errorf("saved config does not declare schema_version:\n%s", raw)
	}

	// Saving again must be a no-op diff: the version is now on disk, so the
	// second write has nothing new to report either.
	rep, err = SaveSafely(cfg, SaveOptions{})
	if err != nil {
		t.Fatalf("second SaveSafely: %v", err)
	}
	if len(rep.Removed) > 0 {
		t.Errorf("second SaveSafely reported %v as dropped", rep.Removed)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SchemaVersion != SchemaVersion {
		t.Errorf("reloaded SchemaVersion = %d, want %d", reloaded.SchemaVersion, SchemaVersion)
	}
}

// A fresh install writes the version it seeds.
func TestSeedDeclaresSchemaVersion(t *testing.T) {
	path := isolate(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != SchemaVersion {
		t.Errorf("seeded SchemaVersion = %d, want %d", cfg.SchemaVersion, SchemaVersion)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "schema_version: 1") {
		t.Errorf("seeded config does not declare schema_version:\n%s", first(string(raw)))
	}
}

// WatchlistGroups is the only place the two spellings combine, so its
// precedence is the app's precedence.
func TestWatchlistGroups(t *testing.T) {
	t.Parallel()
	named := []Watchlist{
		{Name: "Crypto", Symbols: []string{"BTC-USD"}},
		{Name: "Chips", Symbols: []string{"NVDA"}},
	}
	tests := []struct {
		name string
		cfg  Config
		want []Watchlist
	}{
		{
			name: "flat list leads the named groups",
			cfg:  Config{Watchlist: []string{"VTI", "AAPL"}, Watchlists: named},
			want: append([]Watchlist{{Name: DefaultWatchlistName, Symbols: []string{"VTI", "AAPL"}}}, named...),
		},
		{
			name: "flat list alone becomes the one group",
			cfg:  Config{Watchlist: []string{"VTI"}},
			want: []Watchlist{{Name: DefaultWatchlistName, Symbols: []string{"VTI"}}},
		},
		{
			name: "named groups alone keep file order",
			cfg:  Config{Watchlists: named},
			want: named,
		},
		{
			name: "neither yields one empty group to render into",
			cfg:  Config{},
			want: []Watchlist{{Name: DefaultWatchlistName}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cfg.WatchlistGroups(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("WatchlistGroups() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The resolved groups must not alias the config, or a caller that trims a
// group edits the config every other caller reads.
func TestWatchlistGroupsDoesNotAliasConfig(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Watchlist:  []string{"VTI"},
		Watchlists: []Watchlist{{Name: "Crypto", Symbols: []string{"BTC-USD"}}},
	}
	groups := cfg.WatchlistGroups()
	groups[0].Name = "mutated"
	groups[1].Symbols = []string{"ETH-USD"}

	if cfg.Watchlists[0].Name != "Crypto" {
		t.Errorf("renaming a resolved group renamed the config group: %q", cfg.Watchlists[0].Name)
	}
	if got := cfg.Watchlists[0].Symbols; !reflect.DeepEqual(got, []string{"BTC-USD"}) {
		t.Errorf("replacing a resolved group's symbols changed the config: %v", got)
	}
}

// writeAndLoad puts raw at an isolated config path and returns the load.
func writeAndLoad(t *testing.T, raw string) *Config {
	t.Helper()
	path := isolate(t)
	writeConfigFile(t, path, raw)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// first trims a config dump to its opening lines so a failure message stays
// readable against a seeded file.
func first(s string) string {
	lines := strings.SplitN(s, "\n", 6)
	if len(lines) > 5 {
		lines = append(lines[:5], "...")
	}
	return strings.Join(lines, "\n")
}

// Package settings manages mew's on-disk configuration file (~/.mew/config.json).
//
// The MCP server itself is still configured entirely through environment
// variables (see internal/config). The settings file is a convenience layer
// for humans: it stores the same values keyed by stable dotted names, so a
// user runs `mew config set ...` once and `mew serve` reads them back —
// without the secrets ever appearing in an MCP client's own config.
//
// Precedence is intentional: a value present in the real environment always
// wins over the file, so ad-hoc overrides and the legacy env-only setup keep
// working. ApplyEnv only fills in variables that are unset.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// dirName is the per-user state directory under $HOME.
const dirName = ".mew"

// fileName is the settings file within DefaultDir.
const fileName = "config.json"

// File is the parsed contents of the settings file plus the path it came
// from. The zero value is not usable; obtain one via Load.
type File struct {
	// Values maps a registry key (e.g. "redash.base_url") to its value.
	Values map[string]string `json:"settings"`
	// Disabled lists toolsets the user has explicitly turned off even when
	// their configuration is present.
	Disabled []string `json:"disabled_toolsets,omitempty"`

	// path is where Save writes; not serialized.
	path string `json:"-"`
}

// DefaultDir is ~/.mew.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// DefaultPath is ~/.mew/config.json.
func DefaultPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the settings file at path. A missing file is not an error: it
// returns an empty, ready-to-populate File bound to that path.
func Load(path string) (*File, error) {
	f := &File{Values: map[string]string{}, path: path}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w (fix the JSON or delete the file to start over)", path, err)
	}
	if f.Values == nil {
		f.Values = map[string]string{}
	}
	f.path = path
	return f, nil
}

// Path returns the file's location on disk.
func (f *File) Path() string { return f.path }

// Exists reports whether the settings file is present on disk.
func (f *File) Exists() bool {
	_, err := os.Stat(f.path)
	return err == nil
}

// Save writes the file atomically with owner-only permissions, creating the
// parent directory if needed. The file can hold API tokens, so it is 0600.
func (f *File) Save() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	enc, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding settings: %w", err)
	}
	enc = append(enc, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(enc); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, f.path)
}

// Set records a value for a known registry key. Unknown keys are rejected so
// typos surface immediately instead of silently doing nothing at serve time.
func (f *File) Set(key, value string) error {
	if _, ok := Lookup(key); !ok {
		return fmt.Errorf("unknown setting %q; run `mew config show` to list valid keys", key)
	}
	if f.Values == nil {
		f.Values = map[string]string{}
	}
	f.Values[key] = value
	return nil
}

// Get returns the stored value for key, if present.
func (f *File) Get(key string) (string, bool) {
	v, ok := f.Values[key]
	return v, ok
}

// Unset removes a key. Removing an absent key is a no-op.
func (f *File) Unset(key string) { delete(f.Values, key) }

// ApplyEnv exports every stored value as its environment variable, but only
// when that variable is not already set — so the real environment wins.
func (f *File) ApplyEnv() error {
	for key, val := range f.Values {
		s, ok := Lookup(key)
		if !ok {
			continue // tolerate stale keys from a newer/older binary
		}
		if os.Getenv(s.Env) == "" {
			if err := os.Setenv(s.Env, val); err != nil {
				return fmt.Errorf("applying %s: %w", s.Env, err)
			}
		}
	}
	return nil
}

// IsDisabled reports whether the named toolset has been turned off explicitly.
func (f *File) IsDisabled(toolset string) bool {
	return slices.Contains(f.Disabled, toolset)
}

// SetDisabled toggles a toolset's disabled flag, keeping the list sorted and
// free of duplicates.
func (f *File) SetDisabled(toolset string, disabled bool) {
	set := map[string]bool{}
	for _, t := range f.Disabled {
		set[t] = true
	}
	if disabled {
		set[toolset] = true
	} else {
		delete(set, toolset)
	}
	f.Disabled = make([]string, 0, len(set))
	for t := range set {
		f.Disabled = append(f.Disabled, t)
	}
	sort.Strings(f.Disabled)
}

// Configured reports whether every required key of a toolset has a value, and
// returns the keys that are still missing.
func (f *File) Configured(toolset string) (ok bool, missing []string) {
	ts, found := LookupToolset(toolset)
	if !found {
		return false, nil
	}
	for _, key := range ts.RequiredKeys {
		if strings.TrimSpace(f.Values[key]) == "" {
			missing = append(missing, key)
		}
	}
	return len(missing) == 0, missing
}

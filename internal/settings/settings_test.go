package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func tempFile(t *testing.T) *File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	f := tempFile(t)
	if f.Exists() {
		t.Fatal("expected file not to exist yet")
	}
	if len(f.Values) != 0 {
		t.Fatalf("expected empty values, got %v", f.Values)
	}
}

func TestSetRejectsUnknownKey(t *testing.T) {
	f := tempFile(t)
	if err := f.Set("redash.nope", "x"); err == nil {
		t.Fatal("expected error for unknown key")
	}
	if err := f.Set("redash.base_url", "https://r"); err != nil {
		t.Fatalf("Set known key: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	f := tempFile(t)
	if err := f.Set("redash.base_url", "https://r"); err != nil {
		t.Fatal(err)
	}
	f.SetDisabled("clickup", true)
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Permissions must be owner-only because the file holds secrets.
	info, err := os.Stat(f.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected mode 0600, got %o", perm)
	}

	reloaded, err := Load(f.Path())
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if v, _ := reloaded.Get("redash.base_url"); v != "https://r" {
		t.Fatalf("value not persisted, got %q", v)
	}
	if !reloaded.IsDisabled("clickup") {
		t.Fatal("disabled flag not persisted")
	}
}

func TestUnset(t *testing.T) {
	f := tempFile(t)
	_ = f.Set("redash.base_url", "https://r")
	f.Unset("redash.base_url")
	if _, ok := f.Get("redash.base_url"); ok {
		t.Fatal("expected key to be removed")
	}
}

func TestApplyEnvDoesNotOverrideRealEnv(t *testing.T) {
	f := tempFile(t)
	_ = f.Set("redash.base_url", "from-file")

	t.Setenv("REDASH_BASE_URL", "from-env")
	if err := f.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv("REDASH_BASE_URL"); got != "from-env" {
		t.Fatalf("real env should win, got %q", got)
	}
}

func TestApplyEnvFillsUnsetVars(t *testing.T) {
	f := tempFile(t)
	_ = f.Set("clickup.api_token", "pk_file")

	t.Setenv("CLICKUP_API_TOKEN", "") // ensure unset for the test
	if err := f.ApplyEnv(); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv("CLICKUP_API_TOKEN"); got != "pk_file" {
		t.Fatalf("expected file value applied, got %q", got)
	}
}

func TestSetDisabledIsIdempotentAndSorted(t *testing.T) {
	f := tempFile(t)
	f.SetDisabled("clickup", true)
	f.SetDisabled("clickup", true) // duplicate
	f.SetDisabled("redash", true)
	if len(f.Disabled) != 2 {
		t.Fatalf("expected 2 disabled, got %v", f.Disabled)
	}
	if f.Disabled[0] != "clickup" || f.Disabled[1] != "redash" {
		t.Fatalf("expected sorted order, got %v", f.Disabled)
	}
	f.SetDisabled("clickup", false)
	if f.IsDisabled("clickup") {
		t.Fatal("clickup should be re-enabled")
	}
}

func TestConfigured(t *testing.T) {
	f := tempFile(t)
	if ok, missing := f.Configured("redash"); ok || len(missing) != 2 {
		t.Fatalf("expected both keys missing, got ok=%v missing=%v", ok, missing)
	}
	_ = f.Set("redash.base_url", "https://r")
	_ = f.Set("redash.api_key", "k")
	if ok, missing := f.Configured("redash"); !ok || len(missing) != 0 {
		t.Fatalf("expected configured, got ok=%v missing=%v", ok, missing)
	}
	if ok, _ := f.Configured("unknown"); ok {
		t.Fatal("unknown toolset should not be configured")
	}
}

func TestRegistryMatchesToolsetKeys(t *testing.T) {
	// Every key a toolset requires must exist in the registry, or Configured
	// and the wizard would reference a phantom setting.
	for _, ts := range Toolsets {
		for _, key := range ts.RequiredKeys {
			if _, ok := Lookup(key); !ok {
				t.Errorf("toolset %q requires unknown key %q", ts.Name, key)
			}
		}
	}
}

package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// clearEnv blanks every variable this package reads so tests are hermetic
// regardless of the developer's shell environment.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		EnvLogLevel,
		EnvRedashBaseURL, EnvRedashAPIKey,
		EnvRedashHTTPTimeout, EnvRedashQueryTimeout,
		EnvRedashMaxRows, EnvRedashMaxResultChars,
		EnvRedashAdhocAutoLimit, EnvRedashDisableAdhoc, EnvRedashDefaultDataSource,
		EnvClickUpAPIToken, EnvClickUpTeamID,
		EnvClickUpHTTPTimeout, EnvClickUpMaxResultChars, EnvClickUpEnableWrites,
	} {
		t.Setenv(name, "")
	}
}

func TestNoToolsetsConfigured(t *testing.T) {
	clearEnv(t)
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Redash != nil {
		t.Fatal("Redash config should be nil when no variables are set")
	}
	if cfg.ClickUp != nil {
		t.Fatal("ClickUp config should be nil when no variables are set")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestRedashDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvRedashBaseURL, "https://redash.example.com/api/")
	t.Setenv(EnvRedashAPIKey, "secret")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	r := cfg.Redash
	if r == nil {
		t.Fatal("Redash config missing")
	}
	if r.BaseURL != "https://redash.example.com" {
		t.Errorf("BaseURL = %q, want normalized root", r.BaseURL)
	}
	if r.HTTPTimeout != DefaultRedashHTTPTimeout || r.QueryTimeout != DefaultRedashQueryTimeout {
		t.Errorf("timeouts = %v/%v, want defaults", r.HTTPTimeout, r.QueryTimeout)
	}
	if r.MaxRows != DefaultRedashMaxRows || r.MaxResultChars != DefaultRedashMaxResultChars {
		t.Errorf("limits = %d/%d, want defaults", r.MaxRows, r.MaxResultChars)
	}
	if r.AdhocAutoLimit != DefaultRedashAdhocAutoLimit || r.DisableAdhoc {
		t.Errorf("adhoc settings = %d/%v, want defaults", r.AdhocAutoLimit, r.DisableAdhoc)
	}
}

func TestRedashOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvRedashBaseURL, "https://redash.example.com")
	t.Setenv(EnvRedashAPIKey, "secret")
	t.Setenv(EnvRedashHTTPTimeout, "10")
	t.Setenv(EnvRedashQueryTimeout, "300")
	t.Setenv(EnvRedashMaxRows, "50")
	t.Setenv(EnvRedashMaxResultChars, "20000")
	t.Setenv(EnvRedashAdhocAutoLimit, "0")
	t.Setenv(EnvRedashDisableAdhoc, "true")
	t.Setenv(EnvLogLevel, "debug")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	r := cfg.Redash
	if r.HTTPTimeout != 10*time.Second || r.QueryTimeout != 300*time.Second {
		t.Errorf("timeouts = %v/%v", r.HTTPTimeout, r.QueryTimeout)
	}
	if r.MaxRows != 50 || r.MaxResultChars != 20000 {
		t.Errorf("limits = %d/%d", r.MaxRows, r.MaxResultChars)
	}
	if r.AdhocAutoLimit != 0 {
		t.Errorf("AdhocAutoLimit = %d, want 0 (disabled)", r.AdhocAutoLimit)
	}
	if !r.DisableAdhoc {
		t.Error("DisableAdhoc should be true")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want debug", cfg.LogLevel)
	}
}

func TestClickUpDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClickUpAPIToken, "pk_secret")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	c := cfg.ClickUp
	if c == nil {
		t.Fatal("ClickUp config missing")
	}
	if cfg.Redash != nil {
		t.Fatal("Redash config should stay nil")
	}
	if c.HTTPTimeout != DefaultClickUpHTTPTimeout || c.MaxResultChars != DefaultClickUpMaxResultChars {
		t.Errorf("defaults = %v/%d", c.HTTPTimeout, c.MaxResultChars)
	}
	if c.EnableWrites || c.TeamID != "" {
		t.Errorf("EnableWrites/TeamID = %v/%q, want off/empty", c.EnableWrites, c.TeamID)
	}
}

func TestClickUpOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClickUpAPIToken, "pk_secret")
	t.Setenv(EnvClickUpTeamID, "9013037641")
	t.Setenv(EnvClickUpHTTPTimeout, "10")
	t.Setenv(EnvClickUpMaxResultChars, "20000")
	t.Setenv(EnvClickUpEnableWrites, "true")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	c := cfg.ClickUp
	if c.TeamID != "9013037641" || c.HTTPTimeout != 10*time.Second || c.MaxResultChars != 20000 {
		t.Errorf("overrides not applied: %+v", c)
	}
	if !c.EnableWrites {
		t.Error("EnableWrites should be true")
	}
}

func TestClickUpBadValuesReported(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvClickUpAPIToken, "pk_secret")
	t.Setenv(EnvClickUpHTTPTimeout, "abc")
	t.Setenv(EnvClickUpEnableWrites, "banana")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{EnvClickUpHTTPTimeout, EnvClickUpEnableWrites} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s, got: %v", want, err)
		}
	}
}

func TestRedashPartialConfigFails(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvRedashAPIKey, "secret")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected an error when only the API key is set")
	}
	if !strings.Contains(err.Error(), EnvRedashBaseURL) {
		t.Errorf("error should name %s, got: %v", EnvRedashBaseURL, err)
	}
}

func TestRedashBadValuesAllReported(t *testing.T) {
	clearEnv(t)
	t.Setenv(EnvRedashBaseURL, "https://redash.example.com")
	t.Setenv(EnvRedashAPIKey, "secret")
	t.Setenv(EnvRedashMaxRows, "abc")
	t.Setenv(EnvRedashHTTPTimeout, "0")
	t.Setenv(EnvRedashDisableAdhoc, "banana")
	t.Setenv(EnvLogLevel, "silly")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{EnvRedashMaxRows, EnvRedashHTTPTimeout, EnvRedashDisableAdhoc, EnvLogLevel} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s, got: %v", want, err)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "https://redash.example.com", want: "https://redash.example.com"},
		{in: "https://redash.example.com/", want: "https://redash.example.com"},
		{in: "https://redash.example.com/api", want: "https://redash.example.com"},
		{in: "https://redash.example.com/api/", want: "https://redash.example.com"},
		{in: "https://example.com/redash/api", want: "https://example.com/redash"},
		{in: "http://localhost:5000", want: "http://localhost:5000"},
		{in: "ftp://redash.example.com", wantErr: true},
		{in: "not a url", wantErr: true},
	}
	for _, tc := range cases {
		got, err := normalizeBaseURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeBaseURL(%q): expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeBaseURL(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

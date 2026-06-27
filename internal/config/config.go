// Package config loads and validates mew's configuration from environment
// variables. Each toolset (Redash today, more later) owns a section that is
// only parsed when its environment variables are present.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variables recognized by the server.
const (
	EnvLogLevel = "MEW_LOG_LEVEL"

	EnvRedashBaseURL        = "REDASH_BASE_URL"
	EnvRedashAPIKey         = "REDASH_API_KEY"
	EnvRedashHTTPTimeout    = "REDASH_HTTP_TIMEOUT_SECONDS"
	EnvRedashQueryTimeout   = "REDASH_QUERY_TIMEOUT_SECONDS"
	EnvRedashMaxRows        = "REDASH_MAX_ROWS"
	EnvRedashMaxResultChars = "REDASH_MAX_RESULT_CHARS"
	EnvRedashAdhocAutoLimit = "REDASH_ADHOC_AUTO_LIMIT"
	EnvRedashDisableAdhoc   = "REDASH_DISABLE_ADHOC"

	EnvClickUpAPIToken       = "CLICKUP_API_TOKEN"
	EnvClickUpTeamID         = "CLICKUP_TEAM_ID"
	EnvClickUpHTTPTimeout    = "CLICKUP_HTTP_TIMEOUT_SECONDS"
	EnvClickUpMaxResultChars = "CLICKUP_MAX_RESULT_CHARS"
	EnvClickUpEnableWrites   = "CLICKUP_ENABLE_WRITES"
)

// Defaults applied when the corresponding variable is unset.
const (
	DefaultRedashHTTPTimeout    = 30 * time.Second
	DefaultRedashQueryTimeout   = 120 * time.Second
	DefaultRedashMaxRows        = 1000
	DefaultRedashMaxResultChars = 50_000
	DefaultRedashAdhocAutoLimit = 1000

	// DefaultRowsPerCall is the per-call row count used when a tool call
	// does not ask for an explicit max_rows.
	DefaultRowsPerCall = 100

	DefaultClickUpHTTPTimeout    = 30 * time.Second
	DefaultClickUpMaxResultChars = 50_000
)

// Config is the validated server configuration.
type Config struct {
	// LogLevel applies to stderr logging for the whole server.
	LogLevel slog.Level
	// Redash is nil when the Redash toolset is not configured.
	Redash *Redash
	// ClickUp is nil when the ClickUp toolset is not configured.
	ClickUp *ClickUp
}

// ClickUp configures the ClickUp toolset.
type ClickUp struct {
	// APIToken is a ClickUp personal API token (ClickUp → Settings → Apps).
	APIToken string
	// TeamID is the default workspace id used when a tool call passes none.
	// Empty means auto-detect when exactly one workspace is visible.
	TeamID string
	// HTTPTimeout bounds each individual HTTP request to ClickUp.
	HTTPTimeout time.Duration
	// MaxResultChars caps the serialized size of one result payload.
	MaxResultChars int
	// EnableWrites registers the mutating tools (create/update task, add
	// comment). Off by default: reads only.
	EnableWrites bool
}

// Redash configures the Redash toolset.
type Redash struct {
	// BaseURL is the Redash root URL, normalized to carry no trailing
	// slash and no /api suffix.
	BaseURL string
	// APIKey is a Redash *user* API key (from the Redash profile page).
	APIKey string
	// HTTPTimeout bounds each individual HTTP request to Redash.
	HTTPTimeout time.Duration
	// QueryTimeout bounds one query execution end to end, job polling
	// included.
	QueryTimeout time.Duration
	// MaxRows is the hard cap on rows returned to the model in one call.
	MaxRows int
	// MaxResultChars caps the serialized size of one result payload.
	MaxResultChars int
	// AdhocAutoLimit is the row limit injected into ad-hoc Mongo queries
	// that specify none. Zero disables injection.
	AdhocAutoLimit int
	// DisableAdhoc hides the ad-hoc query tool, restricting execution to
	// saved queries.
	DisableAdhoc bool
}

// FromEnv reads the configuration, collecting every problem into one error
// instead of stopping at the first.
func FromEnv() (Config, error) {
	var errs []error

	cfg := Config{LogLevel: slog.LevelInfo}
	if lvl, ok, err := optionalLogLevel(EnvLogLevel); err != nil {
		errs = append(errs, err)
	} else if ok {
		cfg.LogLevel = lvl
	}

	redashCfg, err := redashFromEnv()
	if err != nil {
		errs = append(errs, err)
	}
	cfg.Redash = redashCfg

	clickupCfg, err := clickupFromEnv()
	if err != nil {
		errs = append(errs, err)
	}
	cfg.ClickUp = clickupCfg

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// clickupFromEnv returns (nil, nil) when CLICKUP_API_TOKEN is unset, meaning
// the toolset is simply not enabled.
func clickupFromEnv() (*ClickUp, error) {
	token := strings.TrimSpace(os.Getenv(EnvClickUpAPIToken))
	if token == "" {
		return nil, nil
	}

	var errs []error
	c := &ClickUp{
		APIToken:       token,
		TeamID:         strings.TrimSpace(os.Getenv(EnvClickUpTeamID)),
		HTTPTimeout:    DefaultClickUpHTTPTimeout,
		MaxResultChars: DefaultClickUpMaxResultChars,
	}

	if n, ok, err := optionalInt(EnvClickUpHTTPTimeout, 1); err != nil {
		errs = append(errs, err)
	} else if ok {
		c.HTTPTimeout = time.Duration(n) * time.Second
	}
	if n, ok, err := optionalInt(EnvClickUpMaxResultChars, 1000); err != nil {
		errs = append(errs, err)
	} else if ok {
		c.MaxResultChars = n
	}
	if b, ok, err := optionalBool(EnvClickUpEnableWrites); err != nil {
		errs = append(errs, err)
	} else if ok {
		c.EnableWrites = b
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// redashFromEnv returns (nil, nil) when no Redash variable is set at all,
// meaning the toolset is simply not enabled.
func redashFromEnv() (*Redash, error) {
	rawURL := strings.TrimSpace(os.Getenv(EnvRedashBaseURL))
	rawKey := strings.TrimSpace(os.Getenv(EnvRedashAPIKey))
	if rawURL == "" && rawKey == "" {
		return nil, nil
	}

	var errs []error
	r := &Redash{
		APIKey:         rawKey,
		HTTPTimeout:    DefaultRedashHTTPTimeout,
		QueryTimeout:   DefaultRedashQueryTimeout,
		MaxRows:        DefaultRedashMaxRows,
		MaxResultChars: DefaultRedashMaxResultChars,
		AdhocAutoLimit: DefaultRedashAdhocAutoLimit,
	}

	if rawURL == "" {
		errs = append(errs, fmt.Errorf("%s is required when %s is set", EnvRedashBaseURL, EnvRedashAPIKey))
	} else if u, err := normalizeBaseURL(rawURL); err != nil {
		errs = append(errs, err)
	} else {
		r.BaseURL = u
	}

	if rawKey == "" {
		errs = append(errs, fmt.Errorf("%s is required when %s is set: copy your user API key from the Redash profile page", EnvRedashAPIKey, EnvRedashBaseURL))
	}

	if n, ok, err := optionalInt(EnvRedashHTTPTimeout, 1); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.HTTPTimeout = time.Duration(n) * time.Second
	}
	if n, ok, err := optionalInt(EnvRedashQueryTimeout, 1); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.QueryTimeout = time.Duration(n) * time.Second
	}
	if n, ok, err := optionalInt(EnvRedashMaxRows, 1); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.MaxRows = n
	}
	if n, ok, err := optionalInt(EnvRedashMaxResultChars, 1000); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.MaxResultChars = n
	}
	if n, ok, err := optionalInt(EnvRedashAdhocAutoLimit, 0); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.AdhocAutoLimit = n
	}
	if b, ok, err := optionalBool(EnvRedashDisableAdhoc); err != nil {
		errs = append(errs, err)
	} else if ok {
		r.DisableAdhoc = b
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return r, nil
}

func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%s must be an http(s) URL like https://redash.example.com, got %q", EnvRedashBaseURL, raw)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/api")
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

// optionalInt parses name as an integer when set, enforcing a minimum.
func optionalInt(name string, min int) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	if n < min {
		return 0, false, fmt.Errorf("%s must be >= %d, got %d", name, min, n)
	}
	return n, true, nil
}

func optionalBool(name string) (bool, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false, fmt.Errorf("%s must be a boolean (true/false), got %q", name, raw)
	}
	return b, true, nil
}

func optionalLogLevel(name string) (slog.Level, bool, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "":
		return 0, false, nil
	case "debug":
		return slog.LevelDebug, true, nil
	case "info":
		return slog.LevelInfo, true, nil
	case "warn", "warning":
		return slog.LevelWarn, true, nil
	case "error":
		return slog.LevelError, true, nil
	}
	return 0, false, fmt.Errorf("%s must be one of debug, info, warn, error; got %q", name, raw)
}

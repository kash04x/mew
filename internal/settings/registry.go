package settings

import "mew/internal/config"

// Setting describes one recognized configuration value: its stable dotted
// key (used by `mew config set`), the environment variable it maps to (read
// by internal/config at serve time), and presentation metadata.
type Setting struct {
	// Key is the dotted name users type, e.g. "redash.base_url".
	Key string
	// Env is the environment variable the server actually reads.
	Env string
	// Toolset groups the setting ("redash", "clickup") or is empty for
	// global settings.
	Toolset string
	// Secret marks values that must be masked in any human-facing output.
	Secret bool
	// Essential marks the values the `config init` wizard prompts for; the
	// rest are optional tuning knobs.
	Essential bool
	// Desc is a one-line explanation shown by `config show`.
	Desc string
}

// Registry is the authoritative list of settings, mirroring the environment
// variables in internal/config. Keep the two in sync when adding a knob.
var Registry = []Setting{
	{Key: "log_level", Env: config.EnvLogLevel, Desc: "stderr log level: debug, info, warn, error"},

	// Redash.
	{Key: "redash.base_url", Env: config.EnvRedashBaseURL, Toolset: "redash", Essential: true, Desc: "Redash root URL (https://redash.example.com)"},
	{Key: "redash.api_key", Env: config.EnvRedashAPIKey, Toolset: "redash", Secret: true, Essential: true, Desc: "Redash user API key (from your Redash profile page)"},
	{Key: "redash.http_timeout_seconds", Env: config.EnvRedashHTTPTimeout, Toolset: "redash", Desc: "per-HTTP-request timeout"},
	{Key: "redash.query_timeout_seconds", Env: config.EnvRedashQueryTimeout, Toolset: "redash", Desc: "end-to-end budget for one query execution"},
	{Key: "redash.max_rows", Env: config.EnvRedashMaxRows, Toolset: "redash", Desc: "hard cap on rows returned per call"},
	{Key: "redash.max_result_chars", Env: config.EnvRedashMaxResultChars, Toolset: "redash", Desc: "character budget per result payload"},
	{Key: "redash.adhoc_auto_limit", Env: config.EnvRedashAdhocAutoLimit, Toolset: "redash", Desc: "row limit injected into ad-hoc queries (0 disables)"},
	{Key: "redash.disable_adhoc", Env: config.EnvRedashDisableAdhoc, Toolset: "redash", Desc: "true removes the ad-hoc query tool entirely"},

	// ClickUp.
	{Key: "clickup.api_token", Env: config.EnvClickUpAPIToken, Toolset: "clickup", Secret: true, Essential: true, Desc: "ClickUp personal API token (starts with pk_)"},
	{Key: "clickup.team_id", Env: config.EnvClickUpTeamID, Toolset: "clickup", Desc: "default workspace id (auto when only one is visible)"},
	{Key: "clickup.http_timeout_seconds", Env: config.EnvClickUpHTTPTimeout, Toolset: "clickup", Desc: "per-HTTP-request timeout"},
	{Key: "clickup.max_result_chars", Env: config.EnvClickUpMaxResultChars, Toolset: "clickup", Desc: "character budget per task-list payload"},
	{Key: "clickup.enable_writes", Env: config.EnvClickUpEnableWrites, Toolset: "clickup", Desc: "true registers task/comment write tools"},
}

// Lookup finds a setting by its dotted key.
func Lookup(key string) (Setting, bool) {
	for _, s := range Registry {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// SettingsFor returns the settings belonging to a toolset ("" for global).
func SettingsFor(toolset string) []Setting {
	var out []Setting
	for _, s := range Registry {
		if s.Toolset == toolset {
			out = append(out, s)
		}
	}
	return out
}

// Toolset describes an installable capability and the keys it needs to run.
type Toolset struct {
	// Name is the stable identifier ("redash", "clickup").
	Name string
	// Title is a human-friendly label.
	Title string
	// RequiredKeys must all be set for the toolset to activate.
	RequiredKeys []string
}

// Toolsets is the catalog of available toolsets.
var Toolsets = []Toolset{
	{Name: "redash", Title: "Redash (production MongoDB)", RequiredKeys: []string{"redash.base_url", "redash.api_key"}},
	{Name: "clickup", Title: "ClickUp workspace", RequiredKeys: []string{"clickup.api_token"}},
}

// LookupToolset finds a toolset by name.
func LookupToolset(name string) (Toolset, bool) {
	for _, t := range Toolsets {
		if t.Name == name {
			return t, true
		}
	}
	return Toolset{}, false
}

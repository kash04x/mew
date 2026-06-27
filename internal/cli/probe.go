package cli

import (
	"context"
	"fmt"
	"time"

	"mew/internal/clickup"
	"mew/internal/config"
	"mew/internal/redash"
	"mew/internal/settings"
)

// effectiveConfig resolves the configuration exactly as `serve` would: the
// settings file fills in any environment variable not already set, then
// internal/config validates the result. It also returns the loaded file so
// callers can consult disabled-toolset state.
func effectiveConfig() (config.Config, *settings.File, error) {
	file, err := loadSettings()
	if err != nil {
		return config.Config{}, nil, err
	}
	if err := file.ApplyEnv(); err != nil {
		return config.Config{}, nil, err
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return config.Config{}, file, err
	}
	return cfg, file, nil
}

// probeResult is the outcome of one toolset connection check.
type probeResult struct {
	toolset string
	ok      bool
	summary string // identity/details on success
	err     error  // populated on failure
}

// probeRedash verifies the Redash base URL and key by fetching the session.
func probeRedash(ctx context.Context, rc config.Redash) probeResult {
	res := probeResult{toolset: "redash"}
	client, err := redash.NewClient(redash.Options{
		BaseURL: rc.BaseURL,
		APIKey:  rc.APIKey,
		Timeout: rc.HTTPTimeout,
	})
	if err != nil {
		res.err = err
		return res
	}
	session, err := client.Session(ctx)
	if err != nil {
		res.err = userHint(err)
		return res
	}
	sources, err := client.DataSources(ctx)
	if err != nil {
		res.err = userHint(err)
		return res
	}
	res.ok = true
	res.summary = fmt.Sprintf("%s <%s> · %d data source(s) visible",
		session.User.Name, session.User.Email, len(sources))
	return res
}

// probeClickUp verifies the ClickUp token by fetching the user and teams.
func probeClickUp(ctx context.Context, cc config.ClickUp) probeResult {
	res := probeResult{toolset: "clickup"}
	client, err := clickup.NewClient(clickup.Options{
		APIToken: cc.APIToken,
		Timeout:  cc.HTTPTimeout,
	})
	if err != nil {
		res.err = err
		return res
	}
	user, err := client.User(ctx)
	if err != nil {
		res.err = userHint(err)
		return res
	}
	teams, err := client.Teams(ctx)
	if err != nil {
		res.err = userHint(err)
		return res
	}
	res.ok = true
	res.summary = fmt.Sprintf("%s <%s> · %d workspace(s) visible",
		user.Username, user.Email, len(teams))
	return res
}

// userHint appends remediation advice from the typed API errors, mirroring
// how the toolsets surface failures to the model.
func userHint(err error) error {
	type hinter interface{ Hint() string }
	if h, ok := err.(hinter); ok {
		if hint := h.Hint(); hint != "" {
			return fmt.Errorf("%w (%s)", err, hint)
		}
	}
	return err
}

// probeContext bounds a connection check so the CLI never hangs.
func probeContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

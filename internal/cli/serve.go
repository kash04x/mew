package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"mew/internal/clickup"
	"mew/internal/config"
	"mew/internal/redash"
	clickuptools "mew/internal/toolsets/clickup"
	redashtools "mew/internal/toolsets/redash"
	"mew/internal/version"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the MCP stdio server (invoked by MCP clients)",
	Long: `Starts the Model Context Protocol server on stdio. This is the command an
MCP client such as Claude Code or Claude Desktop runs; you rarely call it by
hand. Configuration is read from the settings file, then overridden by any
matching environment variables.

stdout carries the MCP protocol — all logs go to stderr.`,
	Args: cobra.NoArgs,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	// File values fill in only the environment variables a client did not
	// already pass, so explicit env always wins.
	if err := file.ApplyEnv(); err != nil {
		return err
	}

	cfg, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("configuration error:\n%w", err)
	}

	// stdout carries the MCP protocol; logs must only ever go to stderr.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// A toolset disabled via `mew toolset disable` is dropped even when its
	// configuration is present.
	if file.IsDisabled("redash") {
		cfg.Redash = nil
	}
	if file.IsDisabled("clickup") {
		cfg.ClickUp = nil
	}

	var sections []string
	var registrations []func(*mcp.Server)

	if cfg.Redash != nil {
		client, err := redash.NewClient(redash.Options{
			BaseURL: cfg.Redash.BaseURL,
			APIKey:  cfg.Redash.APIKey,
			Timeout: cfg.Redash.HTTPTimeout,
			Logger:  logger,
		})
		if err != nil {
			return err
		}
		deps := redashtools.Deps{
			Client: client,
			Config: *cfg.Redash,
			Logger: logger,
			Poll:   redash.DefaultPollConfig(),
		}
		registrations = append(registrations, func(s *mcp.Server) { redashtools.Register(s, deps) })
		sections = append(sections, redashtools.Instructions)
		logger.Info("redash toolset enabled",
			"base_url", cfg.Redash.BaseURL,
			"adhoc_enabled", !cfg.Redash.DisableAdhoc,
			"query_timeout", cfg.Redash.QueryTimeout)
	}

	if cfg.ClickUp != nil {
		client, err := clickup.NewClient(clickup.Options{
			APIToken: cfg.ClickUp.APIToken,
			Timeout:  cfg.ClickUp.HTTPTimeout,
			Logger:   logger,
		})
		if err != nil {
			return err
		}
		deps := clickuptools.Deps{
			Client: client,
			Config: *cfg.ClickUp,
			Logger: logger,
		}
		registrations = append(registrations, func(s *mcp.Server) { clickuptools.Register(s, deps) })
		sections = append(sections, clickuptools.Instructions(cfg.ClickUp.EnableWrites))
		logger.Info("clickup toolset enabled",
			"default_team_id", cfg.ClickUp.TeamID,
			"writes_enabled", cfg.ClickUp.EnableWrites)
	}

	if len(registrations) == 0 {
		return fmt.Errorf("no toolsets configured: run `mew config init`, or set %s and %s for Redash, or %s for ClickUp",
			config.EnvRedashBaseURL, config.EnvRedashAPIKey, config.EnvClickUpAPIToken)
	}

	instructions := "Mew is a personal MCP server exposing internal company systems.\n\n" +
		strings.Join(sections, "\n\n")

	server := mcp.NewServer(
		&mcp.Implementation{Name: "mew", Title: "Mew — personal MCP server", Version: version.Version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	for _, register := range registrations {
		register(server)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("mew starting", "version", version.Version)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		return err
	}
	logger.Info("mew stopped")
	return nil
}

// Package cli implements mew's command-line interface: the `serve` command
// that an MCP client invokes, plus the management commands a human uses to
// configure, diagnose, install, and update the tool.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"mew/internal/settings"
)

// configPath is the resolved settings-file path, set by the persistent
// --config flag (defaulting to ~/.mew/config.json). Every command reads it.
var configPath string

var rootCmd = &cobra.Command{
	Use:   "mew",
	Short: "Mew — a personal MCP server and its management CLI",
	Long: `Mew is a single personal MCP server that bundles toolsets for internal
systems (Redash, ClickUp). Clients spawn it with "mew serve"; everything else
here configures, diagnoses, installs, and updates that server.

Quick start:
  mew config init      Set up your toolset credentials interactively
  mew install          Register mew with Claude Code
  mew doctor           Verify everything is wired up correctly`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the entry point called from main with the build version.
func Execute(version string) {
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "mew:", err)
		os.Exit(1)
	}
}

func init() {
	def, err := settings.DefaultPath()
	if err != nil {
		// Without a home directory we cannot place a default file; leave the
		// flag empty so commands surface a clear error when they need it.
		def = ""
	}
	rootCmd.PersistentFlags().StringVar(&configPath, "config", def,
		"path to the settings file")
}

// loadSettings reads the settings file at the resolved --config path.
func loadSettings() (*settings.File, error) {
	if configPath == "" {
		return nil, fmt.Errorf("could not determine a config path; pass --config explicitly")
	}
	return settings.Load(configPath)
}

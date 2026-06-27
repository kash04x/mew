package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"mew/internal/settings"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show a configuration and registration summary",
	Long:  "Prints the version, binary and settings locations, toolset state, and whether mew is registered with Claude Code. Performs no network calls — use `mew doctor` for connectivity.",
	Args:  cobra.NoArgs,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "Version:       %s\n", rootCmd.Version)
	if self, err := os.Executable(); err == nil {
		fmt.Fprintf(out, "Binary:        %s\n", self)
	}
	settingsState := file.Path()
	if !file.Exists() {
		settingsState += " (not created — run `mew config init`)"
	}
	fmt.Fprintf(out, "Settings file: %s\n", settingsState)
	fmt.Fprintf(out, "Claude Code:   %s\n", claudeRegistration())

	fmt.Fprintln(out, "\nToolsets:")
	for _, ts := range settings.Toolsets {
		configured, missing := file.Configured(ts.Name)
		state := "ready"
		switch {
		case !configured:
			state = "not configured (missing: " + strings.Join(missing, ", ") + ")"
		case file.IsDisabled(ts.Name):
			state = "disabled"
		}
		fmt.Fprintf(out, "  %-10s %s\n", ts.Name, state)
	}
	return nil
}

// claudeRegistration reports whether mew appears in `claude mcp list`, best
// effort: a missing Claude CLI is reported plainly rather than as an error.
func claudeRegistration() string {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return "claude CLI not found on PATH"
	}
	outBytes, err := exec.Command(bin, "mcp", "list").CombinedOutput() //nolint:gosec // fixed args
	if err != nil {
		return "could not query (run `claude mcp list`)"
	}
	if strings.Contains(string(outBytes), "mew") {
		return "registered"
	}
	return "not registered (run `mew install`)"
}

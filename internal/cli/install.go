package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	installScope   string
	installDesktop bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Register mew as an MCP server with Claude Code",
	Long: `Registers this binary with Claude Code so it launches "mew serve" automatically.
Credentials come from the settings file, so no secrets are written into
Claude's own configuration.

Use --desktop to print a Claude Desktop config block instead of touching the
Claude Code CLI.`,
	Args: cobra.NoArgs,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installScope, "scope", "user", "Claude Code scope: user, project, or local")
	installCmd.Flags().BoolVar(&installDesktop, "desktop", false, "print a Claude Desktop config block instead of registering with Claude Code")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, _ []string) error {
	self, err := resolvedBinaryPath()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if installDesktop {
		printDesktopConfig(out, self)
		return nil
	}

	bin, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(out, "Claude Code CLI not found on PATH. Register manually with:")
		fmt.Fprintf(out, "\n  claude mcp add mew --scope %s -- %s serve\n\n", installScope, self)
		fmt.Fprintln(out, "Or, for Claude Desktop, run `mew install --desktop`.")
		return nil
	}

	args := []string{"mcp", "add", "mew", "--scope", installScope, "--", self, "serve"}
	c := exec.Command(bin, args...) //nolint:gosec // args are fixed plus the resolved self path
	c.Stdout, c.Stderr = out, cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("`claude mcp add` failed: %w (if mew is already registered, run `mew uninstall` first or `claude mcp remove mew`)", err)
	}

	fmt.Fprintf(out, "\nRegistered mew with Claude Code (scope: %s).\n", installScope)
	fmt.Fprintln(out, "Run `mew doctor` to confirm your credentials, then restart Claude.")
	return nil
}

func printDesktopConfig(out io.Writer, self string) {
	fmt.Fprintln(out, "Add this to your Claude Desktop config file:")
	fmt.Fprintln(out)
	fmt.Fprintf(out, `  "mcpServers": {
    "mew": {
      "command": %q,
      "args": ["serve"]
    }
  }
`, self)
	fmt.Fprintln(out, "\nLocation: ~/Library/Application Support/Claude/claude_desktop_config.json (macOS).")
	fmt.Fprintln(out, "Credentials are read from the settings file — run `mew config init` if you haven't.")
}

// resolvedBinaryPath returns the absolute, symlink-resolved path to the
// running mew binary so registrations point at a stable location.
func resolvedBinaryPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return self, nil
}

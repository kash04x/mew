package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"mew/internal/settings"
)

var uninstallYes bool

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unregister from Claude and remove mew completely",
	Long: `Removes mew in three steps:
  1. Unregisters the MCP server from Claude Code (best effort).
  2. Deletes the settings directory (~/.mew), including saved credentials.
  3. Deletes the mew binary itself.

If the binary lives in a root-owned directory (e.g. /usr/local/bin), re-run
with sudo to complete step 3.`,
	Args: cobra.NoArgs,
	RunE: runUninstall,
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "skip the confirmation prompt")
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	self, err := resolvedBinaryPath()
	if err != nil {
		return err
	}
	dir, err := settings.DefaultDir()
	if err != nil {
		return err
	}

	if !uninstallYes {
		fmt.Fprintf(out, "This will unregister mew from Claude, delete %s, and remove %s.\n", dir, self)
		fmt.Fprint(out, "Proceed? [y/N] ")
		if !confirm(cmd) {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	// 1. Unregister from Claude Code (best effort).
	if bin, err := exec.LookPath("claude"); err == nil {
		c := exec.Command(bin, "mcp", "remove", "mew") //nolint:gosec // fixed args
		if err := c.Run(); err != nil {
			fmt.Fprintln(out, "• Claude Code: not registered or already removed")
		} else {
			fmt.Fprintln(out, "• Claude Code: unregistered mew")
		}
	} else {
		fmt.Fprintln(out, "• Claude Code: CLI not found, skipping (remove the Desktop entry manually if present)")
	}

	// 2. Remove the settings directory.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	fmt.Fprintf(out, "• Removed %s\n", dir)

	// 3. Remove the binary itself.
	if err := os.Remove(self); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("could not remove %s: permission denied — re-run with: sudo mew uninstall -y", self)
		}
		return fmt.Errorf("removing %s: %w", self, err)
	}
	fmt.Fprintf(out, "• Removed %s\n", self)

	fmt.Fprintln(out, "\nmew uninstalled.")
	return nil
}

func confirm(cmd *cobra.Command) bool {
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

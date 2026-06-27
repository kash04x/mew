package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mew/internal/settings"
)

var toolsetCmd = &cobra.Command{
	Use:   "toolset",
	Short: "List and toggle toolsets",
}

var toolsetListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show toolsets and whether each is configured and enabled",
	Args:  cobra.NoArgs,
	RunE:  runToolsetList,
}

var toolsetEnableCmd = &cobra.Command{
	Use:       "enable <name>",
	Short:     "Enable a toolset that was previously disabled",
	Args:      cobra.ExactArgs(1),
	ValidArgs: toolsetNames(),
	RunE:      func(cmd *cobra.Command, args []string) error { return setToolsetDisabled(cmd, args[0], false) },
}

var toolsetDisableCmd = &cobra.Command{
	Use:       "disable <name>",
	Short:     "Disable a toolset without removing its configuration",
	Args:      cobra.ExactArgs(1),
	ValidArgs: toolsetNames(),
	RunE:      func(cmd *cobra.Command, args []string) error { return setToolsetDisabled(cmd, args[0], true) },
}

func init() {
	toolsetCmd.AddCommand(toolsetListCmd, toolsetEnableCmd, toolsetDisableCmd)
	rootCmd.AddCommand(toolsetCmd)
}

func runToolsetList(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-10s %-12s %-10s %s\n", "TOOLSET", "CONFIGURED", "ENABLED", "NOTES")
	for _, ts := range settings.Toolsets {
		configured, missing := file.Configured(ts.Name)
		enabled := !file.IsDisabled(ts.Name)
		notes := ts.Title
		if !configured {
			notes = "missing: " + strings.Join(missing, ", ")
		}
		fmt.Fprintf(out, "%-10s %-12s %-10s %s\n", ts.Name, yesNo(configured), yesNo(enabled), notes)
	}
	return nil
}

func setToolsetDisabled(cmd *cobra.Command, name string, disabled bool) error {
	if _, ok := settings.LookupToolset(name); !ok {
		return fmt.Errorf("unknown toolset %q (known: %s)", name, strings.Join(toolsetNames(), ", "))
	}
	file, err := loadSettings()
	if err != nil {
		return err
	}
	file.SetDisabled(name, disabled)
	if err := file.Save(); err != nil {
		return err
	}
	state := "enabled"
	if disabled {
		state = "disabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Toolset %s %s\n", name, state)
	return nil
}

func toolsetNames() []string {
	names := make([]string, len(settings.Toolsets))
	for i, t := range settings.Toolsets {
		names[i] = t.Name
	}
	return names
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

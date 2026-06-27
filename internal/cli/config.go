package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"mew/internal/settings"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage the settings file",
	Long:  "Read and write mew's settings file (~/.mew/config.json by default).",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up toolset credentials interactively",
	Args:  cobra.NoArgs,
	RunE:  runConfigInit,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a single value (e.g. mew config set redash.api_key KEY)",
	Args:  cobra.ExactArgs(2),
	RunE:  runConfigSet,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print a single value",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigGet,
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Remove a value",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigUnset,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show all settings (secrets masked)",
	Args:  cobra.NoArgs,
	RunE:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the settings file location",
	Args:  cobra.NoArgs,
	RunE:  runConfigPath,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the settings file in $EDITOR",
	Args:  cobra.NoArgs,
	RunE:  runConfigEdit,
}

func init() {
	configCmd.AddCommand(configInitCmd, configSetCmd, configGetCmd, configUnsetCmd, configShowCmd, configPathCmd, configEditCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	if err := file.Set(args[0], args[1]); err != nil {
		return err
	}
	if err := file.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Set %s\n", args[0])
	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	if _, ok := settings.Lookup(args[0]); !ok {
		return fmt.Errorf("unknown setting %q", args[0])
	}
	v, ok := file.Get(args[0])
	if !ok {
		return fmt.Errorf("%s is not set", args[0])
	}
	fmt.Fprintln(cmd.OutOrStdout(), v)
	return nil
}

func runConfigUnset(cmd *cobra.Command, args []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	if _, ok := settings.Lookup(args[0]); !ok {
		return fmt.Errorf("unknown setting %q", args[0])
	}
	file.Unset(args[0])
	if err := file.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Unset %s\n", args[0])
	return nil
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Settings file: %s", file.Path())
	if !file.Exists() {
		fmt.Fprint(out, " (not created yet)")
	}
	fmt.Fprint(out, "\n\n")

	for _, group := range []struct{ title, toolset string }{
		{"Global", ""},
		{"Redash", "redash"},
		{"ClickUp", "clickup"},
	} {
		fmt.Fprintf(out, "%s\n", group.title)
		for _, s := range settings.SettingsFor(group.toolset) {
			val, ok := file.Get(s.Key)
			display := "(unset)"
			if ok {
				display = maskValue(s, val)
			}
			fmt.Fprintf(out, "  %-30s %s\n", s.Key, display)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func runConfigPath(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), file.Path())
	return nil
}

func runConfigEdit(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	// Ensure the file exists so the editor opens something valid.
	if !file.Exists() {
		if err := file.Save(); err != nil {
			return err
		}
	}
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	ed := exec.Command(editor, file.Path()) //nolint:gosec // editor is the user's own choice
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := ed.Run(); err != nil {
		return fmt.Errorf("editor %q failed: %w", editor, err)
	}
	// Re-parse to catch a botched hand-edit early.
	if _, err := settings.Load(file.Path()); err != nil {
		return err
	}
	return nil
}

func runConfigInit(cmd *cobra.Command, _ []string) error {
	file, err := loadSettings()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	in := bufio.NewReader(cmd.InOrStdin())

	fmt.Fprintln(out, "Configuring mew. Press Enter to keep the current value or skip.")
	fmt.Fprintln(out)

	for _, ts := range settings.Toolsets {
		fmt.Fprintf(out, "── %s ──\n", ts.Title)
		for _, s := range settings.SettingsFor(ts.Name) {
			if !s.Essential {
				continue
			}
			current, _ := file.Get(s.Key)
			prompt := s.Desc
			if current != "" {
				prompt += fmt.Sprintf(" [current: %s]", maskValue(s, current))
			}
			fmt.Fprintf(out, "%s\n  %s = ", prompt, s.Key)

			line, err := in.ReadString('\n')
			if err != nil && line == "" {
				break // EOF: stop prompting gracefully
			}
			value := strings.TrimSpace(line)
			if value == "" {
				continue
			}
			if err := file.Set(s.Key, value); err != nil {
				return err
			}
		}
		fmt.Fprintln(out)
	}

	if err := file.Save(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved %s\n", file.Path())
	fmt.Fprintln(out, "Run `mew doctor` to verify, then `mew install` to register with Claude.")
	return nil
}

// maskValue hides secret values, keeping a short suffix for recognizability.
func maskValue(s settings.Setting, v string) string {
	if !s.Secret || v == "" {
		return v
	}
	if len(v) <= 4 {
		return "••••"
	}
	return "••••" + v[len(v)-4:]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

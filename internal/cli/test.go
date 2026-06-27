package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:       "test <toolset>",
	Short:     "Test connectivity for a single toolset",
	Long:      "Connects to one toolset (redash or clickup) and reports the authenticated identity.",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"redash", "clickup"},
	RunE:      runTest,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	name := args[0]
	cfg, file, err := effectiveConfig()
	if err != nil {
		return err
	}
	ctx, cancel := probeContext()
	defer cancel()

	var res probeResult
	switch name {
	case "redash":
		if cfg.Redash == nil {
			return fmt.Errorf("redash is not configured — run `mew config init`")
		}
		res = probeRedash(ctx, *cfg.Redash)
	case "clickup":
		if cfg.ClickUp == nil {
			return fmt.Errorf("clickup is not configured — run `mew config init`")
		}
		res = probeClickUp(ctx, *cfg.ClickUp)
	default:
		return fmt.Errorf("unknown toolset %q (known: redash, clickup)", name)
	}

	if file.IsDisabled(name) {
		fmt.Fprintf(cmd.OutOrStdout(), "note: %s is disabled and will not load in `mew serve`\n", name)
	}
	if !res.ok {
		return res.err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ %s: %s\n", name, res.summary)
	return nil
}

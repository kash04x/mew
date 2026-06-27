package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check configuration and toolset connectivity",
	Long: `Validates the settings file, then connects to every configured and enabled
toolset to confirm credentials and reachability. Exits non-zero if any check
fails, so it is safe to use in scripts.`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cfg, file, err := effectiveConfig()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Settings file: %s\n\n", file.Path())

	ctx, cancel := probeContext()
	defer cancel()

	var checks []probeResult

	// Redash.
	switch {
	case file.IsDisabled("redash"):
		checks = append(checks, probeResult{toolset: "redash", ok: true, summary: "disabled — skipped"})
	case cfg.Redash == nil:
		checks = append(checks, probeResult{toolset: "redash", summary: "not configured", ok: false, err: errSkip})
	default:
		checks = append(checks, probeRedash(ctx, *cfg.Redash))
	}

	// ClickUp.
	switch {
	case file.IsDisabled("clickup"):
		checks = append(checks, probeResult{toolset: "clickup", ok: true, summary: "disabled — skipped"})
	case cfg.ClickUp == nil:
		checks = append(checks, probeResult{toolset: "clickup", summary: "not configured", ok: false, err: errSkip})
	default:
		checks = append(checks, probeClickUp(ctx, *cfg.ClickUp))
	}

	failures := 0
	configuredAny := false
	for _, c := range checks {
		switch {
		case c.err == errSkip:
			fmt.Fprintf(out, "○ %-8s %s\n", c.toolset, c.summary)
		case c.ok:
			configuredAny = true
			fmt.Fprintf(out, "✓ %-8s %s\n", c.toolset, c.summary)
		default:
			configuredAny = true
			failures++
			fmt.Fprintf(out, "✗ %-8s %v\n", c.toolset, c.err)
		}
	}

	fmt.Fprintln(out)
	if !configuredAny {
		return fmt.Errorf("no toolsets configured — run `mew config init`")
	}
	if failures > 0 {
		return fmt.Errorf("%d toolset check(s) failed", failures)
	}
	fmt.Fprintln(out, "All configured toolsets are reachable.")
	return nil
}

// errSkip marks a toolset that was not checked because it is not configured.
var errSkip = fmt.Errorf("not configured")

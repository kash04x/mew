package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the mew version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "mew %s\n", rootCmd.Version)
	},
}

func init() {
	rootCmd.SetVersionTemplate("mew {{.Version}}\n")
	rootCmd.AddCommand(versionCmd)
}

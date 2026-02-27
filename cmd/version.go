package cmd

import (
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

// SetVersionInfo stores build-time version metadata for the version command.
func SetVersionInfo(version, commit, date string) {
	buildVersion = version
	buildCommit = commit
	buildDate = date
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version as JSON",
	Long:  `Prints build version, git commit SHA, and build date as a JSON object. Useful for verifying installation and reporting bugs.`,
	Run: func(cmd *cobra.Command, args []string) {
		data := map[string]string{
			"version": buildVersion,
			"commit":  buildCommit,
			"date":    buildDate,
		}
		output.PrintData(cmd.OutOrStdout(), data)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

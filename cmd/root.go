package cmd

import (
	"os"

	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// flagErrPrinted tracks whether SetFlagErrorFunc already emitted JSON to
// stderr, preventing Execute from printing a duplicate error.
var flagErrPrinted bool

var rootCmd = &cobra.Command{
	Use:   "shovels",
	Short: "Agent-first CLI for the Shovels REST API",
	Long: `shovels is an agent-first CLI for the Shovels building permit and contractor API.

Every command outputs valid JSON to stdout. Errors go to stderr as JSON.
Pipe output to jq, parse it programmatically, or feed it to another AI agent.

Authentication: set SHOVELS_API_KEY env var, pass --api-key flag, or run: shovels config set api-key <key>`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.String("api-key", "", "Shovels API key (overrides SHOVELS_API_KEY env var and config file)")
	flags.String("limit", "50", `Maximum number of records to return. Use a number (1-100000) or "all" for up to --max-records`)
	flags.Int("max-records", 10000, "Upper bound when --limit=all (default 10000, max 100000)")
	flags.String("base-url", "https://api.shovels.ai/v2", "Shovels API base URL")
	flags.Bool("no-retry", false, "Disable automatic retry on rate-limit (429) responses")
	flags.String("timeout", "30s", "Per-request timeout as a Go duration (e.g. 10s, 1m)")

	// Emit JSON to stderr on flag-parsing errors instead of cobra's plain text.
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		flagErrPrinted = true
		output.PrintError(os.Stderr, err.Error(), 1)
		return err
	})
}

// Execute runs the root command and returns the exit code.
func Execute() int {
	flagErrPrinted = false
	if err := rootCmd.Execute(); err != nil {
		if !flagErrPrinted {
			output.PrintError(os.Stderr, err.Error(), 1)
		}
		return 1
	}
	return 0
}

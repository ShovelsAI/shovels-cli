package cmd

import (
	"github.com/spf13/cobra"
)

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

// Execute runs the root command and returns the exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}

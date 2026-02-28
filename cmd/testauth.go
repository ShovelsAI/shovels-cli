//go:build e2e

package cmd

import (
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// testAuthCmd is a hidden command used solely by e2e tests to verify
// auth gating and credential precedence. It outputs the resolved API key
// so tests can assert which source (flag, env, file) won the chain.
var testAuthCmd = &cobra.Command{
	Use:    "_test-auth",
	Short:  "Test fixture: verify auth gating and credential precedence",
	Hidden: true,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	Run: func(cmd *cobra.Command, args []string) {
		cfg := ResolvedConfig()
		output.PrintData(cmd.OutOrStdout(), map[string]string{
			"api_key": cfg.APIKey,
		})
	},
}

func init() {
	rootCmd.AddCommand(testAuthCmd)
}

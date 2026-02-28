package cmd

import (
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// testAuthCmd is a hidden command used solely by e2e tests to verify
// that the auth gating middleware (PersistentPreRunE + AnnotationRequiresAuth)
// correctly enforces API key presence before command execution.
var testAuthCmd = &cobra.Command{
	Use:    "_test-auth",
	Short:  "Test fixture: verify auth gating",
	Hidden: true,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	Run: func(cmd *cobra.Command, args []string) {
		output.PrintData(cmd.OutOrStdout(), "ok")
	},
}

func init() {
	rootCmd.AddCommand(testAuthCmd)
}

package cmd

import (
	"encoding/json"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show API credit usage and limits for the authenticated account",
	Long: `Retrieve credit usage for the authenticated account. Returns total credits
consumed, credit limit, and other account details. Useful for monitoring
quota before running large queries.

Example:
  shovels usage

Response: {"data": {"credits_used": N, "credit_limit": N, ...}, "meta": {"credits_used": N, ...}}
For unlimited plans, credit_limit is null. Not paginated.`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runUsage,
}

func runUsage(cmd *cobra.Command, args []string) error {
	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	resp, err := cl.Get(cmd.Context(), "/usage", nil)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	var data any
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		output.PrintErrorTyped(os.Stderr, "failed to parse API response", 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintSingle(cmd.OutOrStdout(), data, resp.Credits)
	return nil
}

func init() {
	rootCmd.AddCommand(usageCmd)
}

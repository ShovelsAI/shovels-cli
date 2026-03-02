//go:build e2e

package cmd

import (
	"context"
	"os"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// testPaginateCmd is a hidden command used solely by e2e tests to exercise
// the paginator through the full CLI binary. It makes paginated GET requests
// to <base-url>/<path> and outputs the assembled envelope.
var testPaginateCmd = &cobra.Command{
	Use:    "_test-paginate <path>",
	Short:  "Test fixture: exercise paginator for e2e testing",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := ResolvedConfig()

		limitStr, _ := cmd.Flags().GetString("limit")
		lc, err := client.ParseLimit(limitStr)
		if err != nil {
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}

		maxRecords, _ := cmd.Flags().GetInt("max-records")
		if err := client.ValidateMaxRecords(maxRecords); err != nil {
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}
		if lc.All {
			lc = lc.WithMaxRecords(maxRecords)
		}

		timeoutStr, _ := cmd.Flags().GetString("timeout")
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			output.PrintErrorTyped(os.Stderr, "invalid timeout: "+timeoutStr, 1, "validation_error")
			return &exitError{code: 1}
		}

		noRetry, _ := cmd.Flags().GetBool("no-retry")

		c := client.New(client.Options{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Timeout: timeout,
			NoRetry: noRetry,
			Version: buildVersion,
		})

		result, err := c.Paginate(context.Background(), args[0], nil, lc)
		if err != nil {
			apiErr, ok := err.(*client.APIError)
			if ok {
				output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
				return &exitError{code: apiErr.ExitCode}
			}
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, "unknown_error")
			return &exitError{code: 1}
		}

		output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, result.TotalCount)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(testPaginateCmd)
}

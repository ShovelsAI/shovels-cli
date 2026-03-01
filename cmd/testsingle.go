//go:build e2e

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// testSingleCmd is a hidden command used solely by e2e tests to exercise
// the non-paginated (single object) response path through the full CLI binary.
// It makes a GET request to <base-url>/<path> and outputs the result using
// PrintSingle, which produces an envelope without count or has_more in meta.
var testSingleCmd = &cobra.Command{
	Use:    "_test-single <path>",
	Short:  "Test fixture: exercise non-paginated response for e2e testing",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := ResolvedConfig()

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

		resp, err := c.Get(context.Background(), args[0], nil)
		if err != nil {
			apiErr, ok := err.(*client.APIError)
			if ok {
				output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
				return &exitError{code: apiErr.ExitCode}
			}
			output.PrintErrorTyped(os.Stderr, err.Error(), 1, "unknown_error")
			return &exitError{code: 1}
		}

		var bodyData any
		if err := json.Unmarshal(resp.Body, &bodyData); err != nil {
			bodyData = string(resp.Body)
		}

		output.PrintSingle(cmd.OutOrStdout(), bodyData, resp.Credits)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(testSingleCmd)
}

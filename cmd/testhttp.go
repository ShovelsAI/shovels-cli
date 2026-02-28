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

// testHTTPCmd is a hidden command used solely by e2e tests to exercise
// the HTTP client through the full CLI binary. It makes a GET request
// to <base-url>/<path> and outputs the response or error.
var testHTTPCmd = &cobra.Command{
	Use:    "_test-http <path>",
	Short:  "Test fixture: exercise HTTP client for e2e testing",
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

		// Parse response body as JSON for envelope wrapping.
		var bodyData any
		if err := json.Unmarshal(resp.Body, &bodyData); err != nil {
			bodyData = string(resp.Body)
		}

		meta := map[string]any{}
		if resp.Credits.CreditsUsed != nil {
			meta["credits_used"] = *resp.Credits.CreditsUsed
		}
		// credits_remaining is always present: int value when known,
		// nil (JSON null) for unlimited plans with no credit headers.
		if resp.Credits.CreditsRemaining != nil {
			meta["credits_remaining"] = *resp.Credits.CreditsRemaining
		} else {
			meta["credits_remaining"] = nil
		}

		env := output.Envelope{
			Data: bodyData,
			Meta: meta,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		_ = enc.Encode(env)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(testHTTPCmd)
}

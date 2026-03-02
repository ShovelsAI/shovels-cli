package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var addressesCmd = &cobra.Command{
	Use:   "addresses",
	Short: "Search addresses in the Shovels database",
	Long: `Query the Shovels address database. Subcommands:

  search   Find addresses by query string

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var addressesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search addresses by query string",
	Long: `Search the Shovels address database with a text query. The query matches
against address fields (street, city, state, zip).

Required flags:
  --query, -q   Search string (e.g. "123 Main St", "San Francisco")

Example:
  shovels addresses search --query "123 Main St"
  shovels addresses search -q "San Francisco"
  shovels addresses search --query "90210" --limit 10

Response: {"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runAddressesSearch,
}

func runAddressesSearch(cmd *cobra.Command, args []string) error {
	query, _ := cmd.Flags().GetString("query")
	if query == "" {
		output.PrintErrorTyped(os.Stderr, "query is required", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	q := url.Values{
		"q": {query},
	}

	result, err := cl.Paginate(context.Background(), "/addresses", q, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits)
	return nil
}

func init() {
	addressesSearchCmd.Flags().StringP("query", "q", "", "Search string for address lookup (required)")

	addressesCmd.AddCommand(addressesSearchCmd)
	rootCmd.AddCommand(addressesCmd)
}

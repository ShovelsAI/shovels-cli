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
	Short: "Search addresses by street, city, state, or zip code",
	Long: `Query the Shovels address database.

Available subcommands:
  search   Search addresses by street name, city, state, or zip code

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var addressesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search addresses by street name, city, state, or zip code",
	Long: `Search the Shovels address database. The query matches against street name,
city, state, and zip code fields.

Required flags:
  --query, -q TEXT   Address search string, e.g. "123 Main St" or "90210" (required)

Examples:
  Search by street address:
    shovels addresses search --query "123 Main St"

  Search by city name:
    shovels addresses search -q "San Francisco"

  Search by zip code with result limit:
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

	result, err := cl.Paginate(context.Background(), "/addresses/search", q, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

func init() {
	addressesSearchCmd.Flags().StringP("query", "q", "", "Address search string, e.g. \"123 Main St\" or \"90210\" (required)")

	addressesCmd.AddCommand(addressesSearchCmd)
	rootCmd.AddCommand(addressesCmd)
}

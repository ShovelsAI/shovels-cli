package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var zipcodesCmd = &cobra.Command{
	Use:   "zipcodes",
	Short: "Search zip codes to find geo_ids for use with --geo-id",
	Long: `Search the Shovels zip code database to find 5-digit zip code geo_ids.

Available subcommands:
  search    Search zip codes by prefix or full code

Zip code geo_ids are used directly as 5-digit codes with --geo-id on other
commands like permits search and contractors search. No resolution step needed
for zip codes you already know, but this command helps discover zip codes by
partial prefix.

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var zipcodesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search zip codes by prefix to find geo_ids for use with --geo-id",
	Long: `Search the Shovels zip code database. Returns zip code objects with geo_id
(the 5-digit zip code string) and state fields. Use the geo_id value directly
with --geo-id on permits search, contractors search, and other commands.

Required flags:
  --query, -q TEXT   Zip code prefix or full code to search for, e.g. "902" or "90210" (required)

Examples:
  Search by prefix:
    shovels zipcodes search -q "902"

  Search by full zip code:
    shovels zipcodes search -q "90210"

  Limit results:
    shovels zipcodes search -q "9" --limit 5

Workflow — find zip codes, then search permits:
  shovels zipcodes search -q "902" | jq '.data[].geo_id'
  shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"geo_id": "90210", "state": "CA"}, ...], "meta": {"count": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runZipcodesSearch,
}

func runZipcodesSearch(cmd *cobra.Command, args []string) error {
	query, _ := cmd.Flags().GetString("query")
	if query == "" {
		output.PrintErrorTyped(os.Stderr, "required flag missing: --query (-q)", 1, client.ErrorTypeValidation)
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

	result, err := cl.Paginate(context.Background(), "/zipcodes/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

func init() {
	zipcodesSearchCmd.Flags().StringP("query", "q", "", "Zip code prefix or full code to search for, e.g. \"902\" or \"90210\" (required)")

	zipcodesCmd.AddCommand(zipcodesSearchCmd)
	rootCmd.AddCommand(zipcodesCmd)
}

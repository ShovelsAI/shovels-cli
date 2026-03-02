package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var countiesCmd = &cobra.Command{
	Use:   "counties",
	Short: "Search counties to resolve geo_ids for county-level queries",
	Long: `Query the Shovels county database to resolve county names into geo_ids.

Available subcommands:
  search   Search counties by name to get their geo_id for use in --geo-id

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var countiesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search counties by name to get their geo_id for use in --geo-id",
	Long: `Search the Shovels county database. Returns county objects with geo_id, name,
and state fields. Use the geo_id value in --geo-id flags on permits and
contractors searches.

Required flags:
  --query, -q TEXT   County name to search for, e.g. "Los Angeles" or "Cook" (required)

Examples:
  Find a county's geo_id:
    shovels counties search --query "Los Angeles"

  Use short flag:
    shovels counties search -q "Cook"

  Limit results:
    shovels counties search -q "Washington" --limit 5

Workflow — resolve a county, then search permits:
  GEO=$(shovels counties search -q "Los Angeles" | jq -r '.data[0].geo_id')
  shovels permits search --geo-id "$GEO" --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"geo_id": "...", "name": "LOS ANGELES, CA", "state": "CA"}, ...], "meta": {"count": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runCountiesSearch,
}

func runCountiesSearch(cmd *cobra.Command, args []string) error {
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

	result, err := cl.Paginate(context.Background(), "/counties/search", q, lc)
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
	countiesSearchCmd.Flags().StringP("query", "q", "", "County name to search for, e.g. \"Los Angeles\" or \"Cook\" (required)")

	countiesCmd.AddCommand(countiesSearchCmd)
	rootCmd.AddCommand(countiesCmd)
}

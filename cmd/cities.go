package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var citiesCmd = &cobra.Command{
	Use:   "cities",
	Short: "Search cities to resolve geo_ids for city-level queries",
	Long: `Query the Shovels city database to resolve city names into geo_ids.

Available subcommands:
  search   Search cities by name to get their geo_id for use in --geo-id

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var citiesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search cities by name to get their geo_id for use in --geo-id",
	Long: `Search the Shovels city database. Returns city objects with geo_id, name,
and state fields. Use the geo_id value in --geo-id flags on permits and
contractors searches.

Required flags:
  --query, -q TEXT   City name to search for, e.g. "Miami" or "San Francisco" (required)

Examples:
  Find a city's geo_id:
    shovels cities search --query "Miami"

  Use short flag:
    shovels cities search -q "San Francisco"

  Limit results:
    shovels cities search -q "Portland" --limit 5

Workflow — resolve a city, then search permits:
  GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
  shovels permits search --geo-id "$GEO" --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"geo_id": "...", "name": "MIAMI, MIAMI-DADE, FL", "state": "FL"}, ...], "meta": {"count": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runCitiesSearch,
}

func runCitiesSearch(cmd *cobra.Command, args []string) error {
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

	result, err := cl.Paginate(context.Background(), "/cities/search", q, lc)
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
	citiesSearchCmd.Flags().StringP("query", "q", "", "City name to search for, e.g. \"Miami\" or \"San Francisco\" (required)")

	citiesCmd.AddCommand(citiesSearchCmd)
	rootCmd.AddCommand(citiesCmd)
}

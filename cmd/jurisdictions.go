package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var jurisdictionsCmd = &cobra.Command{
	Use:   "jurisdictions",
	Short: "Search jurisdictions to resolve geo_ids for jurisdiction-level queries",
	Long: `Query the Shovels jurisdiction database to resolve jurisdiction names into geo_ids.

Available subcommands:
  search   Search jurisdictions by name to get their geo_id for use in --geo-id

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var jurisdictionsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search jurisdictions by name to get their geo_id for use in --geo-id",
	Long: `Search the Shovels jurisdiction database. Returns jurisdiction objects with geo_id,
name, and state fields. Use the geo_id value in --geo-id flags on permits and
contractors searches.

Required flags:
  --query, -q TEXT   Jurisdiction name to search for, e.g. "Portland" or "Miami-Dade" (required)

Examples:
  Find a jurisdiction's geo_id:
    shovels jurisdictions search --query "Portland"

  Use short flag:
    shovels jurisdictions search -q "Miami-Dade"

  Limit results:
    shovels jurisdictions search -q "Washington" --limit 5

Workflow — resolve a jurisdiction, then search permits:
  GEO=$(shovels jurisdictions search -q "Portland" | jq -r '.data[0].geo_id')
  shovels permits search --geo-id "$GEO" --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"geo_id": "...", "name": "PORTLAND, OR", "state": "OR"}, ...], "meta": {"count": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runJurisdictionsSearch,
}

func runJurisdictionsSearch(cmd *cobra.Command, args []string) error {
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

	result, err := cl.Paginate(context.Background(), "/jurisdictions/search", q, lc)
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
	jurisdictionsSearchCmd.Flags().StringP("query", "q", "", "Jurisdiction name to search for, e.g. \"Portland\" or \"Miami-Dade\" (required)")

	jurisdictionsCmd.AddCommand(jurisdictionsSearchCmd)
	rootCmd.AddCommand(jurisdictionsCmd)
}

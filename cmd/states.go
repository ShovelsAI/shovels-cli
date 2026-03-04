package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var statesCmd = &cobra.Command{
	Use:   "states",
	Short: "Search US states to find geo_ids for use with --geo-id",
	Long: `Search the Shovels US state database to find 2-letter state abbreviation geo_ids.

Available subcommands:
  search    Search states by name or abbreviation

State geo_ids are 2-letter abbreviations (CA, TX, NY) used directly with --geo-id
on other commands like permits search and contractors search. This command helps
discover the correct abbreviation from a full or partial state name.

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var statesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search states by name to find geo_ids for use with --geo-id",
	Long: `Search the Shovels US state database. Returns state objects with geo_id
(the 2-letter state abbreviation like "CA") and name (full state name like
"California"). Use the geo_id value directly with --geo-id on permits search,
contractors search, and other commands.

Required flags:
  --query, -q TEXT   State name or abbreviation to search for, e.g. "Cal" or "California" (required)

Examples:
  Search by partial name:
    shovels states search -q "Cal"

  Search by full name:
    shovels states search -q "California"

  Limit results:
    shovels states search -q "New" --limit 5

Workflow — find a state abbreviation, then search permits:
  shovels states search -q "California" | jq -r '.data[0].geo_id'
  shovels permits search --geo-id CA --permit-from 2024-01-01 --permit-to 2024-12-31

Response: {"data": [{"geo_id": "CA", "name": "California"}, ...], "meta": {"count": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runStatesSearch,
}

func runStatesSearch(cmd *cobra.Command, args []string) error {
	if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
		return err
	}

	query, _ := cmd.Flags().GetString("query")
	if query == "" {
		output.PrintErrorTyped(os.Stderr, "required flag missing: --query (-q)", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	q := url.Values{
		"q": {query},
	}

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, "/states/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/states/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

func init() {
	statesSearchCmd.Flags().StringP("query", "q", "", "State name or abbreviation to search for, e.g. \"Cal\" or \"California\" (required)")
	registerSchemaFlag(statesSearchCmd)

	statesCmd.AddCommand(statesSearchCmd)
	rootCmd.AddCommand(statesCmd)
}

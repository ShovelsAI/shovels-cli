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
  coverage  Check which permit fields are reliably populated for a state

State geo_ids are 2-letter abbreviations (CA, TX, NY) used directly with --geo-id
on other commands like permits search and contractors search. This command helps
discover the correct abbreviation from a full or partial state name.

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var statesCoverageCmd = newCoverageCmd("state",
	"Check which permit fields are reliably populated for a state",
	`Check per-field data coverage for a US state before running permit queries.
Returns one item per tracked permit field that is NOT reliably populated:
tier "missing" (<10% of permits have it) or "partial" (10-80%). Fields that
are reliable (>=80%) are omitted, so an empty data array means full coverage.

GEO_ID is positional and is the 2-letter state abbreviation (CA, TX, NY):
  shovels states coverage CA --coverage-from 2024-01-01 --coverage-to 2024-12-31

Required flags:
  --coverage-from DATE   Start date in YYYY-MM-DD format (required)
  --coverage-to DATE     End date in YYYY-MM-DD format (required)

This endpoint is credit-exempt and not paginated.

Response: {"data": [{"field": "fees", "tier": "missing", "fill_pct": 0.02, "permits_total": 12034}, ...], "meta": {}}`+coverageWhyMissing)

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

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

func init() {
	statesSearchCmd.Flags().StringP("query", "q", "", "State name or abbreviation to search for, e.g. \"Cal\" or \"California\" (required)")
	registerSchemaFlag(statesSearchCmd)

	statesCmd.AddCommand(statesSearchCmd)
	statesCmd.AddCommand(statesCoverageCmd)
	rootCmd.AddCommand(statesCmd)
}

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

var zipcodesCmd = &cobra.Command{
	Use:   "zipcodes",
	Short: "Search zip codes to find geo_ids for use with --geo-id",
	Long: `Search the Shovels zip code database to find 5-digit zip code geo_ids.

Available subcommands:
  search    Search zip codes by prefix or full code
  coverage  Check which permit fields are reliably populated for a zip code

Zip code geo_ids are used directly as 5-digit codes with --geo-id on other
commands like permits search and contractors search. No resolution step needed
for zip codes you already know, but this command helps discover zip codes by
partial prefix.

There is no zip-scoped metrics command. Geo-scoped metrics exist only for a
resolved address, city, county, or jurisdiction, so a 5-digit code is rejected
there even though it works on permits search, contractors search, and zipcodes
coverage. No substitute reports on a zip: a zip may span several
municipalities, and a city covers areas outside the zip. If city-wide scope is
acceptable for your question, resolve a relevant city and say so in the answer:
  GEO=$(shovels cities search -q "Encinitas" | jq -r '.data[0].geo_id')
  shovels cities metrics current "$GEO" --tag solar --property-type residential

To aggregate over a zip exactly, use permits search, which does accept a zip,
and compute the aggregate yourself.

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var zipcodesCoverageCmd = newCoverageCmd("zipcode",
	"Check which permit fields are reliably populated for a zip code",
	`Check per-field data coverage for a zip code before running permit queries.
Returns one item per tracked permit field that is NOT reliably populated:
tier "missing" (<10% of permits have it) or "partial" (10-80%). Fields that
are reliable (>=80%) are omitted, so an empty data array means full coverage.

GEO_ID is positional and is the 5-digit zip code used directly (no resolution):
  shovels zipcodes coverage 92024 --coverage-from 2024-01-01 --coverage-to 2024-12-31

Required flags:
  --coverage-from DATE   Start date in YYYY-MM-DD format (required)
  --coverage-to DATE     End date in YYYY-MM-DD format (required)

This endpoint is credit-exempt and not paginated.

Response: {"data": [{"field": "fees", "tier": "missing", "fill_pct": 0.02, "permits_total": 12034}, ...], "meta": {}}`+coverageWhyMissing)

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
		return printDryRun(cmd, "/zipcodes/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/zipcodes/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

func init() {
	zipcodesSearchCmd.Flags().StringP("query", "q", "", "Zip code prefix or full code to search for, e.g. \"902\" or \"90210\" (required)")
	registerSchemaFlag(zipcodesSearchCmd)

	zipcodesCmd.AddCommand(zipcodesSearchCmd)
	zipcodesCmd.AddCommand(zipcodesCoverageCmd)
	rootCmd.AddCommand(zipcodesCmd)
}

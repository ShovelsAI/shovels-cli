package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var countiesMetricsCfg = newMetricsConfig("counties", "county", true)

var countiesCmd = &cobra.Command{
	Use:   "counties",
	Short: "Search counties and view permit activity metrics",
	Long: `Query the Shovels county database to resolve county names into geo_ids,
and retrieve permit activity metrics for counties.

Available subcommands:
  search    Search counties by name to get their geo_id for use in --geo-id
  metrics   View permit activity metrics for a county (current snapshot or monthly)
  coverage  Check which permit fields are reliably populated for a county

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var countiesCoverageCmd = newCoverageCmd("county",
	"Check which permit fields are reliably populated for a county",
	`Check per-field data coverage for a county before running permit queries.
Returns one item per tracked permit field that is NOT reliably populated:
tier "missing" (<10% of permits have it) or "partial" (10-80%). Fields that
are reliable (>=80%) are omitted, so an empty data array means full coverage.

GEO_ID is positional. Resolve a county geo_id first with "counties search":
  GEO=$(shovels counties search -q "Los Angeles" | jq -r '.data[0].geo_id')
  shovels counties coverage "$GEO" --coverage-from 2024-01-01 --coverage-to 2024-12-31

Required flags:
  --coverage-from DATE   Start date in YYYY-MM-DD format (required)
  --coverage-to DATE     End date in YYYY-MM-DD format (required)

This endpoint is credit-exempt and not paginated.

Response: {"data": [{"field": "fees", "tier": "missing", "fill_pct": 0.02, "permits_total": 12034}, ...], "meta": {}}`+coverageWhyMissing)

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
		return printDryRun(cmd, "/counties/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/counties/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

var countiesMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "View permit activity metrics for a county (current snapshot or monthly)",
	Long: `Retrieve permit activity metrics for a specific county. Metrics summarize
permit counts, contractor counts, job values, approval durations, and more.

Available subcommands:
  current   Current aggregate metrics snapshot for a county
  monthly   Monthly metrics time series for a county over a date range

Resolve a county geo_id first:
  GEO=$(shovels counties search -q "Los Angeles" | jq -r '.data[0].geo_id')
  shovels counties metrics current "$GEO" --tag solar --property-type residential

` + metricsCoverageTip + `
    shovels counties coverage "$GEO" --coverage-from 2024-01-01 --coverage-to 2024-12-31`,
}

var countiesMetricsCurrentCmd = &cobra.Command{
	Use:   "current GEO_ID",
	Short: "Current aggregate metrics snapshot for a county",
	Long: `Retrieve current permit activity metrics for a county. Returns aggregate data
including permit counts, contractor counts, average construction duration,
total job value, inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational (required)

Optional flags:
  --include-count       Request total result count in meta.total_count

Examples:
  Current solar metrics for a county:
    GEO=$(shovels counties search -q "Los Angeles" | jq -r '.data[0].geo_id')
    shovels counties metrics current "$GEO" --tag solar --property-type residential

  With total count:
    shovels counties metrics current ABC123 --tag solar --property-type residential --include-count

Workflow — resolve county, then query metrics:
  GEO=$(shovels counties search -q "Cook" | jq -r '.data[0].geo_id')
  shovels counties metrics current "$GEO" --tag solar --property-type residential

Response fields: geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsCurrent(countiesMetricsCfg),
}

var countiesMetricsMonthlyCmd = &cobra.Command{
	Use:   "monthly GEO_ID",
	Short: "Monthly metrics time series for a county over a date range",
	Long: `Retrieve monthly permit activity metrics for a county over a specified date
range. Returns one record per month with a date field, plus permit counts,
contractor counts, average construction duration, total job value,
inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational (required)
  --metric-from DATE   Start date in YYYY-MM-DD format (required)
  --metric-to DATE     End date in YYYY-MM-DD format (required)

Optional flags:
  --include-count       Request total result count in meta.total_count

Examples:
  Monthly solar metrics for a county in 2024:
    GEO=$(shovels counties search -q "Los Angeles" | jq -r '.data[0].geo_id')
    shovels counties metrics monthly "$GEO" --tag solar --property-type residential \
      --metric-from 2024-01-01 --metric-to 2024-12-31

Workflow — resolve county, then query monthly metrics:
  GEO=$(shovels counties search -q "Cook" | jq -r '.data[0].geo_id')
  shovels counties metrics monthly "$GEO" --tag solar --property-type residential \
    --metric-from 2024-01-01 --metric-to 2024-12-31

Response fields: date, geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsMonthly(countiesMetricsCfg),
}

func init() {
	countiesSearchCmd.Flags().StringP("query", "q", "", "County name to search for, e.g. \"Los Angeles\" or \"Cook\" (required)")
	registerSchemaFlag(countiesSearchCmd)
	registerSchemaFlag(countiesMetricsCurrentCmd)
	registerSchemaFlag(countiesMetricsMonthlyCmd)

	registerMetricsCurrentFlags(countiesMetricsCurrentCmd, true)
	registerMetricsMonthlyFlags(countiesMetricsMonthlyCmd, true)

	countiesMetricsCmd.AddCommand(countiesMetricsCurrentCmd)
	countiesMetricsCmd.AddCommand(countiesMetricsMonthlyCmd)

	countiesCmd.AddCommand(countiesSearchCmd)
	countiesCmd.AddCommand(countiesMetricsCmd)
	countiesCmd.AddCommand(countiesCoverageCmd)
	rootCmd.AddCommand(countiesCmd)
}

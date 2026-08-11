package cmd

import (
	"context"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var citiesMetricsCfg = newMetricsConfig("cities", "city", true)

var citiesCmd = &cobra.Command{
	Use:   "cities",
	Short: "Search cities and view permit activity metrics",
	Long: `Query the Shovels city database to resolve city names into geo_ids,
and retrieve permit activity metrics for cities.

Available subcommands:
  search    Search cities by name to get their geo_id for use in --geo-id
  metrics   View permit activity metrics for a city (current snapshot or monthly)
  coverage  Check which permit fields are reliably populated for a city

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var citiesCoverageCmd = newCoverageCmd("city",
	"Check which permit fields are reliably populated for a city",
	`Check per-field data coverage for a city before running permit queries.
Returns one item per tracked permit field that is NOT reliably populated:
tier "missing" (<10% of permits have it) or "partial" (10-80%). Fields that
are reliable (>=80%) are omitted, so an empty data array means full coverage.

GEO_ID is positional. Resolve a city geo_id first with "cities search":
  GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
  shovels cities coverage "$GEO" --coverage-from 2024-01-01 --coverage-to 2024-12-31

Required flags:
  --coverage-from DATE   Start date in YYYY-MM-DD format (required)
  --coverage-to DATE     End date in YYYY-MM-DD format (required)

This endpoint is credit-exempt and not paginated.

Response: {"data": [{"field": "fees", "tier": "missing", "fill_pct": 0.02, "permits_total": 12034}, ...], "meta": {}}`+coverageWhyMissing)

var citiesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search cities by name to get their geo_id for use in --geo-id",
	Long: `Search the Shovels city database. Returns city objects with geo_id, name,
and state fields. Use the geo_id value in --geo-id flags on permits and
contractors searches.
` + serverCappedNote("cities search") + `
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
		return printDryRun(cmd, "/cities/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/cities/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

var citiesMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "View permit activity metrics for a city (current snapshot or monthly)",
	Long: `Retrieve permit activity metrics for a specific city. Metrics summarize
permit counts, contractor counts, job values, approval durations, and more.

Available subcommands:
  current   Current aggregate metrics snapshot for a city
  monthly   Monthly metrics time series for a city over a date range

Resolve a city geo_id first:
  GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
  shovels cities metrics current "$GEO" --tag solar --property-type residential

` + metricsCoverageTip + `
    shovels cities coverage "$GEO" --coverage-from 2024-01-01 --coverage-to 2024-12-31`,
}

var citiesMetricsCurrentCmd = &cobra.Command{
	Use:   "current GEO_ID",
	Short: "Current aggregate metrics snapshot for a city",
	Long: `Retrieve current permit activity metrics for a city. Returns aggregate data
including permit counts, contractor counts, average construction duration,
total job value, inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial, agricultural, vacant land, exempt, miscellaneous, office, recreational (required)

Optional flags:
  --include-count       Request total result count in meta.total_count

Examples:
  Current solar metrics for a city:
    GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
    shovels cities metrics current "$GEO" --tag solar --property-type residential

  With total count:
    shovels cities metrics current SfAy51LPDMc --tag solar --property-type residential --include-count

Workflow — resolve city, then query metrics:
  GEO=$(shovels cities search -q "San Diego" | jq -r '.data[0].geo_id')
  shovels cities metrics current "$GEO" --tag solar --property-type residential

Response fields: geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsCurrent(citiesMetricsCfg),
}

var citiesMetricsMonthlyCmd = &cobra.Command{
	Use:   "monthly GEO_ID",
	Short: "Monthly metrics time series for a city over a date range",
	Long: `Retrieve monthly permit activity metrics for a city over a specified date
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
  Monthly solar metrics for 2024:
    GEO=$(shovels cities search -q "Miami" | jq -r '.data[0].geo_id')
    shovels cities metrics monthly "$GEO" --tag solar --property-type residential \
      --metric-from 2024-01-01 --metric-to 2024-12-31

Workflow — resolve city, then query monthly metrics:
  GEO=$(shovels cities search -q "San Diego" | jq -r '.data[0].geo_id')
  shovels cities metrics monthly "$GEO" --tag solar --property-type residential \
    --metric-from 2024-01-01 --metric-to 2024-12-31

Response fields: date, geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsMonthly(citiesMetricsCfg),
}

func init() {
	citiesSearchCmd.Flags().StringP("query", "q", "", "City name to search for, e.g. \"Miami\" or \"San Francisco\" (required)")
	registerSchemaFlag(citiesSearchCmd)
	registerSchemaFlag(citiesMetricsCurrentCmd)
	registerSchemaFlag(citiesMetricsMonthlyCmd)

	registerMetricsCurrentFlags(citiesMetricsCurrentCmd, true)
	registerMetricsMonthlyFlags(citiesMetricsMonthlyCmd, true)

	citiesMetricsCmd.AddCommand(citiesMetricsCurrentCmd)
	citiesMetricsCmd.AddCommand(citiesMetricsMonthlyCmd)

	citiesCmd.AddCommand(citiesSearchCmd)
	citiesCmd.AddCommand(citiesMetricsCmd)
	citiesCmd.AddCommand(citiesCoverageCmd)
	rootCmd.AddCommand(citiesCmd)
}

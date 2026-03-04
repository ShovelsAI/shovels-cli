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

var jurisdictionsMetricsCfg = metricsConfig{
	resource:            "jurisdictions",
	requirePropertyType: true,
}

var jurisdictionsCmd = &cobra.Command{
	Use:   "jurisdictions",
	Short: "Search jurisdictions and view permit activity metrics",
	Long: `Query the Shovels jurisdiction database to resolve jurisdiction names into geo_ids,
and retrieve permit activity metrics for jurisdictions.

Available subcommands:
  search    Search jurisdictions by name to get their geo_id for use in --geo-id
  metrics   View permit activity metrics for a jurisdiction (current snapshot or monthly)

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

	q := url.Values{
		"q": {query},
	}

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, "/jurisdictions/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/jurisdictions/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

var jurisdictionsMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "View permit activity metrics for a jurisdiction (current snapshot or monthly)",
	Long: `Retrieve permit activity metrics for a specific jurisdiction. Metrics summarize
permit counts, contractor counts, job values, approval durations, and more.

Available subcommands:
  current   Current aggregate metrics snapshot for a jurisdiction
  monthly   Monthly metrics time series for a jurisdiction over a date range

Resolve a jurisdiction geo_id first:
  GEO=$(shovels jurisdictions search -q "Portland" | jq -r '.data[0].geo_id')
  shovels jurisdictions metrics current "$GEO" --tag solar --property-type residential`,
}

var jurisdictionsMetricsCurrentCmd = &cobra.Command{
	Use:   "current GEO_ID",
	Short: "Current aggregate metrics snapshot for a jurisdiction",
	Long: `Retrieve current permit activity metrics for a jurisdiction. Returns aggregate
data including permit counts, contractor counts, average construction duration,
total job value, inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial (required)

Optional flags:
  --include-count       Request total result count in meta.total_count

Examples:
  Current solar metrics for a jurisdiction:
    GEO=$(shovels jurisdictions search -q "Portland" | jq -r '.data[0].geo_id')
    shovels jurisdictions metrics current "$GEO" --tag solar --property-type residential

  With total count:
    shovels jurisdictions metrics current ABC123 --tag solar --property-type residential --include-count

Workflow — resolve jurisdiction, then query metrics:
  GEO=$(shovels jurisdictions search -q "Miami-Dade" | jq -r '.data[0].geo_id')
  shovels jurisdictions metrics current "$GEO" --tag solar --property-type residential

Response fields: geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsCurrent(jurisdictionsMetricsCfg),
}

var jurisdictionsMetricsMonthlyCmd = &cobra.Command{
	Use:   "monthly GEO_ID",
	Short: "Monthly metrics time series for a jurisdiction over a date range",
	Long: `Retrieve monthly permit activity metrics for a jurisdiction over a specified
date range. Returns one record per month with a date field, plus permit counts,
contractor counts, average construction duration, total job value,
inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial (required)
  --metric-from DATE   Start date in YYYY-MM-DD format (required)
  --metric-to DATE     End date in YYYY-MM-DD format (required)

Optional flags:
  --include-count       Request total result count in meta.total_count

Examples:
  Monthly solar metrics for a jurisdiction in 2024:
    GEO=$(shovels jurisdictions search -q "Portland" | jq -r '.data[0].geo_id')
    shovels jurisdictions metrics monthly "$GEO" --tag solar --property-type residential \
      --metric-from 2024-01-01 --metric-to 2024-12-31

Workflow — resolve jurisdiction, then query monthly metrics:
  GEO=$(shovels jurisdictions search -q "Miami-Dade" | jq -r '.data[0].geo_id')
  shovels jurisdictions metrics monthly "$GEO" --tag solar --property-type residential \
    --metric-from 2024-01-01 --metric-to 2024-12-31

Response fields: date, geo_id, tag, property_type, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsMonthly(jurisdictionsMetricsCfg),
}

func init() {
	jurisdictionsSearchCmd.Flags().StringP("query", "q", "", "Jurisdiction name to search for, e.g. \"Portland\" or \"Miami-Dade\" (required)")

	registerMetricsCurrentFlags(jurisdictionsMetricsCurrentCmd, true)
	registerMetricsMonthlyFlags(jurisdictionsMetricsMonthlyCmd, true)

	jurisdictionsMetricsCmd.AddCommand(jurisdictionsMetricsCurrentCmd)
	jurisdictionsMetricsCmd.AddCommand(jurisdictionsMetricsMonthlyCmd)

	jurisdictionsCmd.AddCommand(jurisdictionsSearchCmd)
	jurisdictionsCmd.AddCommand(jurisdictionsMetricsCmd)
	rootCmd.AddCommand(jurisdictionsCmd)
}

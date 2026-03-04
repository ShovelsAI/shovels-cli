package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var citiesCmd = &cobra.Command{
	Use:   "cities",
	Short: "Search cities and view permit activity metrics",
	Long: `Query the Shovels city database to resolve city names into geo_ids,
and retrieve permit activity metrics for cities.

Available subcommands:
  search    Search cities by name to get their geo_id for use in --geo-id
  metrics   View permit activity metrics for a city (current snapshot or monthly)

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
  shovels cities metrics current "$GEO" --tag solar --property-type residential`,
}

var citiesMetricsCurrentCmd = &cobra.Command{
	Use:   "current GEO_ID",
	Short: "Current aggregate metrics snapshot for a city",
	Long: `Retrieve current permit activity metrics for a city. Returns aggregate data
including permit counts, contractor counts, average construction duration,
total job value, inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT            Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --property-type TEXT  Property type: residential, commercial, industrial (required)

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
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runCitiesMetricsCurrent,
}

func runCitiesMetricsCurrent(cmd *cobra.Command, args []string) error {
	tag, _ := cmd.Flags().GetString("tag")
	propertyType, _ := cmd.Flags().GetString("property-type")

	var missing []string
	if tag == "" {
		missing = append(missing, "--tag")
	}
	if propertyType == "" {
		missing = append(missing, "--property-type")
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
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
		"tag":           {tag},
		"property_type": {propertyType},
	}
	setBoolFlag(cmd, "include-count", "include_count", q)

	endpoint := fmt.Sprintf("/cities/%s/metrics/current", args[0])
	result, err := cl.Paginate(context.Background(), endpoint, q, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, result.TotalCount)
	return nil
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
  --property-type TEXT  Property type: residential, commercial, industrial (required)
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
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runCitiesMetricsMonthly,
}

func runCitiesMetricsMonthly(cmd *cobra.Command, args []string) error {
	tag, _ := cmd.Flags().GetString("tag")
	propertyType, _ := cmd.Flags().GetString("property-type")
	metricFrom, _ := cmd.Flags().GetString("metric-from")
	metricTo, _ := cmd.Flags().GetString("metric-to")

	var missing []string
	if tag == "" {
		missing = append(missing, "--tag")
	}
	if propertyType == "" {
		missing = append(missing, "--property-type")
	}
	if metricFrom == "" {
		missing = append(missing, "--metric-from")
	}
	if metricTo == "" {
		missing = append(missing, "--metric-to")
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if !datePattern.MatchString(metricFrom) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --metric-from: %q (expected YYYY-MM-DD)", metricFrom), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if !datePattern.MatchString(metricTo) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --metric-to: %q (expected YYYY-MM-DD)", metricTo), 1, client.ErrorTypeValidation)
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
		"tag":           {tag},
		"property_type": {propertyType},
		"metric_from":   {metricFrom},
		"metric_to":     {metricTo},
	}
	setBoolFlag(cmd, "include-count", "include_count", q)

	endpoint := fmt.Sprintf("/cities/%s/metrics/monthly", args[0])
	result, err := cl.Paginate(context.Background(), endpoint, q, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, result.TotalCount)
	return nil
}

func init() {
	citiesSearchCmd.Flags().StringP("query", "q", "", "City name to search for, e.g. \"Miami\" or \"San Francisco\" (required)")

	// Metrics current flags
	citiesMetricsCurrentCmd.Flags().String("tag", "", "Permit tag: solar, roofing, electrical, plumbing, etc. (required)")
	citiesMetricsCurrentCmd.Flags().String("property-type", "", "Property type: residential, commercial, industrial (required)")
	citiesMetricsCurrentCmd.Flags().Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")
	rejectDateFlagsOnCurrent(citiesMetricsCurrentCmd)

	// Metrics monthly flags
	citiesMetricsMonthlyCmd.Flags().String("tag", "", "Permit tag: solar, roofing, electrical, plumbing, etc. (required)")
	citiesMetricsMonthlyCmd.Flags().String("property-type", "", "Property type: residential, commercial, industrial (required)")
	citiesMetricsMonthlyCmd.Flags().String("metric-from", "", "Start date in YYYY-MM-DD format (required)")
	citiesMetricsMonthlyCmd.Flags().String("metric-to", "", "End date in YYYY-MM-DD format (required)")
	citiesMetricsMonthlyCmd.Flags().Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")

	citiesMetricsCmd.AddCommand(citiesMetricsCurrentCmd)
	citiesMetricsCmd.AddCommand(citiesMetricsMonthlyCmd)

	citiesCmd.AddCommand(citiesSearchCmd)
	citiesCmd.AddCommand(citiesMetricsCmd)
	rootCmd.AddCommand(citiesCmd)
}

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

var addressesMetricsCfg = metricsConfig{
	resource:            "addresses",
	requirePropertyType: false,
}

var addressesCmd = &cobra.Command{
	Use:   "addresses",
	Short: "Search addresses, view metrics, and look up residents",
	Long: `Query the Shovels address database to search addresses, retrieve
permit activity metrics, and look up resident information.

Available subcommands:
  search     Search addresses by street name, city, state, or zip code
  metrics    View permit activity metrics for an address (current snapshot or monthly)
  residents  Look up residents at a specific address (personal information)

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var addressesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search addresses by street name, city, state, or zip code",
	Long: `Search the Shovels address database. The query matches against street name,
city, state, and zip code fields.

Required flags:
  --query, -q TEXT   Address search string, e.g. "123 Main St" or "90210" (required)

Examples:
  Search by street address:
    shovels addresses search --query "123 Main St"

  Search by city name:
    shovels addresses search -q "San Francisco"

  Search by zip code with result limit:
    shovels addresses search --query "90210" --limit 10

Response: {"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runAddressesSearch,
}

func runAddressesSearch(cmd *cobra.Command, args []string) error {
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
		return printDryRun(cmd, "/addresses/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/addresses/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

var addressesMetricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "View permit activity metrics for an address (current snapshot or monthly)",
	Long: `Retrieve permit activity metrics for a specific address. Metrics summarize
permit counts, contractor counts, job values, approval durations, and more.

Available subcommands:
  current   Current aggregate metrics snapshot for an address
  monthly   Monthly metrics time series for an address over a date range

Resolve an address geo_id first:
  GEO=$(shovels addresses search -q "123 Main St, Miami, FL" | jq -r '.data[0].geo_id')
  shovels addresses metrics current "$GEO" --tag solar`,
}

var addressesMetricsCurrentCmd = &cobra.Command{
	Use:   "current GEO_ID",
	Short: "Current aggregate metrics snapshot for an address",
	Long: `Retrieve current permit activity metrics for an address. Returns aggregate data
including permit counts, contractor counts, average construction duration,
total job value, inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT   Permit tag: solar, roofing, electrical, plumbing, etc. (required)

Optional flags:
  --include-count   Request total result count in meta.total_count

Examples:
  Current solar metrics for an address:
    GEO=$(shovels addresses search -q "123 Main St, Miami, FL" | jq -r '.data[0].geo_id')
    shovels addresses metrics current "$GEO" --tag solar

  With total count:
    shovels addresses metrics current ABC123 --tag solar --include-count

Workflow — resolve address, then query metrics:
  GEO=$(shovels addresses search -q "456 Oak Ave, Portland, OR" | jq -r '.data[0].geo_id')
  shovels addresses metrics current "$GEO" --tag solar

Response fields: geo_id, tag, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsCurrent(addressesMetricsCfg),
}

var addressesMetricsMonthlyCmd = &cobra.Command{
	Use:   "monthly GEO_ID",
	Short: "Monthly metrics time series for an address over a date range",
	Long: `Retrieve monthly permit activity metrics for an address over a specified date
range. Returns one record per month with a date field, plus permit counts,
contractor counts, average construction duration, total job value,
inspection pass rate, and active/in-review permit counts.

Required flags:
  --tag TEXT          Permit tag: solar, roofing, electrical, plumbing, etc. (required)
  --metric-from DATE Start date in YYYY-MM-DD format (required)
  --metric-to DATE   End date in YYYY-MM-DD format (required)

Optional flags:
  --include-count     Request total result count in meta.total_count

Examples:
  Monthly solar metrics for an address in 2024:
    GEO=$(shovels addresses search -q "123 Main St, Miami, FL" | jq -r '.data[0].geo_id')
    shovels addresses metrics monthly "$GEO" --tag solar \
      --metric-from 2024-01-01 --metric-to 2024-12-31

Workflow — resolve address, then query monthly metrics:
  GEO=$(shovels addresses search -q "456 Oak Ave, Portland, OR" | jq -r '.data[0].geo_id')
  shovels addresses metrics monthly "$GEO" --tag solar \
    --metric-from 2024-01-01 --metric-to 2024-12-31

Response fields: date, geo_id, tag, permit_count, contractor_count,
avg_construction_duration, avg_approval_duration, total_job_value,
avg_inspection_pass_rate, permit_active_count, permit_in_review_count`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runMetricsMonthly(addressesMetricsCfg),
}

var addressesResidentsCmd = &cobra.Command{
	Use:   "residents GEO_ID",
	Short: "Look up residents at a specific address",
	Long: `Retrieve resident records for a specific address. This endpoint returns
personal information including name, email, phone, LinkedIn URL, net worth,
income range, and homeowner status. Access is controlled server-side by your
API plan — requests return 403 if your account lacks resident data access.

Positional argument:
  GEO_ID   Address geo_id (resolve via addresses search first)

Examples:
  Look up residents at an address:
    GEO=$(shovels addresses search -q "123 Main St, Miami, FL" | jq -r '.data[0].geo_id')
    shovels addresses residents "$GEO"

  Limit to first 10 residents:
    shovels addresses residents "$GEO" --limit 10

Workflow — resolve address, then look up residents:
  GEO=$(shovels addresses search -q "456 Oak Ave, Portland, OR" | jq -r '.data[0].geo_id')
  shovels addresses residents "$GEO"

Response fields: name, personal_emails, phone, linkedin_url, net_worth,
income_range, is_homeowner, street_no, street, city, state, zip_code,
zip_code_ext

Response: {"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, ...}}`,
	Args: exactArgsUnlessSchema(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runAddressesResidents,
}

func runAddressesResidents(cmd *cobra.Command, args []string) error {
	if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
		return err
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("/addresses/%s/residents", args[0])

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q := url.Values{}
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, endpoint, q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), endpoint, nil, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits, nil)
	return nil
}

func init() {
	addressesSearchCmd.Flags().StringP("query", "q", "", "Address search string, e.g. \"123 Main St\" or \"90210\" (required)")
	registerSchemaFlag(addressesSearchCmd)
	registerSchemaFlag(addressesMetricsCurrentCmd)
	registerSchemaFlag(addressesMetricsMonthlyCmd)
	registerSchemaFlag(addressesResidentsCmd)

	registerMetricsCurrentFlags(addressesMetricsCurrentCmd, false)
	registerMetricsMonthlyFlags(addressesMetricsMonthlyCmd, false)

	addressesMetricsCmd.AddCommand(addressesMetricsCurrentCmd)
	addressesMetricsCmd.AddCommand(addressesMetricsMonthlyCmd)

	addressesCmd.AddCommand(addressesSearchCmd)
	addressesCmd.AddCommand(addressesMetricsCmd)
	addressesCmd.AddCommand(addressesResidentsCmd)
	rootCmd.AddCommand(addressesCmd)
}

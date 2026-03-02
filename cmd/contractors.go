package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

var contractorsCmd = &cobra.Command{
	Use:   "contractors",
	Short: "Search and retrieve contractors",
	Long: `Query the Shovels contractor database. Subcommands:

  search   Find contractors by location, date range, tags, and performance metrics

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var contractorsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search contractors by location, date, and filters",
	Long: `Search the Shovels contractor database with required location and date filters
plus optional permit, property, and contractor filters.

Required flags:
  --geo-id    Geographic filter (e.g. ZIP_90210, CITY_LOS_ANGELES_CA)
  --from      Start date in YYYY-MM-DD format
  --to        End date in YYYY-MM-DD format

Example:
  shovels contractors search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31

Filter by classification:
  shovels contractors search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --contractor-classification general_building

Skip tallies for faster response:
  shovels contractors search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --no-tallies`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsSearch,
}

func runContractorsSearch(cmd *cobra.Command, args []string) error {
	// --- Validate required flags ---
	geoID, _ := cmd.Flags().GetString("geo-id")
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")

	var missing []string
	if geoID == "" {
		missing = append(missing, "--geo-id")
	}
	if from == "" {
		missing = append(missing, "--from")
	}
	if to == "" {
		missing = append(missing, "--to")
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	// --- Validate date formats ---
	if !datePattern.MatchString(from) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --from: %q (expected YYYY-MM-DD)", from), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if !datePattern.MatchString(to) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --to: %q (expected YYYY-MM-DD)", to), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	// --- Validate --query length ---
	query, _ := cmd.Flags().GetString("query")
	if len(query) > 50 {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("--query must be at most 50 characters, got %d", len(query)), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	// --- Validate --status values ---
	statuses, _ := cmd.Flags().GetStringSlice("status")
	for _, s := range statuses {
		if !isValidStatus(s) {
			msg := fmt.Sprintf("invalid --status value %q: valid options are %s", s, strings.Join(validPermitStatuses, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}

	// --- Parse --limit and --max-records ---
	limitStr, _ := cmd.Flags().GetString("limit")
	lc, err := client.ParseLimit(limitStr)
	if err != nil {
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}
	maxRecords, _ := cmd.Flags().GetInt("max-records")
	if err := client.ValidateMaxRecords(maxRecords); err != nil {
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}
	if lc.All {
		lc = lc.WithMaxRecords(maxRecords)
	}

	// --- Build query params ---
	q := url.Values{
		"geo_id":      {geoID},
		"permit_from": {from},
		"permit_to":   {to},
	}

	// Permit filters
	tags, _ := cmd.Flags().GetStringSlice("tags")
	for _, tag := range tags {
		q.Add("permit_tags", tag)
	}
	if query != "" {
		q.Set("permit_q", query)
	}
	for _, s := range statuses {
		q.Add("permit_status", s)
	}

	setIntFlag(cmd, "min-approval-duration", "permit_min_approval_duration", q)
	setIntFlag(cmd, "min-construction-duration", "permit_min_construction_duration", q)
	setIntFlag(cmd, "min-inspection-pr", "permit_min_inspection_pr", q)
	setIntFlag(cmd, "min-job-value", "permit_min_job_value", q)
	setIntFlag(cmd, "min-fees", "permit_min_fees", q)

	// Property filters
	setStringFlag(cmd, "property-type", "property_type", q)
	setIntFlag(cmd, "property-min-market-value", "property_min_market_value", q)
	setIntFlag(cmd, "property-min-building-area", "property_min_building_area", q)
	setIntFlag(cmd, "property-min-lot-size", "property_min_lot_size", q)
	setIntFlag(cmd, "property-min-story-count", "property_min_story_count", q)
	setIntFlag(cmd, "property-min-unit-count", "property_min_unit_count", q)

	// Contractor filters
	classifications, _ := cmd.Flags().GetStringSlice("contractor-classification")
	for _, c := range classifications {
		q.Add("contractor_classification_derived", c)
	}
	setStringFlag(cmd, "contractor-name", "contractor_name", q)
	setStringFlag(cmd, "contractor-website", "contractor_website", q)
	setIntFlag(cmd, "contractor-min-total-job-value", "contractor_min_total_job_value", q)
	setIntFlag(cmd, "contractor-min-total-permits-count", "contractor_min_total_permits_count", q)
	setIntFlag(cmd, "contractor-min-inspection-pr", "contractor_min_inspection_pr", q)
	setStringFlag(cmd, "contractor-license", "contractor_license", q)

	// Contractors-specific: --no-tallies sends include_tallies=false to the API.
	noTallies, _ := cmd.Flags().GetBool("no-tallies")
	if noTallies {
		q.Set("include_tallies", "false")
	}

	// --- Create client and paginate ---
	cfg := ResolvedConfig()

	timeoutStr, _ := cmd.Flags().GetString("timeout")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		output.PrintErrorTyped(os.Stderr, "invalid timeout: "+timeoutStr, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	noRetry, _ := cmd.Flags().GetBool("no-retry")

	cl := client.New(client.Options{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Timeout: timeout,
		NoRetry: noRetry,
		Version: buildVersion,
	})

	result, err := cl.Paginate(context.Background(), "/contractors/search", q, lc)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	output.PrintPaginated(cmd.OutOrStdout(), result.Items, result.HasMore, result.Credits)
	return nil
}

func init() {
	f := contractorsSearchCmd.Flags()

	// Required filters
	f.String("geo-id", "", "Geographic filter ID (e.g. ZIP_90210, CITY_LOS_ANGELES_CA, COUNTY_LOS_ANGELES_CA, STATE_CA)")
	f.String("from", "", "Start date for permit filing (YYYY-MM-DD format, required)")
	f.String("to", "", "End date for permit filing (YYYY-MM-DD format, required)")

	// Permit filters
	f.StringSlice("tags", nil, "Permit tags to filter by (AND logic). Prefix with - to exclude (e.g. --tags=-roofing)")
	f.String("query", "", "Substring search in permit description (case-insensitive, max 50 chars)")
	f.StringSlice("status", nil, "Permit status filter: final, in_review, inactive, active")
	f.Int("min-approval-duration", 0, "Minimum approval duration in days")
	f.Int("min-construction-duration", 0, "Minimum construction duration in days")
	f.Int("min-inspection-pr", 0, "Minimum inspection pass rate (0-100)")
	f.Int("min-job-value", 0, "Minimum job value in dollars")
	f.Int("min-fees", 0, "Minimum permit fees in dollars")

	// Property filters
	f.String("property-type", "", "Property type filter (e.g. residential, commercial, industrial)")
	f.Int("property-min-market-value", 0, "Minimum assessed market value in dollars")
	f.Int("property-min-building-area", 0, "Minimum building area in square feet")
	f.Int("property-min-lot-size", 0, "Minimum lot size in square feet")
	f.Int("property-min-story-count", 0, "Minimum number of stories")
	f.Int("property-min-unit-count", 0, "Minimum number of units")

	// Contractor filters
	f.StringSlice("contractor-classification", nil, "Contractor classification filter (AND logic, prefix with - to exclude)")
	f.String("contractor-name", "", "Filter by contractor name or partial name")
	f.String("contractor-website", "", "Filter by contractor website (exclude http/https prefix)")
	f.Int("contractor-min-total-job-value", 0, "Minimum lifetime contractor job value in dollars")
	f.Int("contractor-min-total-permits-count", 0, "Minimum lifetime permits count")
	f.Int("contractor-min-inspection-pr", 0, "Minimum lifetime inspection pass rate (0-100)")
	f.String("contractor-license", "", "Filter by contractor license number")

	// Contractors-specific
	f.Bool("no-tallies", false, "Omit tag and status tallies from response for faster results (sends include_tallies=false)")

	setGroupedUsage(contractorsSearchCmd, []flagGroup{
		{
			Title: "Required Flags",
			Names: []string{"geo-id", "from", "to"},
		},
		{
			Title: "Permit Filters",
			Names: []string{
				"tags", "query", "status",
				"min-approval-duration", "min-construction-duration",
				"min-inspection-pr", "min-job-value", "min-fees",
			},
		},
		{
			Title: "Property Filters",
			Names: []string{
				"property-type", "property-min-market-value",
				"property-min-building-area", "property-min-lot-size",
				"property-min-story-count", "property-min-unit-count",
			},
		},
		{
			Title: "Contractor Filters",
			Names: []string{
				"contractor-classification", "contractor-name",
				"contractor-website", "contractor-min-total-job-value",
				"contractor-min-total-permits-count",
				"contractor-min-inspection-pr", "contractor-license",
			},
		},
		{
			Title: "Response Options",
			Names: []string{"no-tallies"},
		},
	})

	contractorsCmd.AddCommand(contractorsSearchCmd)
	rootCmd.AddCommand(contractorsCmd)
}

package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// validPermitStatuses lists the values the API accepts for permit_status.
var validPermitStatuses = []string{"final", "in_review", "inactive", "active"}

// datePattern matches YYYY-MM-DD format.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// badGeoIDPattern matches common wrong geo_id formats like ZIP_90210,
// CITY_LOS_ANGELES_CA, COUNTY_*, STATE_CA, etc.
var badGeoIDPattern = regexp.MustCompile(`^(?i)(ZIP|CITY|COUNTY|STATE|ADDR)_`)

// validStatePattern matches 2-letter US state codes.
var validStatePattern = regexp.MustCompile(`^[A-Z]{2}$`)

// validZipPattern matches 5-digit US zip codes.
var validZipPattern = regexp.MustCompile(`^\d{5}$`)

// registerSearchFlags adds the common search flags shared by permits search
// and contractors search onto the given command's flag set.
func registerSearchFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// Required filters
	f.String("geo-id", "", `Geographic area ID (required). Formats:
  Zip code:  5-digit code directly (92024, 78701, 33139)
  State:     2-letter code directly (CA, TX, FL)
  Address/city/county: opaque Shovels ID, resolve first:
    shovels addresses search -q "Austin, TX" | jq '.data[0].geo_id'`)
	f.String("permit-from", "", "Permit start date in YYYY-MM-DD format (required)")
	f.String("permit-to", "", "Permit end date in YYYY-MM-DD format (required)")

	// Permit filters
	f.StringSlice("tags", nil, "Permit tags, AND logic, prefix with - to exclude (e.g. solar, -roofing)")
	f.String("query", "", "Substring search in permit description, case-insensitive, max 50 chars")
	f.StringSlice("status", nil, "Permit status: final, in_review, inactive, active")
	f.Int("min-approval-duration", 0, "Minimum approval duration in days (integer)")
	f.Int("min-construction-duration", 0, "Minimum construction duration in days (integer)")
	f.Int("min-inspection-pr", 0, "Minimum inspection pass rate, 0-100 (integer)")
	f.Int("min-job-value", 0, "Minimum job value in dollars (integer)")
	f.Int("min-fees", 0, "Minimum permit fees in dollars (integer)")

	// Property filters
	f.String("property-type", "", "Property type: residential, commercial, industrial")
	f.Int("property-min-market-value", 0, "Minimum assessed market value in dollars (integer)")
	f.Int("property-min-building-area", 0, "Minimum building area in square feet (integer)")
	f.Int("property-min-lot-size", 0, "Minimum lot size in square feet (integer)")
	f.Int("property-min-story-count", 0, "Minimum number of stories (integer)")
	f.Int("property-min-unit-count", 0, "Minimum number of units (integer)")

	// Contractor filters
	f.StringSlice("contractor-classification", nil, "Contractor classification, AND logic, prefix with - to exclude (e.g. general_building)")
	f.String("contractor-name", "", "Filter by contractor name or partial name (string)")
	f.String("contractor-website", "", "Filter by contractor website domain, omit http/https (string)")
	f.Int("contractor-min-total-job-value", 0, "Minimum lifetime contractor job value in dollars (integer)")
	f.Int("contractor-min-total-permits-count", 0, "Minimum lifetime permits count (integer)")
	f.Int("contractor-min-inspection-pr", 0, "Minimum lifetime inspection pass rate, 0-100 (integer)")
	f.String("contractor-license", "", "Filter by contractor license number (string)")

	// Response options
	f.Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")
}

// searchFlagGroups returns the standard flag groups shared by permits search
// and contractors search. Callers can append command-specific groups.
func searchFlagGroups() []flagGroup {
	return []flagGroup{
		{
			Title: "Required Flags",
			Names: []string{"geo-id", "permit-from", "permit-to"},
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
			Names: []string{"include-count"},
		},
	}
}

// validateSearchFlags validates the required flags (geo-id, from, to), date
// formats, query length, and status values. Returns a non-nil error (already
// printed to stderr) if validation fails.
func validateSearchFlags(cmd *cobra.Command) error {
	geoID, _ := cmd.Flags().GetString("geo-id")
	from, _ := cmd.Flags().GetString("permit-from")
	to, _ := cmd.Flags().GetString("permit-to")

	var missing []string
	if geoID == "" {
		missing = append(missing, "--geo-id")
	}
	if from == "" {
		missing = append(missing, "--permit-from")
	}
	if to == "" {
		missing = append(missing, "--permit-to")
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if badGeoIDPattern.MatchString(geoID) {
		msg := fmt.Sprintf(
			"invalid --geo-id %q. Do not use prefixes like ZIP_, CITY_, COUNTY_, or STATE_. "+
				"Use the zip code directly (e.g. 90210), the state code (e.g. CA), "+
				"or resolve a city/address: shovels addresses search -q \"...\" | jq '.data[0].geo_id'",
			geoID,
		)
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if !datePattern.MatchString(from) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --permit-from: %q (expected YYYY-MM-DD)", from), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if !datePattern.MatchString(to) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --permit-to: %q (expected YYYY-MM-DD)", to), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	query, _ := cmd.Flags().GetString("query")
	if len(query) > 50 {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("--query must be at most 50 characters, got %d", len(query)), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	statuses, _ := cmd.Flags().GetStringSlice("status")
	for _, s := range statuses {
		if !isValidStatus(s) {
			msg := fmt.Sprintf("invalid --status value %q: valid options are %s", s, strings.Join(validPermitStatuses, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}

	return nil
}

// buildSearchQuery reads the common search flags from cmd and builds
// url.Values containing geo, date, permit, property, and contractor
// filter parameters.
func buildSearchQuery(cmd *cobra.Command) url.Values {
	geoID, _ := cmd.Flags().GetString("geo-id")
	from, _ := cmd.Flags().GetString("permit-from")
	to, _ := cmd.Flags().GetString("permit-to")

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
	query, _ := cmd.Flags().GetString("query")
	if query != "" {
		q.Set("permit_q", query)
	}
	statuses, _ := cmd.Flags().GetStringSlice("status")
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

	return q
}

// parseLimitConfig parses --limit and --max-records flags and returns a
// configured LimitConfig. Returns a non-nil error (already printed to
// stderr) if parsing or validation fails.
func parseLimitConfig(cmd *cobra.Command) (client.LimitConfig, error) {
	limitStr, _ := cmd.Flags().GetString("limit")
	lc, err := client.ParseLimit(limitStr)
	if err != nil {
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return lc, &exitError{code: 1}
	}
	maxRecords, _ := cmd.Flags().GetInt("max-records")
	if err := client.ValidateMaxRecords(maxRecords); err != nil {
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return lc, &exitError{code: 1}
	}
	if lc.All {
		lc = lc.WithMaxRecords(maxRecords)
	}
	return lc, nil
}

// newClientFromFlags creates a client.Client from the resolved config and
// the --timeout / --no-retry flags. Returns a non-nil error (already
// printed to stderr) if timeout parsing fails.
func newClientFromFlags(cmd *cobra.Command) (*client.Client, error) {
	cfg := ResolvedConfig()

	timeoutStr, _ := cmd.Flags().GetString("timeout")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		output.PrintErrorTyped(os.Stderr, "invalid timeout: "+timeoutStr, 1, client.ErrorTypeValidation)
		return nil, &exitError{code: 1}
	}

	noRetry, _ := cmd.Flags().GetBool("no-retry")

	cl := client.New(client.Options{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Timeout: timeout,
		NoRetry: noRetry,
		Version: buildVersion,
	})
	return cl, nil
}

// runPaginatedSearch validates common search flags, builds query params,
// creates a client, paginates the given endpoint, and prints the result.
// Callers can modify the query via the optional queryFn callback before
// the request is made.
func runPaginatedSearch(cmd *cobra.Command, endpoint string, queryFn func(url.Values)) error {
	if err := validateSearchFlags(cmd); err != nil {
		return err
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	q := buildSearchQuery(cmd)
	setBoolFlag(cmd, "include-count", "include_count", q)
	if queryFn != nil {
		queryFn(q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

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

// isValidStatus checks whether s is one of the API-accepted permit statuses.
func isValidStatus(s string) bool {
	for _, valid := range validPermitStatuses {
		if s == valid {
			return true
		}
	}
	return false
}

// setStringFlag adds a query parameter only when the flag was explicitly set.
func setStringFlag(cmd *cobra.Command, flag, param string, q url.Values) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		q.Set(param, v)
	}
}

// setIntFlag adds a query parameter (as a string) only when the flag was
// explicitly set.
func setIntFlag(cmd *cobra.Command, flag, param string, q url.Values) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt(flag)
		q.Set(param, fmt.Sprintf("%d", v))
	}
}

// setBoolFlag adds a query parameter only when the flag was explicitly set.
func setBoolFlag(cmd *cobra.Command, flag, param string, q url.Values) {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		q.Set(param, fmt.Sprintf("%t", v))
	}
}

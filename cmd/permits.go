package cmd

import (
	"context"
	"encoding/json"
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

// maxPermitGetIDs is the maximum number of permit IDs accepted per request.
const maxPermitGetIDs = 50

// validPermitStatuses lists the values the API accepts for permit_status.
var validPermitStatuses = []string{"final", "in_review", "inactive", "active"}

// datePattern matches YYYY-MM-DD format.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var permitsCmd = &cobra.Command{
	Use:   "permits",
	Short: "Search and retrieve building permits",
	Long: `Query the Shovels permits database. Subcommands:

  search   Find permits by location, date range, tags, and dozens of filters
  get      Retrieve one or more permits by ID

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var permitsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search building permits by location, date, and filters",
	Long: `Search the Shovels permits database with required location and date filters
plus optional permit, property, and contractor filters.

Required flags:
  --geo-id    Geographic filter (e.g. ZIP_90210, CITY_LOS_ANGELES_CA)
  --from      Start date in YYYY-MM-DD format
  --to        End date in YYYY-MM-DD format

Example:
  shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar --limit 10

Multiple tags use AND logic:
  shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar --tags roofing

Exclude a tag with a dash prefix:
  shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar --tags=-roofing`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPermitsSearch,
}

func runPermitsSearch(cmd *cobra.Command, args []string) error {
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

	// Permit filters: tags and statuses use repeated query params for arrays.
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

	setBoolFlag(cmd, "has-contractor", "permit_has_contractor", q)

	// Property filters
	setStringFlag(cmd, "property-type", "property_type", q)
	setIntFlag(cmd, "property-min-market-value", "property_min_market_value", q)
	setIntFlag(cmd, "property-min-building-area", "property_min_building_area", q)
	setIntFlag(cmd, "property-min-lot-size", "property_min_lot_size", q)
	setIntFlag(cmd, "property-min-story-count", "property_min_story_count", q)
	setIntFlag(cmd, "property-min-unit-count", "property_min_unit_count", q)

	// Contractor filters: classifications use repeated query params for arrays.
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

	result, err := cl.Paginate(context.Background(), "/permits/search", q, lc)
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

var permitsGetCmd = &cobra.Command{
	Use:   "get ID [ID...]",
	Short: "Retrieve one or more permits by ID",
	Long: `Fetch specific permits by their IDs. Pass one or more permit IDs as
positional arguments (maximum 50 per request).

Example (single permit):
  shovels permits get P123

Example (multiple permits):
  shovels permits get P123 P456 P789

Response envelope: {"data": [...], "meta": {"count": N, ...}}
When some IDs are not found, meta.missing lists the missing IDs.`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPermitsGet,
}

func runPermitsGet(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		output.PrintErrorTyped(os.Stderr, "at least one permit ID required", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if len(args) > maxPermitGetIDs {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("maximum %d IDs per request", maxPermitGetIDs), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

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

	q := url.Values{}
	for _, id := range args {
		q.Add("id", id)
	}

	resp, err := cl.Get(context.Background(), "/permits", q)
	if err != nil {
		apiErr, ok := err.(*client.APIError)
		if ok {
			output.PrintErrorTyped(os.Stderr, apiErr.Message, apiErr.ExitCode, apiErr.ErrorType)
			return &exitError{code: apiErr.ExitCode}
		}
		output.PrintErrorTyped(os.Stderr, err.Error(), 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		output.PrintErrorTyped(os.Stderr, "failed to parse API response", 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	// Detect missing IDs by comparing requested IDs against returned IDs.
	missing := findMissingIDs(args, page.Items)

	output.PrintBatch(cmd.OutOrStdout(), page.Items, missing, resp.Credits)
	return nil
}

// findMissingIDs returns the subset of requested IDs not present in the
// returned items. Each item is expected to have an "id" field at the top level.
func findMissingIDs(requested []string, items []json.RawMessage) []string {
	found := make(map[string]bool, len(items))
	for _, item := range items {
		var obj struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(item, &obj) == nil && obj.ID != "" {
			found[obj.ID] = true
		}
	}

	var missing []string
	for _, id := range requested {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func init() {
	f := permitsSearchCmd.Flags()

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
	f.Bool("has-contractor", false, "Return only permits that have a contractor ID")

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

	setGroupedUsage(permitsSearchCmd, []flagGroup{
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
				"has-contractor",
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
	})

	permitsCmd.AddCommand(permitsSearchCmd)
	permitsCmd.AddCommand(permitsGetCmd)
	rootCmd.AddCommand(permitsCmd)
}

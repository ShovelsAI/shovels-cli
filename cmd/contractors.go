package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// maxContractorGetIDs is the maximum number of contractor IDs accepted per request.
const maxContractorGetIDs = 50

var contractorsCmd = &cobra.Command{
	Use:   "contractors",
	Short: "Search contractors and retrieve their permits, employees, and metrics",
	Long: `Query the Shovels contractor database.

Available subcommands:
  search     Search contractors by geographic area, date range, permit tags, and performance metrics
  get        Retrieve one or more contractors by their exact contractor ID
  permits    List building permits filed by a specific contractor
  employees  List employees associated with a specific contractor
  metrics    Get monthly performance metrics for a specific contractor

Every response is a JSON envelope: {"data": ..., "meta": {...}}`,
}

var contractorsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search contractors by location, date range, permit tags, and performance metrics",
	Long: `Search the Shovels contractor database. Requires a geographic area and date
range. Supports permit tag filters, property type filters, contractor
classification filters, and minimum-value thresholds.

Required flags:
  --geo-id       Geographic area: zip code (92024), state (CA), or Shovels ID from addresses search
  --permit-from  Start date in YYYY-MM-DD format
  --permit-to    End date in YYYY-MM-DD format

Geographic IDs:
  Zip codes and states work directly (92024, CA). For cities, counties, or
  addresses, resolve the ID first:
    shovels addresses search -q "Austin, TX" | jq '.data[0].geo_id'

Examples:
  Search contractors in a zip code:
    shovels contractors search --geo-id 78701 --permit-from 2024-01-01 --permit-to 2024-12-31

  Filter by contractor classification:
    shovels contractors search --geo-id 92024 --permit-from 2024-01-01 --permit-to 2024-12-31 --contractor-classification general_building

  Skip tallies for faster response:
    shovels contractors search --geo-id CA --permit-from 2024-01-01 --permit-to 2024-12-31 --no-tallies`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsSearch,
}

func runContractorsSearch(cmd *cobra.Command, args []string) error {
	return runPaginatedSearch(cmd, "/contractors/search", func(q url.Values) {
		noTallies, _ := cmd.Flags().GetBool("no-tallies")
		if noTallies {
			q.Set("include_tallies", "false")
		}
	})
}

var contractorsGetCmd = &cobra.Command{
	Use:   "get ID [ID...]",
	Short: "Retrieve one or more contractors by their exact contractor ID",
	Long: `Fetch specific contractors by ID. Accepts 1 to 50 contractor IDs as
positional arguments.

Single ID returns the contractor object directly in data.
Multiple IDs return an array in data, with meta.missing listing unfound IDs.

Examples:
  Single contractor:
    shovels contractors get C123

  Multiple contractors in one request:
    shovels contractors get C123 C456 C789

Single ID response: {"data": {<contractor>}, "meta": {"credits_used": N, ...}}
Batch response:     {"data": [{...}, ...], "meta": {"count": N, "missing": [...], ...}}`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsGet,
}

func runContractorsGet(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		output.PrintErrorTyped(os.Stderr, "at least one contractor ID required", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if len(args) > maxContractorGetIDs {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("maximum %d IDs per request", maxContractorGetIDs), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	q := url.Values{}
	for _, id := range args {
		q.Add("id", id)
	}

	resp, err := cl.Get(cmd.Context(), "/contractors", q)
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

	// Single ID: unwrap to object. Multiple IDs: return array with missing tracking.
	if len(args) == 1 {
		if len(page.Items) == 0 {
			output.PrintBatch(cmd.OutOrStdout(), page.Items, args, resp.Credits)
			return nil
		}
		var obj any
		if err := json.Unmarshal(page.Items[0], &obj); err != nil {
			output.PrintErrorTyped(os.Stderr, "failed to parse API response", 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}
		output.PrintSingle(cmd.OutOrStdout(), obj, resp.Credits)
		return nil
	}

	missing := findMissingIDs(args, page.Items)
	output.PrintBatch(cmd.OutOrStdout(), page.Items, missing, resp.Credits)
	return nil
}

var contractorsPermitsCmd = &cobra.Command{
	Use:   "permits ID",
	Short: "List building permits filed by a specific contractor",
	Long: `Retrieve building permits associated with a contractor. Accepts exactly one
contractor ID as a positional argument. Results are paginated; use --limit to
control how many records are returned.

Examples:
  List permits for a contractor:
    shovels contractors permits ABC123

  Fetch up to 100 permits:
    shovels contractors permits ABC123 --limit 100

  Fetch all permits (up to --max-records cap):
    shovels contractors permits ABC123 --limit all

Response: {"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, ...}}`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsPermits,
}

func runContractorsPermits(cmd *cobra.Command, args []string) error {
	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	var q url.Values
	if cmd.Flags().Changed("include-count") {
		q = url.Values{}
		setBoolFlag(cmd, "include-count", "include_count", q)
	}

	endpoint := fmt.Sprintf("/contractors/%s/permits", args[0])
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

var contractorsEmployeesCmd = &cobra.Command{
	Use:   "employees ID",
	Short: "List employees associated with a specific contractor",
	Long: `Retrieve employees associated with a contractor. Accepts exactly one
contractor ID as a positional argument. Results are paginated; use --limit to
control how many records are returned.

Examples:
  List employees for a contractor:
    shovels contractors employees ABC123

  Fetch all employees (up to --max-records cap):
    shovels contractors employees ABC123 --limit all

Response: {"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, ...}}`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsEmployees,
}

func runContractorsEmployees(cmd *cobra.Command, args []string) error {
	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("/contractors/%s/employees", args[0])
	result, err := cl.Paginate(context.Background(), endpoint, nil, lc)
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

var contractorsMetricsCmd = &cobra.Command{
	Use:   "metrics ID",
	Short: "Get monthly performance metrics for a specific contractor",
	Long: `Retrieve monthly performance metrics for a specific contractor. Accepts exactly
one contractor ID as a positional argument. All four flags are required.

Required flags:
  --metric-from YYYY-MM-DD     Metrics start date, inclusive (required)
  --metric-to YYYY-MM-DD       Metrics end date, inclusive (required)
  --property-type TYPE          Property type: residential, commercial, industrial (required)
  --tag TAG                     Permit tag: solar, roofing, electrical, etc. (required)

Example:
  shovels contractors metrics ABC123 \
    --metric-from 2024-01-01 --metric-to 2024-12-31 \
    --property-type residential --tag solar

Response: {"data": [...], "meta": {"credits_used": N, "credits_remaining": N}}
Metrics are not paginated. The response contains monthly aggregate data.`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runContractorsMetrics,
}

func runContractorsMetrics(cmd *cobra.Command, args []string) error {
	metricFrom, _ := cmd.Flags().GetString("metric-from")
	metricTo, _ := cmd.Flags().GetString("metric-to")
	propertyType, _ := cmd.Flags().GetString("property-type")
	tag, _ := cmd.Flags().GetString("tag")

	var missing []string
	if metricFrom == "" {
		missing = append(missing, "--metric-from")
	}
	if metricTo == "" {
		missing = append(missing, "--metric-to")
	}
	if propertyType == "" {
		missing = append(missing, "--property-type")
	}
	if tag == "" {
		missing = append(missing, "--tag")
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

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	q := url.Values{
		"metric_from":   {metricFrom},
		"metric_to":     {metricTo},
		"property_type": {propertyType},
		"tag":           {tag},
	}

	endpoint := fmt.Sprintf("/contractors/%s/metrics", args[0])
	resp, err := cl.Get(context.Background(), endpoint, q)
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

	output.PrintSingle(cmd.OutOrStdout(), page.Items, resp.Credits)
	return nil
}

func init() {
	registerSearchFlags(contractorsSearchCmd)

	// Contractors-specific flag
	contractorsSearchCmd.Flags().Bool("no-tallies", false, "Omit tag and status tallies for faster response (sends include_tallies=false)")

	groups := searchFlagGroups()
	// Append no-tallies to the existing Response Options group.
	for i := range groups {
		if groups[i].Title == "Response Options" {
			groups[i].Names = append(groups[i].Names, "no-tallies")
			break
		}
	}
	setGroupedUsage(contractorsSearchCmd, groups)

	// Contractor permits flag
	contractorsPermitsCmd.Flags().Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")

	// Metrics flags
	contractorsMetricsCmd.Flags().String("metric-from", "", "Metrics start date in YYYY-MM-DD format (required)")
	contractorsMetricsCmd.Flags().String("metric-to", "", "Metrics end date in YYYY-MM-DD format (required)")
	contractorsMetricsCmd.Flags().String("property-type", "", "Property type: residential, commercial, industrial (required)")
	contractorsMetricsCmd.Flags().String("tag", "", "Permit tag: solar, roofing, electrical, plumbing, etc. (required)")

	contractorsCmd.AddCommand(contractorsSearchCmd)
	contractorsCmd.AddCommand(contractorsGetCmd)
	contractorsCmd.AddCommand(contractorsPermitsCmd)
	contractorsCmd.AddCommand(contractorsEmployeesCmd)
	contractorsCmd.AddCommand(contractorsMetricsCmd)
	rootCmd.AddCommand(contractorsCmd)
}

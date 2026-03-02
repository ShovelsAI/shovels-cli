package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// maxPermitGetIDs is the maximum number of permit IDs accepted per request.
const maxPermitGetIDs = 50

var permitsCmd = &cobra.Command{
	Use:   "permits",
	Short: "Search and retrieve building permits by location, date, type, and contractor",
	Long: `Query the Shovels building permits database.

Available subcommands:
  search   Search permits by geographic area, date range, permit tags, property type, and contractor
  get      Retrieve one or more permits by their exact permit ID

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var permitsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search building permits by location, date range, permit type, and contractor",
	Long: `Search the Shovels building permits database. Requires a geographic area and
date range. Supports permit tag filters, property type filters, contractor
filters, and minimum-value thresholds.

Required flags:
  --geo-id GEO_ID   Geographic area: ZIP_90210, CITY_LOS_ANGELES_CA, COUNTY_LOS_ANGELES_CA, or STATE_CA (required)
  --permit-from DATE        Start date in YYYY-MM-DD format (required)
  --permit-to DATE          End date in YYYY-MM-DD format (required)

Examples:
  Search solar permits in a zip code:
    shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --limit 10

  Multiple tags (AND logic):
    shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags roofing

  Exclude a tag with dash prefix:
    shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags=-roofing

  Filter by property type and minimum job value:
    shovels permits search --geo-id STATE_CA --permit-from 2024-01-01 --permit-to 2024-12-31 --property-type residential --min-job-value 50000`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPermitsSearch,
}

func runPermitsSearch(cmd *cobra.Command, args []string) error {
	return runPaginatedSearch(cmd, "/permits/search", func(q url.Values) {
		setBoolFlag(cmd, "has-contractor", "permit_has_contractor", q)
	})
}

var permitsGetCmd = &cobra.Command{
	Use:   "get ID [ID...]",
	Short: "Retrieve one or more permits by their exact permit ID",
	Long: `Fetch specific building permits by ID. Accepts 1 to 50 permit IDs as
positional arguments.

Examples:
  Single permit:
    shovels permits get P123

  Multiple permits in one request:
    shovels permits get P123 P456 P789

Response: {"data": [...], "meta": {"count": N, "missing": ["UNKNOWN_ID"], ...}}
IDs not found in the database appear in meta.missing.`,
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

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	q := url.Values{}
	for _, id := range args {
		q.Add("id", id)
	}

	resp, err := cl.Get(cmd.Context(), "/permits", q)
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
	registerSearchFlags(permitsSearchCmd)

	// Permits-specific flag
	permitsSearchCmd.Flags().Bool("has-contractor", false, "Include only permits linked to a known contractor")

	groups := searchFlagGroups()
	// Append has-contractor to the Permit Filters group.
	groups[1].Names = append(groups[1].Names, "has-contractor")
	setGroupedUsage(permitsSearchCmd, groups)

	permitsCmd.AddCommand(permitsSearchCmd)
	permitsCmd.AddCommand(permitsGetCmd)
	rootCmd.AddCommand(permitsCmd)
}

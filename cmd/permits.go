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
	return runPaginatedSearch(cmd, "/permits/search", func(q url.Values) {
		setBoolFlag(cmd, "has-contractor", "permit_has_contractor", q)
	})
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
	permitsSearchCmd.Flags().Bool("has-contractor", false, "Return only permits that have a contractor ID")

	groups := searchFlagGroups()
	// Append has-contractor to the Permit Filters group.
	groups[1].Names = append(groups[1].Names, "has-contractor")
	setGroupedUsage(permitsSearchCmd, groups)

	permitsCmd.AddCommand(permitsSearchCmd)
	permitsCmd.AddCommand(permitsGetCmd)
	rootCmd.AddCommand(permitsCmd)
}

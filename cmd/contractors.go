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

// maxContractorGetIDs is the maximum number of contractor IDs accepted per request.
const maxContractorGetIDs = 50

var contractorsCmd = &cobra.Command{
	Use:   "contractors",
	Short: "Search and retrieve contractors",
	Long: `Query the Shovels contractor database. Subcommands:

  search   Find contractors by location, date range, tags, and performance metrics
  get      Retrieve one or more contractors by ID

Every response is a JSON envelope: {"data": ..., "meta": {...}}`,
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
	return runPaginatedSearch(cmd, "/contractors/search", func(q url.Values) {
		noTallies, _ := cmd.Flags().GetBool("no-tallies")
		if noTallies {
			q.Set("include_tallies", "false")
		}
	})
}

var contractorsGetCmd = &cobra.Command{
	Use:   "get ID [ID...]",
	Short: "Retrieve one or more contractors by ID",
	Long: `Fetch specific contractors by their IDs. Pass one or more contractor IDs as
positional arguments (maximum 50 per request).

Single ID returns the contractor object directly in data.
Multiple IDs return an array in data, with meta.missing listing any unfound IDs.

Example (single contractor):
  shovels contractors get C123

Example (multiple contractors):
  shovels contractors get C123 C456 C789

Single ID response: {"data": {<contractor>}, "meta": {"credits_used": N, ...}}
Batch response:     {"data": [{...}, ...], "meta": {"count": N, ...}}`,
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

func init() {
	registerSearchFlags(contractorsSearchCmd)

	// Contractors-specific flag
	contractorsSearchCmd.Flags().Bool("no-tallies", false, "Omit tag and status tallies from response for faster results (sends include_tallies=false)")

	groups := searchFlagGroups()
	groups = append(groups, flagGroup{
		Title: "Response Options",
		Names: []string{"no-tallies"},
	})
	setGroupedUsage(contractorsSearchCmd, groups)

	contractorsCmd.AddCommand(contractorsSearchCmd)
	contractorsCmd.AddCommand(contractorsGetCmd)
	rootCmd.AddCommand(contractorsCmd)
}

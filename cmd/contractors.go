package cmd

import (
	"net/url"

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
	return runPaginatedSearch(cmd, "/contractors/search", func(q url.Values) {
		noTallies, _ := cmd.Flags().GetBool("no-tallies")
		if noTallies {
			q.Set("include_tallies", "false")
		}
	})
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
	rootCmd.AddCommand(contractorsCmd)
}

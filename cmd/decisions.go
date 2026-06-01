package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// maxDecisionQueryRunes is the maximum length of --query for decisions,
// measured in runes to match the API's character-based max_length.
const maxDecisionQueryRunes = 100

// zipPlusFourPattern matches ZIP+4 codes (e.g. 92024-1234).
var zipPlusFourPattern = regexp.MustCompile(`^\d{5}-\d{4}$`)

var decisionsCmd = &cobra.Command{
	Use:   "decisions",
	Short: "Search and retrieve zoning and land-use decisions by location, date, and category",
	Long: `Query the Shovels zoning and land-use decisions database.

Available subcommands:
  search   Search decisions by geographic area, date range, category, and project value

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var decisionsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search zoning and land-use decisions by location, date range, and category",
	Long: `Search the Shovels zoning and land-use decisions database. Requires a
geographic area and date range.

Required flags:
  --geo-id         Geographic area: state code (CA) or a resolved Shovels ID
  --decision-from  Start date in YYYY-MM-DD format
  --decision-to    End date in YYYY-MM-DD format

Geographic IDs:
  ZIP codes are NOT supported for decisions. Use a 2-letter state code
  directly (CA, TX, FL), or resolve a city, county, jurisdiction, or address
  to its Shovels geo_id first:
    shovels cities search -q "Miami" | jq '.data[0].geo_id'
    shovels counties search -q "Los Angeles" | jq '.data[0].geo_id'
    shovels jurisdictions search -q "Portland" | jq '.data[0].geo_id'
    shovels addresses search -q "123 Main St" | jq '.data[0].geo_id'

Examples:
  Rezoning decisions in California in 2024:
    shovels decisions search --geo-id CA --decision-from 2024-01-01 --decision-to 2024-12-31 --category Rezoning

  Filter by asset class and project value (values in cents, 100000000 = $1,000,000):
    shovels decisions search --geo-id CA --decision-from 2024-01-01 --decision-to 2024-12-31 --asset-class Residential --min-project-value 100000000

  Multiple categories (repeat the flag or comma-separate):
    shovels decisions search --geo-id CA --decision-from 2024-01-01 --decision-to 2024-12-31 --category Rezoning --category Variance
    shovels decisions search --geo-id CA --decision-from 2024-01-01 --decision-to 2024-12-31 --category Rezoning,Variance

  Substring search in decision text:
    shovels decisions search --geo-id CA --decision-from 2024-01-01 --decision-to 2024-12-31 --query "downtown"`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runDecisionsSearch,
}

func runDecisionsSearch(cmd *cobra.Command, args []string) error {
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		return err
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	q := buildDecisionsSearchQuery(cmd)

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, "/decisions/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/decisions/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

// registerDecisionsSearchFlags adds the decisions search flags onto cmd.
// Decisions has its own flag set because it rejects ZIP geo_ids and emits
// decision_* parameters, diverging from the permits-shaped search helpers.
func registerDecisionsSearchFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// Required filters
	f.String("geo-id", "", `Geographic area ID (required). ZIP codes are NOT supported. Formats:
  State:        2-letter code directly (CA, TX, FL)
  City:         shovels cities search -q "Miami" | jq '.data[0].geo_id'
  County:       shovels counties search -q "Los Angeles" | jq '.data[0].geo_id'
  Jurisdiction: shovels jurisdictions search -q "Portland" | jq '.data[0].geo_id'
  Address:      shovels addresses search -q "123 Main St" | jq '.data[0].geo_id'`)
	f.String("decision-from", "", "Decision start date in YYYY-MM-DD format (required)")
	f.String("decision-to", "", "Decision end date in YYYY-MM-DD format (required)")

	// Decision filters
	f.StringSlice("asset-class", nil, "Asset class, repeat or comma-separate for multiple (e.g. Residential, Commercial)")
	f.StringSlice("category", nil, "Decision category, repeat or comma-separate for multiple (e.g. Rezoning, Variance)")
	f.StringSlice("subcategory", nil, "Decision subcategory, repeat or comma-separate for multiple")
	f.StringSlice("property-type", nil, "Property type, repeat or comma-separate for multiple")
	f.Int("min-project-value", 0, "Minimum project value in cents (100000000 = $1,000,000)")
	f.Int("max-project-value", 0, "Maximum project value in cents (100000000 = $1,000,000)")
	f.StringP("query", "q", "", "Substring search in decision text, case-insensitive, max 100 characters")

	// Response options
	f.Bool("include-count", false, "Request total result count (capped at 10,000). Returned as total_count in meta on first page")
}

// decisionsSearchFlagGroups returns the grouped help layout for decisions search.
func decisionsSearchFlagGroups() []flagGroup {
	return []flagGroup{
		{
			Title: "Required Flags",
			Names: []string{"geo-id", "decision-from", "decision-to"},
		},
		{
			Title: "Decision Filters",
			Names: []string{
				"asset-class", "category", "subcategory", "property-type",
				"min-project-value", "max-project-value", "query",
			},
		},
		{
			Title: "Response Options",
			Names: []string{"include-count"},
		},
	}
}

// validateDecisionsSearchFlags validates required flags, ZIP rejection, date
// formats, query rune length, and non-negative project values. Returns a
// non-nil error (already printed to stderr) when validation fails.
func validateDecisionsSearchFlags(cmd *cobra.Command) error {
	geoID, _ := cmd.Flags().GetString("geo-id")
	from, _ := cmd.Flags().GetString("decision-from")
	to, _ := cmd.Flags().GetString("decision-to")

	var missing []string
	if geoID == "" {
		missing = append(missing, "--geo-id")
	}
	if from == "" {
		missing = append(missing, "--decision-from")
	}
	if to == "" {
		missing = append(missing, "--decision-to")
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if isZipGeoID(geoID) {
		msg := fmt.Sprintf(
			"invalid --geo-id %q: ZIP geo_ids are not supported for decisions. "+
				"Use a 2-letter state code (e.g. CA) or resolve a city/county/jurisdiction/address via: "+
				"shovels cities search -q \"...\", shovels counties search -q \"...\", "+
				"shovels jurisdictions search -q \"...\", or shovels addresses search -q \"...\"",
			geoID,
		)
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if !datePattern.MatchString(from) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --decision-from: %q (expected YYYY-MM-DD)", from), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if !datePattern.MatchString(to) {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --decision-to: %q (expected YYYY-MM-DD)", to), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	query, _ := cmd.Flags().GetString("query")
	if n := utf8.RuneCountInString(query); n > maxDecisionQueryRunes {
		output.PrintErrorTyped(os.Stderr, fmt.Sprintf("--query must be at most %d characters, got %d", maxDecisionQueryRunes, n), 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if cmd.Flags().Changed("min-project-value") {
		v, _ := cmd.Flags().GetInt("min-project-value")
		if v < 0 {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("--min-project-value must be at least 0, got %d", v), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}
	if cmd.Flags().Changed("max-project-value") {
		v, _ := cmd.Flags().GetInt("max-project-value")
		if v < 0 {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("--max-project-value must be at least 0, got %d", v), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}

	return nil
}

// buildDecisionsSearchQuery reads the decisions search flags and builds the
// url.Values for GET /decisions/search.
func buildDecisionsSearchQuery(cmd *cobra.Command) url.Values {
	geoID, _ := cmd.Flags().GetString("geo-id")
	from, _ := cmd.Flags().GetString("decision-from")
	to, _ := cmd.Flags().GetString("decision-to")

	q := url.Values{
		"geo_id":        {geoID},
		"decision_from": {from},
		"decision_to":   {to},
	}

	addStringSliceParam(cmd, "asset-class", "asset_class", q)
	addStringSliceParam(cmd, "category", "category", q)
	addStringSliceParam(cmd, "subcategory", "subcategory", q)
	addStringSliceParam(cmd, "property-type", "property_type", q)

	setIntFlag(cmd, "min-project-value", "min_project_value", q)
	setIntFlag(cmd, "max-project-value", "max_project_value", q)

	query, _ := cmd.Flags().GetString("query")
	if query != "" {
		q.Set("decision_q", query)
	}

	setBoolFlag(cmd, "include-count", "include_count", q)

	return q
}

// isZipGeoID reports whether geoID is a bare 5-digit ZIP, a ZIP+4, or a
// prefixed geo_id (ZIP_/CITY_/COUNTY_/STATE_/ADDR_). Decisions rejects all of
// these client-side; opaque IDs and 2-letter state codes pass through.
func isZipGeoID(geoID string) bool {
	if validZipPattern.MatchString(geoID) {
		return true
	}
	if zipPlusFourPattern.MatchString(geoID) {
		return true
	}
	return badGeoIDPattern.MatchString(geoID)
}

func init() {
	registerDecisionsSearchFlags(decisionsSearchCmd)
	setGroupedUsage(decisionsSearchCmd, decisionsSearchFlagGroups())

	decisionsCmd.AddCommand(decisionsSearchCmd)
	rootCmd.AddCommand(decisionsCmd)
}

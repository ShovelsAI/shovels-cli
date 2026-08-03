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

// maxPropertyLegalOwners is the maximum number of --legal-owner values the
// API accepts on one properties search.
const maxPropertyLegalOwners = 10

var propertiesCmd = &cobra.Command{
	Use:   "properties",
	Short: "Search properties with their permit history rolled up onto the property record (Beta)",
	Long: `Query the Shovels properties database. One row is one property, with its
permit history rolled up onto the record, so a single row answers "what has
happened at this address".

Beta: query parameters, response fields, and the absence-trust surface may
still change. Treat the response shape as unstable.

Available subcommands:
  search   Search properties by geographic scope and/or legal owner

Every response is a JSON envelope: {"data": [...], "meta": {...}}`,
}

var propertiesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search properties by geographic scope and/or legal owner, filtered by permit history",
	Long: `Search properties by geographic scope and/or legal owner, filtered by the
permits attached to each property. At least one of --geo-id or --legal-owner
is required.

Beta: query parameters, response fields, and the absence-trust surface may
still change. Treat the response shape as unstable.

Geographic IDs (--geo-id):
  Zip code and Zip+4 work directly (92024, 92024-1234)
  State codes work directly (CA, TX, FL)
  Resolve anything else to a Shovels geo_id first:
    shovels cities search -q "Encinitas" | jq '.data[0].geo_id'
    shovels counties search -q "San Diego" | jq '.data[0].geo_id'
    shovels addresses search -q "123 Main St" | jq '.data[0].geo_id'
  Jurisdiction geo_ids are rejected by this endpoint. Scope a jurisdiction
  query by city, county, state, or address instead.

Absence searches (properties WITHOUT a permit type):
  Prefix a tag with - to select properties that have no such permit:
    shovels properties search --geo-id 92024 --permit-tags "-solar"
  Every absence row carries a trust object stating how far to believe the
  absence (jurisdiction coverage tier, unresolved rate, data horizon), and
  each API page's row-weighted summary is collected into
  meta.trust_summaries. Presence-only searches carry neither.

Date windows:
  There is no --permit-to flag. This endpoint filters from a single date
  (--permit-from) only. For a closed date window, use: shovels permits search

Examples:
  Properties in Encinitas with no solar permit:
    shovels properties search --geo-id 92024 --permit-tags "-solar" --limit 10

  Owner portfolio nationwide, no geographic scope needed:
    shovels properties search --legal-owner "INVITATION HOMES"

  Several owners at once (commas inside a name are never split):
    shovels properties search --legal-owner "SMITH, JOHN" --legal-owner "ACME LLC"

  Roofing work started since 2024 that is still unfinaled:
    shovels properties search --geo-id CA --permit-tags-unfinaled roofing --permit-from 2024-01-01

  Finaled solar work in a county:
    GEO=$(shovels counties search -q "San Diego" | jq -r '.data[0].geo_id')
    shovels properties search --geo-id "$GEO" --permit-tags solar --permit-status final`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPropertiesSearch,
}

func runPropertiesSearch(cmd *cobra.Command, args []string) error {
	if err := validatePropertiesSearchFlags(cmd); err != nil {
		return err
	}

	lc, err := parseLimitConfig(cmd)
	if err != nil {
		return err
	}

	q := buildPropertiesSearchQuery(cmd)

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		q.Set("size", fmt.Sprintf("%d", lc.FirstPageSize()))
		return printDryRun(cmd, "/properties/search", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), "/properties/search", q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

// registerPropertiesSearchFlags adds the properties search flags onto cmd.
// Properties has its own flag set because its scope is "geo and/or owner"
// rather than a required geo plus date range, and it takes tag and status
// filters as single comma-separated values.
func registerPropertiesSearchFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// Required scope: at least one of geo-id / legal-owner.
	f.String("geo-id", "", `Geographic scope. Required unless --legal-owner is given. Formats:
  Zip code:  5-digit code directly (92024, 78701)
  Zip+4:     92024-1234
  State:     2-letter code directly (CA, TX, FL)
  City:      shovels cities search -q "Encinitas" | jq '.data[0].geo_id'
  County:    shovels counties search -q "San Diego" | jq '.data[0].geo_id'
  Address:   shovels addresses search -q "123 Main St" | jq '.data[0].geo_id'
  Jurisdiction geo_ids are rejected by this endpoint`)
	f.StringArray("legal-owner", nil,
		"Property legal owner. Repeat the flag for up to 10 owners; values are never split on commas,\n"+
			"so \"SMITH, JOHN\" is one owner. Without --geo-id this searches that owner nationwide")

	// Permit filters
	f.String("permit-tags", "",
		"Canonical permit tags as ONE comma-separated value (e.g. \"solar,-roofing\").\n"+
			"A bare tag keeps properties that have it; a - prefix keeps properties WITHOUT it")
	f.String("permit-status", "", "Permit status as one comma-separated value: final, in_review, inactive, active")
	f.String("permit-from", "",
		"Bind the tag/status/absence filters to this date (YYYY-MM-DD).\n"+
			"There is no --permit-to: use shovels permits search for a closed date window")
	f.String("permit-tags-unfinaled", "",
		"Keep properties with an UNFINALED permit of each named tag, as one comma-separated value (e.g. \"solar,roofing\")")

	// Response options
	f.Bool("include-count", false,
		"Request total result count (capped at 10,000). Returned as total_count in meta on the first page;\n"+
			"omitted when the count query times out")
}

// propertiesSearchFlagGroups returns the grouped help layout for properties search.
func propertiesSearchFlagGroups() []flagGroup {
	return []flagGroup{
		{
			Title: "Required Scope (at least one)",
			Names: []string{"geo-id", "legal-owner"},
		},
		{
			Title: "Permit Filters",
			Names: []string{"permit-tags", "permit-status", "permit-from", "permit-tags-unfinaled"},
		},
		{
			Title: "Response Options",
			Names: []string{"include-count"},
		},
	}
}

// validatePropertiesSearchFlags checks that a scope was given, that owner
// values are non-empty and within the API's count limit, and that
// --permit-from has the YYYY-MM-DD shape. Calendar validity is left to the
// API. Returns a non-nil error (already printed to stderr) on failure.
func validatePropertiesSearchFlags(cmd *cobra.Command) error {
	geoID, _ := cmd.Flags().GetString("geo-id")
	owners, _ := cmd.Flags().GetStringArray("legal-owner")

	var hasNamedOwner, hasEmptyOwner bool
	for _, owner := range owners {
		if owner == "" {
			hasEmptyOwner = true
		} else {
			hasNamedOwner = true
		}
	}

	// Scope is checked first: an owner list of nothing but empty strings is
	// reported as a missing scope, not as a bad value.
	if geoID == "" && !hasNamedOwner {
		output.PrintErrorTyped(os.Stderr, "at least one of --geo-id or --legal-owner required", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	if hasEmptyOwner {
		output.PrintErrorTyped(os.Stderr, "--legal-owner values must not be empty", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if len(owners) > maxPropertyLegalOwners {
		msg := fmt.Sprintf("maximum %d --legal-owner values per request, got %d", maxPropertyLegalOwners, len(owners))
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	from, _ := cmd.Flags().GetString("permit-from")
	if from != "" && !datePattern.MatchString(from) {
		msg := fmt.Sprintf("invalid date format for --permit-from: %q (expected YYYY-MM-DD)", from)
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	return nil
}

// buildPropertiesSearchQuery reads the properties search flags and builds the
// url.Values for GET /properties/search.
func buildPropertiesSearchQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}

	setNonEmptyStringFlag(cmd, "geo-id", "geo_id", q)
	addStringArrayParam(cmd, "legal-owner", "legal_owner", q)

	setNonEmptyStringFlag(cmd, "permit-tags", "permit_tags", q)
	setNonEmptyStringFlag(cmd, "permit-status", "permit_status", q)
	setNonEmptyStringFlag(cmd, "permit-from", "permit_from", q)
	setNonEmptyStringFlag(cmd, "permit-tags-unfinaled", "permit_tags_unfinaled", q)

	setBoolFlag(cmd, "include-count", "include_total_count", q)

	return q
}

func init() {
	registerPropertiesSearchFlags(propertiesSearchCmd)
	setGroupedUsage(propertiesSearchCmd, propertiesSearchFlagGroups())

	propertiesCmd.AddCommand(propertiesSearchCmd)
	rootCmd.AddCommand(propertiesCmd)
}

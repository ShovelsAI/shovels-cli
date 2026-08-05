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

// maxPropertyLegalOwners is the maximum number of --legal-owner values the
// API accepts on one properties search.
const maxPropertyLegalOwners = 10

// propertiesSearchEndpoint is the API path for properties search. It is a
// constant because endpointArrayParams keys off the same string: a literal
// that drifted from the map key would silently change --dry-run typing
// without failing a test.
const propertiesSearchEndpoint = "/properties/search"

// maxPropertiesGetIDs is the maximum number of address IDs accepted per
// properties get request.
const maxPropertiesGetIDs = 50

// propertyRangePair names both bounds of one property attribute range filter.
type propertyRangePair struct{ minFlag, maxFlag string }

// propertyRangePairs drives bound validation, which reads every bound before
// it judges any pair and so cannot be expressed as per-flag checks.
var propertyRangePairs = []propertyRangePair{
	{"property-min-market-value", "property-max-market-value"},
	{"property-min-lot-size", "property-max-lot-size"},
	{"property-min-building-area", "property-max-building-area"},
	{"property-min-unit-count", "property-max-unit-count"},
	{"property-min-year-built", "property-max-year-built"},
}

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
  get      Retrieve up to 50 properties by their exact address ID

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

Property attribute filters:
  Narrow by what the property is rather than what happened to it — type,
  price, size, and age. Market value is integer cents (50000000 = $500,000);
  lot size and building area are square feet.
  Attribute data covers roughly 60-70% of properties, and a property with no
  value for an attribute never matches a range filter on it, so every
  attribute filter narrows results to the covered set.

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
    shovels properties search --geo-id "$GEO" --permit-tags solar --permit-status final

  Homes worth $500k-$1M built before 1990, with no solar permit:
    shovels properties search --geo-id 92024 --permit-tags "-solar" \
      --property-type residential \
      --property-min-market-value 50000000 --property-max-market-value 100000000 \
      --property-max-year-built 1989`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPropertiesSearch,
}

func runPropertiesSearch(cmd *cobra.Command, args []string) error {
	if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
		return err
	}

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
		return printDryRun(cmd, propertiesSearchEndpoint, q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	result, err := cl.Paginate(context.Background(), propertiesSearchEndpoint, q, lc)
	if err != nil {
		return handleAPIError(err)
	}

	output.PrintPaginated(cmd.OutOrStdout(), result)
	return nil
}

var propertiesGetCmd = &cobra.Command{
	Use:   "get ID [ID...]",
	Short: "Retrieve one or more properties by their exact address ID (Beta)",
	Long: `Fetch specific properties by ID. Accepts 1 to 50 address IDs as positional
arguments, all in one request.

Beta: query parameters, response fields, and the absence-trust surface may
still change. Treat the response shape as unstable.

Note: ID is a positional argument, not a flag.
  Correct:   shovels properties get a_123
  Incorrect: shovels properties get --id a_123

IDs are address IDs. Take them from the id field of a properties search row,
or resolve a street address:
  shovels addresses search -q "123 Main St, Encinitas CA" | jq -r '.data[0].geo_id'
City, county, and jurisdiction geo_ids are rejected, and one such ID fails the
whole request, so pass address IDs only.

Examples:
  Single property:
    shovels properties get a_123

  Several properties in one request:
    shovels properties get a_123 a_456 a_789

Response: {"data": [...], "meta": {"count": N, "missing": ["UNKNOWN_ID"], ...}}
Address IDs with no property behind them appear in meta.missing, which is
absent when every requested ID resolved. Trust metadata is a search-only
surface: rows returned here carry no trust object.`,
	Annotations: map[string]string{
		AnnotationRequiresAuth: "true",
	},
	RunE: runPropertiesGet,
}

func runPropertiesGet(cmd *cobra.Command, args []string) error {
	if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
		return err
	}

	if len(args) == 0 {
		output.PrintErrorTyped(os.Stderr, "at least one property ID required", 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}
	if len(args) > maxPropertiesGetIDs {
		msg := fmt.Sprintf("maximum %d IDs per request", maxPropertiesGetIDs)
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	q := url.Values{}
	for _, id := range args {
		q.Add("id", id)
	}

	if _, err := validateTimeout(cmd); err != nil {
		return err
	}

	if isDryRun(cmd) {
		return printDryRun(cmd, "/properties", q)
	}

	cl, err := newClientFromFlags(cmd)
	if err != nil {
		return err
	}

	resp, err := cl.Get(cmd.Context(), "/properties", q)
	if err != nil {
		return handleAPIError(err)
	}

	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		output.PrintErrorTyped(os.Stderr, "failed to parse API response", 1, client.ErrorTypeClient)
		return &exitError{code: 1}
	}

	// The endpoint answers an unresolved address ID by omitting its row rather
	// than failing, so the requested IDs are the only record of what was asked
	// for.
	missing := findMissingIDs(args, page.Items)
	output.PrintBatch(cmd.OutOrStdout(), page.Items, missing, resp.Credits)
	return nil
}

// registerPropertiesSearchFlags adds the properties search flags onto cmd.
// Properties has its own flag set because its scope is "geo and/or owner"
// rather than a required geo plus date range.
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
	f.StringSlice("permit-tags", nil,
		"Canonical permit tags. Repeat the flag or comma-separate to filter on several tags.\n"+
			"A bare tag keeps properties that have it; a - prefix keeps properties WITHOUT it.\n"+
			"Several positive tags require EVERY tag, though not all on the same permit.\n"+
			"An unknown tag is an error, not an empty result: run shovels tags list --limit all for the canonical set")
	f.StringSlice("permit-status", nil,
		"Permit status. Repeat the flag or comma-separate to match any of several statuses.\n"+
			"Valid values: final, in_review, inactive, active")
	f.String("permit-from", "",
		"Bind the tag/status/absence filters to this date (YYYY-MM-DD).\n"+
			"There is no --permit-to: use shovels permits search for a closed date window")
	f.StringSlice("permit-tags-unfinaled", nil,
		"Keep properties with an UNFINALED permit of each named tag.\n"+
			"Repeat the flag or comma-separate to name several tags (e.g. solar,roofing)")

	// Property attribute filters. Permits search additionally filters on story
	// count; the Properties API has no story-count parameter.
	f.StringSlice("property-type", nil,
		"Property type. Repeat the flag or comma-separate to match any of several types.\n"+
			"Valid values: residential, commercial, industrial, agricultural, vacant land,\n"+
			"exempt, miscellaneous, office, recreational")
	f.Int("property-min-market-value", 0, "Minimum assessed market value in integer cents (50000000 = $500,000)")
	f.Int("property-max-market-value", 0, "Maximum assessed market value in integer cents (50000000 = $500,000)")
	f.Int("property-min-lot-size", 0, "Minimum lot size in square feet")
	f.Int("property-max-lot-size", 0, "Maximum lot size in square feet")
	f.Int("property-min-building-area", 0, "Minimum building area in square feet")
	f.Int("property-max-building-area", 0, "Maximum building area in square feet")
	f.Int("property-min-unit-count", 0, "Minimum unit count")
	f.Int("property-max-unit-count", 0, "Maximum unit count")
	f.Int("property-min-year-built", 0, "Minimum year built (e.g. 1990)")
	f.Int("property-max-year-built", 0, "Maximum year built (e.g. 1990)")

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
			Title: "Property Filters",
			Names: []string{
				"property-type",
				"property-min-market-value", "property-max-market-value",
				"property-min-lot-size", "property-max-lot-size",
				"property-min-building-area", "property-max-building-area",
				"property-min-unit-count", "property-max-unit-count",
				"property-min-year-built", "property-max-year-built",
			},
		},
		{
			Title: "Response Options",
			Names: []string{"include-count"},
		},
	}
}

// validatePropertiesSearchFlags checks that a scope was given, that owner
// values are non-empty and within the API's count limit, that --permit-status
// names real statuses, that --permit-from has the YYYY-MM-DD shape, and that
// the attribute range bounds are usable. Calendar validity, tag vocabulary,
// and property-type membership are left to the API, which rejects an unknown
// value with a 422 naming it.
// Returns a non-nil error (already printed to stderr) on failure.
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

	// Statuses are a closed set, so catch a typo locally rather than spending a
	// round trip on it. This matches permits and contractors search, which
	// validate --status the same way. Tags are left to the API, whose
	// vocabulary the CLI does not carry.
	statuses, _ := cmd.Flags().GetStringSlice("permit-status")
	for _, s := range statuses {
		if !isValidStatus(s) {
			msg := fmt.Sprintf("invalid --permit-status value %q: valid options are %s", s, strings.Join(validPermitStatuses, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}

	from, _ := cmd.Flags().GetString("permit-from")
	if from != "" && !datePattern.MatchString(from) {
		msg := fmt.Sprintf("invalid date format for --permit-from: %q (expected YYYY-MM-DD)", from)
		output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
		return &exitError{code: 1}
	}

	return validatePropertyRangeFlags(cmd)
}

// validatePropertyRangeFlags rejects negative bounds and inverted pairs on the
// property attribute range filters. Every bound is checked for negativity
// before any pair is checked for inversion, so a pair that is both negative
// and inverted reports the negative value rather than the ordering.
func validatePropertyRangeFlags(cmd *cobra.Command) error {
	for _, pair := range propertyRangePairs {
		for _, flag := range []string{pair.minFlag, pair.maxFlag} {
			if !cmd.Flags().Changed(flag) {
				continue
			}
			if v, _ := cmd.Flags().GetInt(flag); v < 0 {
				msg := fmt.Sprintf("--%s must not be negative, got %d", flag, v)
				output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
				return &exitError{code: 1}
			}
		}
	}

	for _, pair := range propertyRangePairs {
		if !cmd.Flags().Changed(pair.minFlag) || !cmd.Flags().Changed(pair.maxFlag) {
			continue
		}
		lower, _ := cmd.Flags().GetInt(pair.minFlag)
		upper, _ := cmd.Flags().GetInt(pair.maxFlag)
		if lower > upper {
			msg := fmt.Sprintf("--%s (%d) must not exceed --%s (%d)",
				pair.minFlag, lower, pair.maxFlag, upper)
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
	}

	return nil
}

// buildPropertiesSearchQuery reads the properties search flags and builds the
// url.Values for GET /properties/search.
func buildPropertiesSearchQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}

	setNonEmptyStringFlag(cmd, "geo-id", "geo_id", q)
	addStringArrayParam(cmd, "legal-owner", "legal_owner", q)

	// /properties/search takes these as repeated keys and rejects a comma-joined
	// value with a 422, so each tag must become its own parameter.
	addStringSliceParam(cmd, "permit-tags", "permit_tags", q)
	addStringSliceParam(cmd, "permit-status", "permit_status", q)
	setNonEmptyStringFlag(cmd, "permit-from", "permit_from", q)
	addStringSliceParam(cmd, "permit-tags-unfinaled", "permit_tags_unfinaled", q)

	addStringSliceParam(cmd, "property-type", "property_type", q)
	setIntFlag(cmd, "property-min-market-value", "property_min_market_value", q)
	setIntFlag(cmd, "property-max-market-value", "property_max_market_value", q)
	setIntFlag(cmd, "property-min-lot-size", "property_min_lot_size", q)
	setIntFlag(cmd, "property-max-lot-size", "property_max_lot_size", q)
	setIntFlag(cmd, "property-min-building-area", "property_min_building_area", q)
	setIntFlag(cmd, "property-max-building-area", "property_max_building_area", q)
	setIntFlag(cmd, "property-min-unit-count", "property_min_unit_count", q)
	setIntFlag(cmd, "property-max-unit-count", "property_max_unit_count", q)
	setIntFlag(cmd, "property-min-year-built", "property_min_year_built", q)
	setIntFlag(cmd, "property-max-year-built", "property_max_year_built", q)

	setBoolFlag(cmd, "include-count", "include_total_count", q)

	return q
}

func init() {
	registerPropertiesSearchFlags(propertiesSearchCmd)
	registerSchemaFlag(propertiesSearchCmd)
	registerSchemaFlag(propertiesGetCmd)
	setGroupedUsage(propertiesSearchCmd, propertiesSearchFlagGroups())

	propertiesCmd.AddCommand(propertiesSearchCmd)
	propertiesCmd.AddCommand(propertiesGetCmd)
	rootCmd.AddCommand(propertiesCmd)
}

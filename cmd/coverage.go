package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/shovels-ai/shovels-cli/internal/client"
	"github.com/shovels-ai/shovels-cli/internal/output"
	"github.com/spf13/cobra"
)

// coverageEndpoint is the API path for the data-coverage endpoint. The base
// URL already carries the /v2 prefix, so this resolves to /v2/meta/coverage.
const coverageEndpoint = "/meta/coverage"

// coverageResponse wraps the bare {"items":[...]} body returned by the
// coverage endpoint. Only the items array is forwarded to the envelope.
type coverageResponse struct {
	Items []json.RawMessage `json:"items"`
	// itemsPresent records whether the "items" key existed in the body,
	// distinguishing an absent key from an empty array.
	itemsPresent bool
}

// UnmarshalJSON decodes the coverage body and records whether the "items" key
// was present so a missing or non-array "items" can be rejected.
func (c *coverageResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		Items *[]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Items == nil {
		return nil
	}
	c.itemsPresent = true
	c.Items = *raw.Items
	return nil
}

// runCoverage returns a cobra RunE that handles a coverage request for the
// given geo_type. The geography type is fixed per parent command (e.g.
// "zipcode" for zipcodes); geo_id is the positional argument. It validates the
// date flags, constructs the query, calls GET /meta/coverage, and maps the
// {"items":[...]} body to the standard {"data":[...],"meta":{}} envelope.
func runCoverage(geoType string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if handled, err := handleSchemaFlag(cmd, commandPathFromCobra(cmd)); handled {
			return err
		}

		coverageFrom, _ := cmd.Flags().GetString("coverage-from")
		coverageTo, _ := cmd.Flags().GetString("coverage-to")

		var missing []string
		if coverageFrom == "" {
			missing = append(missing, "--coverage-from")
		}
		if coverageTo == "" {
			missing = append(missing, "--coverage-to")
		}
		if len(missing) > 0 {
			msg := fmt.Sprintf("required flag(s) missing: %s", strings.Join(missing, ", "))
			output.PrintErrorTyped(os.Stderr, msg, 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}

		if !datePattern.MatchString(coverageFrom) {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --coverage-from: %q (expected YYYY-MM-DD)", coverageFrom), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}
		if !datePattern.MatchString(coverageTo) {
			output.PrintErrorTyped(os.Stderr, fmt.Sprintf("invalid date format for --coverage-to: %q (expected YYYY-MM-DD)", coverageTo), 1, client.ErrorTypeValidation)
			return &exitError{code: 1}
		}

		// geo_id is forwarded verbatim — the API resolves and normalizes it.
		geoID := args[0]

		q := url.Values{
			"geo_type":  {geoType},
			"geo_id":    {geoID},
			"date_from": {coverageFrom},
			"date_to":   {coverageTo},
		}

		if _, err := validateTimeout(cmd); err != nil {
			return err
		}

		if isDryRun(cmd) {
			return printDryRun(cmd, coverageEndpoint, q)
		}

		cl, err := newClientFromFlags(cmd)
		if err != nil {
			return err
		}

		resp, err := cl.Get(cmd.Context(), coverageEndpoint, q)
		if err != nil {
			return handleAPIError(err)
		}

		var body coverageResponse
		if err := json.Unmarshal(resp.Body, &body); err != nil || !body.itemsPresent {
			output.PrintErrorTyped(os.Stderr, "failed to parse API response", 1, client.ErrorTypeClient)
			return &exitError{code: 1}
		}

		// Pass only the items array so data is the array, not {"items":[...]}.
		output.PrintSingle(cmd.OutOrStdout(), body.Items, resp.Credits)
		return nil
	}
}

// registerCoverageFlags registers the shared coverage flags (--coverage-from,
// --coverage-to) and the --schema flag on a coverage subcommand.
func registerCoverageFlags(cmd *cobra.Command) {
	cmd.Flags().String("coverage-from", "", "Start date in YYYY-MM-DD format (required)")
	cmd.Flags().String("coverage-to", "", "End date in YYYY-MM-DD format (required)")
	registerSchemaFlag(cmd)
}

// newCoverageCmd builds a `coverage GEO_ID` subcommand for the given geography.
// geoType is the API geo_type discriminator (e.g. "city", "zipcode"); long is
// the LLM-friendly help text for the parent geography.
func newCoverageCmd(geoType, short, long string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coverage GEO_ID",
		Short: short,
		Long:  long,
		Args:  exactArgsUnlessSchema(1),
		Annotations: map[string]string{
			AnnotationRequiresAuth: "true",
		},
		RunE: runCoverage(geoType),
	}
	registerCoverageFlags(cmd)
	return cmd
}

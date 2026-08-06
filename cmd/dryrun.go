package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"

	"github.com/spf13/cobra"
)

// dryRunRequest is the JSON structure printed when --dry-run is active.
type dryRunRequest struct {
	Method string         `json:"method"`
	URL    string         `json:"url"`
	Params map[string]any `json:"params"`
}

// isDryRun returns true when the --dry-run flag is set on the command.
func isDryRun(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("dry-run")
	return v
}

// printDryRun writes the resolved HTTP request to stdout and returns nil.
// The endpoint path is appended to the resolved base URL. Query parameters
// are converted from url.Values to a typed map: single values become strings,
// multi-values become string arrays.
func printDryRun(cmd *cobra.Command, endpoint string, query url.Values) error {
	cfg := ResolvedConfig()
	fullURL := cfg.BaseURL + endpoint

	params := valuesToMap(endpoint, query)

	out := dryRunRequest{
		Method: "GET",
		URL:    fullURL,
		Params: params,
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("failed to encode dry-run output: %w", err)
	}
	return nil
}

// numericParams lists query parameter names that represent integers in the
// dry-run JSON output. All other parameters remain strings to match their
// API semantics (e.g., geo_id "92024" stays a string).
var numericParams = map[string]bool{
	"size": true,
}

// arrayParams lists query parameter names that are array-shaped on every
// endpoint that sends them, so they are always rendered as JSON arrays even
// when they hold a single value. These correspond to API parameters that
// accept repeated values (e.g., permit_tags=solar&permit_tags=roofing).
//
// A parameter whose shape varies by endpoint does NOT belong here — no single
// entry can be correct for it. Use endpointArrayParams instead.
var arrayParams = map[string]bool{
	"permit_tags":                       true,
	"permit_status":                     true,
	"permit_tags_unfinaled":             true,
	"contractor_classification_derived": true,
	"id":                                true,
	"legal_owner":                       true,
	"asset_class":                       true,
	"category":                          true,
	"subcategory":                       true,
}

// paramKey identifies a query parameter on one specific endpoint.
type paramKey struct {
	endpoint string
	param    string
}

// endpointArrayParams lists parameters whose shape depends on the endpoint,
// so a name alone cannot decide how to render them. property_type is a
// repeated key on the four search routes but a single scalar on the metrics
// commands, which take one value — hence per-endpoint rather than a global
// entry, which would render those as a one-element array.
//
// Endpoints are matched literally against the path passed to printDryRun, so
// only fixed paths can appear here — a path built with fmt.Sprintf (for
// example /cities/{geo_id}/metrics/current) can never match a key. This map
// also only expresses "scalar by default, array on these endpoints"; a
// parameter that is array-shaped everywhere except one endpoint would need
// the inverse and does not belong in either map as written.
var endpointArrayParams = map[paramKey]bool{
	{endpoint: propertiesSearchEndpoint, param: "property_type"}:  true,
	{endpoint: decisionsSearchEndpoint, param: "property_type"}:   true,
	{endpoint: permitsSearchEndpoint, param: "property_type"}:     true,
	{endpoint: contractorsSearchEndpoint, param: "property_type"}: true,
}

// valuesToMap converts url.Values into a map suitable for JSON output.
// Single-value keys become strings; multi-value keys become string arrays.
// Parameters listed in numericParams are converted to integers. Parameters
// that are array-shaped — globally via arrayParams, or on this endpoint via
// endpointArrayParams — always produce arrays. Keys are sorted for
// deterministic output.
func valuesToMap(endpoint string, query url.Values) map[string]any {
	m := make(map[string]any, len(query))
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := query[k]
		if arrayParams[k] || endpointArrayParams[paramKey{endpoint, k}] {
			m[k] = v
			continue
		}
		if len(v) == 1 {
			if numericParams[k] {
				if n, err := strconv.Atoi(v[0]); err == nil {
					m[k] = n
					continue
				}
			}
			m[k] = v[0]
		} else {
			m[k] = v
		}
	}
	return m
}

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

	params := valuesToMap(query)

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

// arrayParams lists query parameter names that are always rendered as
// JSON arrays, even when they contain a single value. These correspond
// to API parameters that accept repeated values (e.g., permit_tags=solar&permit_tags=roofing).
var arrayParams = map[string]bool{
	"permit_tags":                       true,
	"permit_status":                     true,
	"contractor_classification_derived": true,
	"id":                                true,
}

// valuesToMap converts url.Values into a map suitable for JSON output.
// Single-value keys become strings; multi-value keys become string arrays.
// Parameters listed in numericParams are converted to integers. Parameters
// listed in arrayParams always produce arrays. Keys are sorted for
// deterministic output.
func valuesToMap(query url.Values) map[string]any {
	m := make(map[string]any, len(query))
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := query[k]
		if arrayParams[k] {
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

//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// openAPISpecURL is the live spec. Unauthenticated, so this test costs no
// credits and needs no key — it is the one case in this package that does not
// call runCLI.
const openAPISpecURL = "https://api.shovels.ai/v2/openapi.json"

// TestOpenAPIDeclaresRepeatedKeyFiltersAsArrays watches the declared contract
// for the parameters the CLI encodes as repeated keys.
//
// It is deliberately narrow: route, parameter name, and semantic type only. A
// full-spec golden or a description diff would be theatre — the spec churns,
// and a raw JSON-shape assertion would be brittle because these parameters are
// declared as nullable anyOf arrays and omit style/explode entirely, relying on
// the OpenAPI defaults (form + explode=true, which is what repeated keys are).
//
// This supplements the live CLI requests rather than replacing them. It cannot
// detect an implementation that diverges from its own spec, which is exactly
// what happened when /properties/search changed shape — so if the spec had
// lagged the deploy, only the request-level tests would have caught it. Its
// value is the other direction: a spec change is visible the same day, before
// anyone runs a query that fails.
func TestOpenAPIDeclaresRepeatedKeyFiltersAsArrays(t *testing.T) {
	spec := fetchSpec(t)

	// NOTE on property_type for /permits/search and /contractors/search: the API
	// declares and honours an array here, but the CLI still registers
	// --property-type as f.String on those two commands, so repeating it drops
	// all but the last value (ENG-4061). These entries assert the API side only.
	// Passing here does NOT mean that parameter is healthy end to end.
	arrayParams := map[string][]string{
		"/properties/search":  {"permit_tags", "permit_status", "permit_tags_unfinaled", "property_type"},
		"/permits/search":     {"permit_tags", "permit_status", "property_type"},
		"/contractors/search": {"permit_tags", "permit_status", "property_type"},
		"/decisions/search":   {"asset_class", "category", "property_type"},
	}

	for route, params := range arrayParams {
		for _, name := range params {
			t.Run(route+" "+name, func(t *testing.T) {
				declared, ok := declaredType(spec, route, name)
				if !ok {
					t.Fatalf("%s no longer declares a %q parameter", route, name)
				}
				if declared != "array" {
					t.Errorf("%s %s is declared as %q, want \"array\".\n"+
						"The CLI sends this as repeated keys. A change to a scalar means "+
						"comma-joined and repeated forms now mean different things on the "+
						"wire, which is how CLI v0.8.0 broke.", route, name, declared)
				}
			})
		}
	}
}

// declaredType returns the semantic type of a query parameter, unwrapping the
// nullable anyOf wrapper the spec uses (anyOf: [{type: array}, {type: null}]).
func declaredType(spec map[string]any, route, param string) (string, bool) {
	paths, _ := spec["paths"].(map[string]any)
	entry, _ := paths[route].(map[string]any)
	get, _ := entry["get"].(map[string]any)
	params, _ := get["parameters"].([]any)

	for _, p := range params {
		pm, _ := p.(map[string]any)
		if pm["name"] != param {
			continue
		}
		schema, _ := pm["schema"].(map[string]any)
		if t, ok := schema["type"].(string); ok {
			return t, true
		}
		variants, _ := schema["anyOf"].([]any)
		for _, v := range variants {
			vm, _ := v.(map[string]any)
			if t, ok := vm["type"].(string); ok && t != "null" {
				return t, true
			}
		}
		return "", true // present, but no resolvable type
	}
	return "", false
}

func fetchSpec(t *testing.T) map[string]any {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(openAPISpecURL)
	if err != nil {
		t.Fatalf("fetching the live OpenAPI spec failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("live OpenAPI spec returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading spec body: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("parsing spec JSON: %v", err)
	}
	return spec
}

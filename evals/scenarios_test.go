//go:build eval

package evals

import (
	"encoding/json"
	"strings"
	"testing"
)

// Scenario defines a blind task for an LLM agent to complete using only
// the shovels CLI and its --help output.
type Scenario struct {
	Name           string   // test name
	Task           string   // natural language prompt — no CLI jargon or hints
	Domain         string   // expected resource: "permits" or "contractors"
	MustHaveFields []string // dot-separated JSON paths in final_output (e.g. "meta.count")
	MinResults     int      // minimum items in data array (0 = any)

	// ValidateOutput replaces the default data-array validation when set.
	// Used for scenarios whose output is not a standard CLI envelope
	// (e.g. schema, dry-run, jq pipeline results).
	ValidateOutput func(t *testing.T, report AgentReport)
}

var scenarios = []Scenario{
	{
		Name:           "SolarPermits",
		Task:           `Show me residential solar permits in Encinitas from 2024.`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "ElectricalContractor",
		Task:           `I need an electrical contractor in California — who's highly rated?`,
		Domain:         "contractors",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "CityPermits",
		Task:           `Find building permits issued in Miami in 2024.`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name:           "SolarInTexas",
		Task:           `What solar permits were filed in Texas in 2024?`,
		Domain:         "permits",
		MustHaveFields: []string{"data", "meta.count"},
		MinResults:     1,
	},
	{
		Name: "MetricsCurrent",
		Task: `What are the current solar permit metrics for zip code 92024?`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireNonEmptyJSON(t, report.FinalOutput)
		},
	},
	{
		Name: "SchemaDiscovery",
		Task: `Show me the schema for the permits search command.`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireFieldPresent(t, parsed, "response_fields")
			requireFieldPresent(t, parsed, "filters")
		},
	},
	{
		Name: "DryRunDiscovery",
		Task: `Show me what API request would be made for solar permits in Texas without actually calling it.`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireFieldPresent(t, parsed, "method")
			requireFieldPresent(t, parsed, "url")
			requireFieldPresent(t, parsed, "params")
		},
	},
	{
		Name: "JqTotalJobValue",
		Task: `Find the total job value of all solar permits in zip 92024 from 2024.`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireNumericOutput(t, report.FinalOutput)
		},
	},
	{
		Name: "JqTopPermits",
		Task: `Show me the top 3 highest-value solar permits in zip 92024 from 2024, sorted by job value.`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireNonEmptyJSON(t, report.FinalOutput)
		},
	},
	{
		Name: "JqMonthlyBreakdown",
		Task: `How many permits were filed per month in 2024 for solar in zip 92024?`,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireNonEmptyJSON(t, report.FinalOutput)
		},
	},
}

// requireNonEmptyJSON verifies the output is non-empty and contains valid JSON.
func requireNonEmptyJSON(t *testing.T, raw string) {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}
	if !containsJSON(trimmed) {
		t.Fatalf("final_output contains no parseable JSON:\n%.500s", trimmed)
	}
}

// requireParsedJSON extracts and returns a JSON object from the output,
// failing the test if none is found. Unlike extractJSONObject, this does
// not require a "data" key — it accepts any valid JSON object.
func requireParsedJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}

	// Fast path: entire output is a JSON object.
	var direct map[string]any
	if err := json.Unmarshal([]byte(trimmed), &direct); err == nil {
		return direct
	}

	// Slow path: scan for first valid JSON object.
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
		var obj map[string]any
		if err := dec.Decode(&obj); err == nil {
			return obj
		}
	}

	t.Fatalf("no valid JSON object found in final_output:\n%.500s", trimmed)
	return nil
}

// requireFieldPresent checks that a top-level key exists in the parsed JSON.
func requireFieldPresent(t *testing.T, obj map[string]any, field string) {
	t.Helper()
	if _, ok := obj[field]; !ok {
		t.Errorf("final_output missing required field %q", field)
	}
}

// requireNumericOutput verifies the output contains a numeric value.
// The agent may return just a number or embed it in text or JSON.
func requireNumericOutput(t *testing.T, raw string) {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}

	// Try parsing the whole output as a number.
	var num json.Number
	if err := json.Unmarshal([]byte(trimmed), &num); err == nil {
		return
	}

	// Try extracting a number from JSON (e.g. {"total": 12345}).
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		for _, v := range obj {
			switch v.(type) {
			case float64, json.Number:
				return
			}
		}
	}

	// Scan for any JSON number token in the output.
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if _, ok := tok.(json.Number); ok {
			return
		}
	}

	t.Fatalf("final_output contains no numeric value:\n%.500s", trimmed)
}

// containsJSON checks whether the string contains at least one valid JSON
// value (object, array, number, string, boolean, or null).
func containsJSON(s string) bool {
	dec := json.NewDecoder(strings.NewReader(s))
	for {
		_, err := dec.Token()
		if err != nil {
			break
		}
		return true
	}

	// Try scanning for embedded JSON objects/arrays.
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '[' {
			dec := json.NewDecoder(strings.NewReader(s[i:]))
			var v any
			if err := dec.Decode(&v); err == nil {
				return true
			}
		}
	}
	return false
}

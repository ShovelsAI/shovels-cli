//go:build eval

package evals

import (
	"encoding/json"
	"fmt"
	"regexp"
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

	// EnforceUsability makes usability rating < 4 a test failure instead
	// of an advisory warning. Set for new scenarios where help text quality
	// is a hard requirement.
	EnforceUsability bool

	// ValidateOutput replaces the default validation when set — the two are
	// alternative branches, so a scenario that sets it must leave Domain
	// empty or the domain check silently never runs. Carries the assertions
	// for output the default path cannot judge: a non-envelope result
	// (schema, dry-run, jq pipeline) or a resource it has no domain check for.
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
		Name:             "MetricsCurrent",
		Task:             `What are the current solar permit metrics for zip code 92024?`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireMetricsFields(t, parsed)
		},
	},
	{
		Name:             "SchemaDiscovery",
		Task:             `Show me the schema for the permits search command.`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireFieldPresent(t, parsed, "response_fields")
			requireFieldPresent(t, parsed, "filters")
		},
	},
	{
		Name:             "DryRunDiscovery",
		Task:             `Show me what API request would be made for solar permits in Texas without actually calling it.`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireFieldPresent(t, parsed, "method")
			requireFieldPresent(t, parsed, "url")
			requireFieldPresent(t, parsed, "params")
		},
	},
	{
		Name:             "JqTotalJobValue",
		Task:             `Find the total job value of all solar permits in zip 92024 from 2024.`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireJqCommand(t, report.FinalCommand)
			requireNumericOutput(t, report.FinalOutput)
		},
	},
	{
		Name:             "JqTopPermits",
		Task:             `Show me the top 3 highest-value solar permits in zip 92024 from 2024, sorted by job value.`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireJqCommand(t, report.FinalCommand)
			items := requireJSONArrayAtMostWithItems(t, report.FinalOutput, 3)
			requireJobValueFields(t, items)
			requireDescendingJobValue(t, items)
		},
	},
	{
		Name:             "ZoningDecisions",
		Task:             `Find rezoning / zoning decisions in California in 2024.`,
		EnforceUsability: true,
		MinResults:       0,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			parsed := requireParsedJSON(t, report.FinalOutput)
			requireFieldPresent(t, parsed, "data")
			requireDecisionsData(t, parsed)
			requireDecisionsSearchUsed(t, report)
		},
	},
	{
		Name:             "JqMonthlyBreakdown",
		Task:             `How many permits were filed per month in 2024 for solar in zip 92024?`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			requireJqCommand(t, report.FinalCommand)
			requireMultiEntryOutput(t, report.FinalOutput, 2)
			requireDateLikeContent(t, report.FinalOutput)
		},
	},
	{
		Name:             "PropertiesNoSolar",
		Task:             `Which properties in Encinitas have no solar permit?`,
		EnforceUsability: true,
		ValidateOutput: func(t *testing.T, report AgentReport) {
			t.Helper()
			for _, problem := range checkNoSolarProperties(report.FinalOutput) {
				t.Error(problem)
			}
		},
	},
}

// noSolarDisambiguation is the manual command that separates a wrong agent
// query from upstream data change when the PropertiesNoSolar scenario finds
// no rows.
const noSolarDisambiguation = `shovels properties search --geo-id 92024 --permit-tags=-solar --limit 1`

// checkNoSolarProperties reports every requirement of an absence-filtered
// properties search result that finalOutput fails, and returns nil when it
// meets them all.
//
// The trust checks double as the resource check, which is why the scenario
// declares no Domain. The properties API attaches a per-row trust object and
// a meta.trust_summaries array only to searches carrying an absence filter (a
// "-"-prefixed permit tag): presence-only property searches leave trust null
// and omit trust_summaries, and permits and contractors rows carry no trust
// field at all.
//
// Only the envelope is read. Encinitas can be scoped by its resolved city
// geo_id or by ZIP 92024, and neither route is privileged.
func checkNoSolarProperties(finalOutput string) []string {
	obj, ok := parseJSONObject(finalOutput)
	if !ok {
		return []string{fmt.Sprintf("final_output holds no JSON object:\n%.500s", strings.TrimSpace(finalOutput))}
	}

	var problems []string

	data, isArray := obj["data"].([]any)
	if !isArray {
		problems = append(problems, "final_output has no 'data' array")
	}

	meta, _ := obj["meta"].(map[string]any)

	switch count, ok := meta["count"].(float64); {
	case !ok:
		problems = append(problems, "final_output has no numeric meta.count")
	case count < 1:
		problems = append(problems, fmt.Sprintf(
			"meta.count is %.0f: either the agent's query missed, or the Encinitas absence data changed upstream — run `%s` to tell those apart",
			count, noSolarDisambiguation))
	}

	if len(data) > 0 && !anyRowCarriesTrust(data) {
		problems = append(problems, "no row carries a non-null 'trust' object, so the search ran without an absence filter")
	}

	if _, ok := meta["trust_summaries"]; !ok {
		problems = append(problems, "final_output has no meta.trust_summaries, so the search ran without an absence filter")
	}

	return problems
}

// anyRowCarriesTrust reports whether any row holds a trust object rather than
// a null or absent trust field.
func anyRowCarriesTrust(data []any) bool {
	for _, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := row["trust"].(map[string]any); ok {
			return true
		}
	}
	return false
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
	obj, ok := parseJSONObject(trimmed)
	if !ok {
		t.Fatalf("no valid JSON object found in final_output:\n%.500s", trimmed)
	}
	return obj
}

// parseJSONObject returns the first JSON object in raw, reporting false when
// raw holds none. Agents wrap CLI output in prose often enough that the whole
// string rarely parses on its own.
func parseJSONObject(raw string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(raw)

	// Fast path: entire output is a JSON object.
	var direct map[string]any
	if err := json.Unmarshal([]byte(trimmed), &direct); err == nil {
		return direct, true
	}

	// Slow path: scan for first valid JSON object.
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
		var obj map[string]any
		if err := dec.Decode(&obj); err == nil {
			return obj, true
		}
	}

	return nil, false
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

// requireMetricsFields verifies the output contains at least one metrics-specific
// field name, either at the top level or nested inside a data envelope.
func requireMetricsFields(t *testing.T, obj map[string]any) {
	t.Helper()

	metricsFields := []string{
		"permit_count", "contractor_count", "avg_construction_duration",
		"total_job_value", "avg_inspection_pass_rate", "tag",
		"permit_active_count", "permit_in_review_count",
	}

	// Check top-level fields and data array items.
	targets := []map[string]any{obj}
	if data, ok := obj["data"].([]any); ok {
		for _, item := range data {
			if m, ok := item.(map[string]any); ok {
				targets = append(targets, m)
			}
		}
	}

	for _, target := range targets {
		for _, field := range metricsFields {
			if _, ok := target[field]; ok {
				return
			}
		}
	}

	t.Errorf("final_output missing metrics-specific fields; expected at least one of %v", metricsFields)
}

// requireDecisionsData verifies the output's data value is a JSON array and,
// when non-empty, that at least one item carries a decisions-distinctive field.
// An empty data array is acceptable: decisions data is sparse, so usability —
// not data volume — is the gate (MinResults: 0).
func requireDecisionsData(t *testing.T, obj map[string]any) {
	t.Helper()

	data, ok := obj["data"].([]any)
	if !ok {
		t.Errorf("final_output 'data' is not a JSON array")
		return
	}
	if len(data) == 0 {
		return // sparse decisions data is acceptable
	}

	distinctiveFields := []string{"decision_date", "zoning_new"}
	for _, item := range data {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range distinctiveFields {
			if _, ok := m[field]; ok {
				return
			}
		}
	}

	t.Errorf("final_output 'data' items missing decisions-distinctive fields; expected at least one of %v", distinctiveFields)
}

// requireDecisionsSearchUsed verifies the agent actually discovered and ran
// the decisions search command. Without this, an empty data array from any
// unrelated command would satisfy the data-shape checks. The invocation may
// land in FinalCommand or in any earlier step — an agent that ends on a
// "... | jq ..." pipe records the jq pipe as FinalCommand while the real
// search lives in Steps — so both are scanned.
func requireDecisionsSearchUsed(t *testing.T, report AgentReport) {
	t.Helper()

	const marker = "decisions search"
	if strings.Contains(report.FinalCommand, marker) {
		return
	}
	for _, step := range report.Steps {
		if strings.Contains(step.Command, marker) {
			return
		}
	}

	var stepCommands []string
	for _, step := range report.Steps {
		stepCommands = append(stepCommands, step.Command)
	}
	t.Errorf("expected the agent to run %q; FinalCommand=%q, steps=%v",
		marker, report.FinalCommand, stepCommands)
}

// requireJSONArrayAtMost verifies the output contains a JSON array with at
// most maxItems elements. The output may be a bare JSON array or an object
// wrapping one.
func requireJSONArrayAtMost(t *testing.T, raw string, maxItems int) {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}

	// Try parsing as a bare JSON array.
	var arr []any
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
		if len(arr) > maxItems {
			t.Errorf("expected at most %d items in array, got %d", maxItems, len(arr))
		}
		if len(arr) == 0 {
			t.Error("expected non-empty array")
		}
		return
	}

	// Try parsing as an object and look for an array value.
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		for _, v := range obj {
			if a, ok := v.([]any); ok {
				if len(a) > maxItems {
					t.Errorf("expected at most %d items in array, got %d", maxItems, len(a))
				}
				if len(a) == 0 {
					t.Error("expected non-empty array")
				}
				return
			}
		}
	}

	// Scan for embedded JSON array.
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == '[' {
			dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
			var embedded []any
			if err := dec.Decode(&embedded); err == nil {
				if len(embedded) > maxItems {
					t.Errorf("expected at most %d items in array, got %d", maxItems, len(embedded))
				}
				if len(embedded) == 0 {
					t.Error("expected non-empty array")
				}
				return
			}
		}
	}

	t.Fatal("final_output contains no JSON array")
}

// requireMultiEntryOutput verifies the output contains multiple entries (at
// least minEntries). Accepts a JSON array with N elements, or a JSON object
// with N keys. Useful for monthly breakdowns and grouped aggregations.
func requireMultiEntryOutput(t *testing.T, raw string, minEntries int) {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}

	// Try as JSON array.
	var arr []any
	if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
		if len(arr) < minEntries {
			t.Errorf("expected at least %d entries in array, got %d", minEntries, len(arr))
		}
		return
	}

	// Try as JSON object (e.g. {"2024-01": 5, "2024-02": 12}).
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		if len(obj) < minEntries {
			t.Errorf("expected at least %d keys in object, got %d", minEntries, len(obj))
		}
		return
	}

	// Scan for embedded JSON array or object.
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case '[':
			dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
			var embedded []any
			if err := dec.Decode(&embedded); err == nil {
				if len(embedded) < minEntries {
					t.Errorf("expected at least %d entries in array, got %d", minEntries, len(embedded))
				}
				return
			}
		case '{':
			dec := json.NewDecoder(strings.NewReader(trimmed[i:]))
			var embedded map[string]any
			if err := dec.Decode(&embedded); err == nil {
				if len(embedded) >= minEntries {
					return
				}
			}
		}
	}

	t.Fatalf("final_output contains no JSON structure with %d+ entries:\n%.500s", minEntries, trimmed)
}

// requireJqCommand verifies the agent used jq in its final command.
// Blind eval agents have freedom in how they construct pipelines, but
// jq scenarios must demonstrate jq discovery, not just lucky output.
func requireJqCommand(t *testing.T, finalCommand string) {
	t.Helper()
	if !strings.Contains(finalCommand, "jq") {
		t.Errorf("expected final_command to contain 'jq', got: %s", finalCommand)
	}
}

// requireJSONArrayAtMostWithItems is like requireJSONArrayAtMost but
// returns the parsed array items for further inspection.
func requireJSONArrayAtMostWithItems(t *testing.T, raw string, maxItems int) []map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		t.Fatal("final_output is empty")
	}

	arr := extractJSONArray(t, trimmed)
	if len(arr) == 0 {
		t.Fatal("expected non-empty array")
	}
	if len(arr) > maxItems {
		t.Errorf("expected at most %d items in array, got %d", maxItems, len(arr))
	}

	var items []map[string]any
	for _, elem := range arr {
		if obj, ok := elem.(map[string]any); ok {
			items = append(items, obj)
		}
	}
	if len(items) == 0 {
		t.Fatal("array items are not JSON objects")
	}
	return items
}

// extractJSONArray finds a JSON array in the output. It tries the
// full string first, then object values, then embedded arrays.
func extractJSONArray(t *testing.T, raw string) []any {
	t.Helper()

	// Bare JSON array.
	var arr []any
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr
	}

	// Object wrapping an array value.
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		for _, v := range obj {
			if a, ok := v.([]any); ok {
				return a
			}
		}
	}

	// Embedded array.
	for i := 0; i < len(raw); i++ {
		if raw[i] == '[' {
			dec := json.NewDecoder(strings.NewReader(raw[i:]))
			var embedded []any
			if err := dec.Decode(&embedded); err == nil {
				return embedded
			}
		}
	}

	t.Fatal("final_output contains no JSON array")
	return nil
}

// requireJobValueFields verifies each item has a job_value field.
func requireJobValueFields(t *testing.T, items []map[string]any) {
	t.Helper()
	for i, item := range items {
		if _, ok := item["job_value"]; !ok {
			t.Errorf("item %d missing 'job_value' field", i)
		}
	}
}

// requireDescendingJobValue checks items are sorted by job_value
// descending. Skips verification if values are not parseable as numbers.
func requireDescendingJobValue(t *testing.T, items []map[string]any) {
	t.Helper()
	if len(items) < 2 {
		return
	}

	var values []float64
	for _, item := range items {
		v, ok := item["job_value"]
		if !ok {
			return // missing field — requireJobValueFields handles this
		}
		switch n := v.(type) {
		case float64:
			values = append(values, n)
		case json.Number:
			f, err := n.Float64()
			if err != nil {
				return // not parseable — skip order check
			}
			values = append(values, f)
		default:
			return // non-numeric — skip order check
		}
	}

	for i := 1; i < len(values); i++ {
		if values[i] > values[i-1] {
			t.Errorf("items not in descending job_value order: index %d (%.0f) > index %d (%.0f)",
				i, values[i], i-1, values[i-1])
		}
	}
}

// requireDateLikeContent verifies the output contains date-like patterns
// (YYYY-MM, month names) consistent with a monthly breakdown. This is
// intentionally permissive — agents may format months in various ways.
func requireDateLikeContent(t *testing.T, raw string) {
	t.Helper()

	// YYYY-MM pattern (e.g. "2024-01", "2024-12").
	yyyyMM := regexp.MustCompile(`20\d{2}-(?:0[1-9]|1[0-2])`)
	if yyyyMM.MatchString(raw) {
		return
	}

	// Month name patterns (full or abbreviated).
	months := []string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
		"Jan", "Feb", "Mar", "Apr", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
	}
	lower := strings.ToLower(raw)
	for _, m := range months {
		if strings.Contains(lower, strings.ToLower(m)) {
			return
		}
	}

	// YYYY/MM pattern.
	yyyySlashMM := regexp.MustCompile(`20\d{2}/(?:0[1-9]|1[0-2])`)
	if yyyySlashMM.MatchString(raw) {
		return
	}

	t.Error("final_output contains no date-like patterns (expected YYYY-MM, month names, or similar)")
}

// containsJSON checks whether the string contains at least one complete,
// valid JSON value (object, array, number, string, boolean, or null).
func containsJSON(s string) bool {
	// Fast path: entire string is valid JSON.
	if json.Valid([]byte(s)) {
		return true
	}

	// Slow path: scan for embedded JSON objects/arrays.
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

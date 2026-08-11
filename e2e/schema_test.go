//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// schemaOutput mirrors the JSON structure of schema command output.
type schemaOutput struct {
	SchemaVersion  int            `json:"schema_version"`
	Command        string         `json:"command"`
	ResponseFields map[string]any `json:"response_fields"`
	MetaFields     map[string]any `json:"meta_fields"`
	FieldIndex     []string       `json:"field_index"`
	Filters        map[string]any `json:"filters"`
}

func parseSchema(t *testing.T, stdout string) schemaOutput {
	t.Helper()
	var out schemaOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid schema JSON: %v\nstdout: %s", err, stdout)
	}
	return out
}

// =======================================================================
// Happy paths
// =======================================================================

func TestSchemaValidPath(t *testing.T) {
	result := runCLI(t, "schema", "permits", "search")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	if out.SchemaVersion != 1 {
		t.Errorf("expected schema_version 1, got %d", out.SchemaVersion)
	}
	if out.Command != "permits search" {
		t.Errorf("expected command %q, got %q", "permits search", out.Command)
	}
	if len(out.ResponseFields) == 0 {
		t.Error("expected non-empty response_fields")
	}
	if len(out.FieldIndex) == 0 {
		t.Error("expected non-empty field_index")
	}
	if len(out.Filters) == 0 {
		t.Error("expected non-empty filters")
	}
}

func TestSchemaFlagAlias(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	if out.Command != "permits search" {
		t.Errorf("expected command %q, got %q", "permits search", out.Command)
	}
	if out.SchemaVersion != 1 {
		t.Errorf("expected schema_version 1, got %d", out.SchemaVersion)
	}
}

func TestSchemaNoArgsListsPaths(t *testing.T) {
	result := runCLI(t, "schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var paths []string
	if err := json.Unmarshal([]byte(result.Stdout), &paths); err != nil {
		t.Fatalf("stdout is not valid JSON array: %v\nstdout: %s", err, result.Stdout)
	}

	if len(paths) == 0 {
		t.Fatal("expected non-empty list of command paths")
	}

	// Verify some expected paths are present.
	expected := []string{"permits search", "permits get", "properties search", "properties get", "contractors search", "tags list", "cities metrics current"}
	pathSet := make(map[string]bool, len(paths))
	for _, p := range paths {
		pathSet[p] = true
	}
	for _, e := range expected {
		if !pathSet[e] {
			t.Errorf("expected path %q in list", e)
		}
	}
}

// =======================================================================
// Edge cases
// =======================================================================

func TestSchemaMultiWordPath(t *testing.T) {
	result := runCLI(t, "schema", "cities", "metrics", "monthly")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	if out.Command != "cities metrics monthly" {
		t.Errorf("expected command %q, got %q", "cities metrics monthly", out.Command)
	}

	// Monthly should have date-related fields in the response.
	if _, ok := out.ResponseFields["date"]; !ok {
		t.Error("cities metrics monthly should have a date response field")
	}
}

func TestSchemaFlagIgnoresOtherFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search", "--schema",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Should produce schema output, not API response or dry-run output.
	out := parseSchema(t, result.Stdout)
	if out.Command != "permits search" {
		t.Errorf("expected schema output, got command %q", out.Command)
	}
}

func TestSchemaFlagPrecedenceOverDryRun(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search", "--schema", "--dry-run",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Schema should win over dry-run: output must have schema_version, not method.
	out := parseSchema(t, result.Stdout)
	if out.SchemaVersion != 1 {
		t.Error("expected schema output to take precedence over dry-run")
	}

	// Must not contain dry-run specific fields.
	if strings.Contains(result.Stdout, `"method"`) {
		t.Error("schema should take precedence over dry-run, but output contains 'method'")
	}
}

func TestSchemaFlagSkipsValidation(t *testing.T) {
	// No auth, no required flags, no positional args.
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without auth/flags, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	if out.Command != "permits search" {
		t.Errorf("expected schema for permits search, got %q", out.Command)
	}
}

func TestSchemaFlagSkipsPositionalArgs(t *testing.T) {
	// cities metrics current normally requires GEO_ID positional arg.
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "--schema",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without positional arg, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	if out.Command != "cities metrics current" {
		t.Errorf("expected schema for cities metrics current, got %q", out.Command)
	}
}

// =======================================================================
// Error conditions
// =======================================================================

func TestSchemaInvalidPath(t *testing.T) {
	result := runCLI(t, "schema", "foobar")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stdout: %s", result.ExitCode, result.Stdout)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "unknown command path") {
		t.Errorf("expected error about unknown command path, got: %s", p.Error)
	}
	// Error should list valid paths.
	if !strings.Contains(p.Error, "permits search") {
		t.Errorf("expected error to list valid paths, got: %s", p.Error)
	}
}

func TestSchemaPartialPath(t *testing.T) {
	result := runCLI(t, "schema", "permits")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stdout: %s", result.ExitCode, result.Stdout)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "incomplete path") {
		t.Errorf("expected error about incomplete path, got: %s", p.Error)
	}
	// Should suggest valid completions.
	if !strings.Contains(p.Error, "permits search") {
		t.Errorf("expected suggestion for 'permits search', got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "permits get") {
		t.Errorf("expected suggestion for 'permits get', got: %s", p.Error)
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

func TestSchemaHelpExplainsPurpose(t *testing.T) {
	result := runCLI(t, "schema", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Help should mention LLM agents and jq.
	if !strings.Contains(result.Stdout, "jq") {
		t.Error("schema --help should mention jq")
	}
	if !strings.Contains(strings.ToLower(result.Stdout), "llm") || !strings.Contains(strings.ToLower(result.Stdout), "agent") {
		t.Error("schema --help should mention LLM agents")
	}

	// The "Available command paths" section must contain real command paths,
	// not be empty due to init ordering.
	requiredPaths := []string{
		"permits search",
		"permits get",
		"contractors search",
		"cities metrics current",
		"tags list",
	}
	for _, p := range requiredPaths {
		if !strings.Contains(result.Stdout, p) {
			t.Errorf("schema --help 'Available command paths' section should list %q", p)
		}
	}

	// Help should include an example.
	if !strings.Contains(result.Stdout, "schema_version") {
		t.Error("schema --help should show example output with schema_version")
	}
}

func TestSchemaNoAuthRequired(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "schema", "permits", "search")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without auth, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	if out.Command != "permits search" {
		t.Errorf("expected schema for permits search, got %q", out.Command)
	}
}

func TestSchemaFlagMatchesSchemaCommand(t *testing.T) {
	// Verify --schema alias produces identical output to schema command.
	env := withIsolatedConfig(t)

	schemaResult := runCLIWithEnv(t, env, "schema", "tags", "list")
	flagResult := runCLIWithEnv(t, env, "tags", "list", "--schema")

	if schemaResult.ExitCode != 0 {
		t.Fatalf("schema command failed: exit %d; stderr: %s", schemaResult.ExitCode, schemaResult.Stderr)
	}
	if flagResult.ExitCode != 0 {
		t.Fatalf("--schema flag failed: exit %d; stderr: %s", flagResult.ExitCode, flagResult.Stderr)
	}

	if schemaResult.Stdout != flagResult.Stdout {
		t.Errorf("schema command and --schema flag produced different output:\ncommand: %s\nflag: %s", schemaResult.Stdout, flagResult.Stdout)
	}
}

func TestSchemaContractorsMetricsViaFlag(t *testing.T) {
	// contractors metrics requires positional arg and 4 required flags.
	// --schema should skip all of that.
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics", "--schema",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without args/flags, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	if out.Command != "contractors metrics" {
		t.Errorf("expected schema for contractors metrics, got %q", out.Command)
	}
}

// =======================================================================
// Contractor scope labels
// =======================================================================

func TestSchemaContractorsSearchGlobalScope(t *testing.T) {
	result := runCLI(t, "schema", "contractors", "search")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	// Every GLOBAL field must say "NOT filtered by search parameters".
	globalFields := []struct {
		field      string
		extraCheck string // additional substring that must appear
	}{
		{"permit_count", ""},
		{"avg_job_value", "in cents"},
		{"total_job_value", "in cents"},
		{"avg_construction_duration", ""},
		{"avg_inspection_pass_rate", ""},
	}
	for _, tc := range globalFields {
		desc := fieldDescription(t, out, tc.field)
		if !strings.Contains(desc, "NOT filtered by search parameters") {
			t.Errorf("%s should say 'NOT filtered by search parameters', got: %s", tc.field, desc)
		}
		if tc.extraCheck != "" && !strings.Contains(desc, tc.extraCheck) {
			t.Errorf("%s should contain %q, got: %s", tc.field, tc.extraCheck, desc)
		}
	}
}

func TestSchemaContractorsSearchFilteredScope(t *testing.T) {
	result := runCLI(t, "schema", "contractors", "search")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	// Every FILTERED field must say "FILTERED".
	filteredFields := []struct {
		field       string
		mustContain []string
	}{
		{"tag_tally", []string{"FILTERED", "--geo-id"}},
		{"status_tally", []string{"FILTERED", "active", "final", "unknown", "inactive", "in_review"}},
	}
	for _, tc := range filteredFields {
		desc := fieldDescription(t, out, tc.field)
		for _, s := range tc.mustContain {
			if !strings.Contains(desc, s) {
				t.Errorf("%s should contain %q, got: %s", tc.field, s, desc)
			}
		}
	}
}

func TestSchemaContractorsGetUnfilteredScope(t *testing.T) {
	result := runCLI(t, "schema", "contractors", "get")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	// tag_tally on get must say "Unfiltered lifetime".
	tagTally := fieldDescription(t, out, "tag_tally")
	if !strings.Contains(tagTally, "Unfiltered lifetime") {
		t.Errorf("contractors get tag_tally should say 'Unfiltered lifetime', got: %s", tagTally)
	}
}

func TestSchemaContractorsSearchTagTallyExceedsPermitCount(t *testing.T) {
	result := runCLI(t, "schema", "contractors", "search")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	tagTally := fieldDescription(t, out, "tag_tally")
	if !strings.Contains(tagTally, "exceed permit_count") {
		t.Errorf("tag_tally should explain sum can exceed permit_count, got: %s", tagTally)
	}
}

func TestSchemaContractorsGetVsSearchScopeDiffers(t *testing.T) {
	searchResult := runCLI(t, "schema", "contractors", "search")
	getResult := runCLI(t, "schema", "contractors", "get")

	if searchResult.ExitCode != 0 || getResult.ExitCode != 0 {
		t.Fatal("schema commands failed")
	}

	searchOut := parseSchema(t, searchResult.Stdout)
	getOut := parseSchema(t, getResult.Stdout)

	searchTagTally := fieldDescription(t, searchOut, "tag_tally")
	getTagTally := fieldDescription(t, getOut, "tag_tally")

	if searchTagTally == getTagTally {
		t.Errorf("tag_tally descriptions should differ between search and get, both say: %s", searchTagTally)
	}
}

// =======================================================================
// Batch-get meta convention
// =======================================================================

// TestSchemaBatchGetMetaConvention verifies every batch-get --schema documents
// the batch envelope its runtime output emits: count, missing, and credits,
// with no has_more. Batch-get responses look up IDs and are never paginated.
func TestSchemaBatchGetMetaConvention(t *testing.T) {
	for _, resource := range []string{"permits", "properties", "decisions", "contractors"} {
		resource := resource
		t.Run(resource, func(t *testing.T) {
			result := runCLI(t, "schema", resource, "get")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			assertBatchGetMeta(t, parseSchema(t, result.Stdout))
		})
	}
}

// =======================================================================
// Coverage schema
// =======================================================================

// TestSchemaCoveragePathsResolve verifies the 5 geo coverage schema paths
// resolve and describe the data[] item fields, the tier enum, the
// coverage date filters, and carry NO meta.* field index entries or
// --include-count filter (coverage is non-paginated and credit-exempt).
func TestSchemaCoveragePathsResolve(t *testing.T) {
	for _, geo := range []string{"cities", "counties", "jurisdictions", "states", "zipcodes"} {
		geo := geo
		t.Run(geo, func(t *testing.T) {
			result := runCLI(t, "schema", geo, "coverage")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := parseSchema(t, result.Stdout)

			want := geo + " coverage"
			if out.Command != want {
				t.Errorf("expected command %q, got %q", want, out.Command)
			}

			for _, field := range []string{"field", "tier", "fill_pct", "permits_total"} {
				if _, ok := out.ResponseFields[field]; !ok {
					t.Errorf("%s: response_fields missing %q", want, field)
				}
			}

			// tier enum must surface missing/partial/reliable.
			raw, ok := out.ResponseFields["tier"]
			if !ok {
				t.Fatalf("%s: response_fields missing tier", want)
			}
			m, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("%s: tier field is not an object", want)
			}
			enum, _ := m["enum"].(string)
			for _, val := range []string{"missing", "partial", "reliable"} {
				if !strings.Contains(enum, val) {
					t.Errorf("%s: tier enum should contain %q, got %q", want, val, enum)
				}
			}

			// No meta.* field index entries (non-paginated, credit-exempt).
			for _, f := range out.FieldIndex {
				if strings.HasPrefix(f, "meta.") {
					t.Errorf("%s: field_index should have no meta.* entries, got %q", want, f)
				}
			}

			// Filters: GEO_ID + coverage dates, no --include-count.
			for _, filter := range []string{"GEO_ID", "--coverage-from", "--coverage-to"} {
				if _, ok := out.Filters[filter]; !ok {
					t.Errorf("%s: filters missing %q", want, filter)
				}
			}
			if _, ok := out.Filters["--include-count"]; ok {
				t.Errorf("%s: filters should not include --include-count", want)
			}

			// Enriched descriptions must convey the two gap-causes so an
			// agent can infer WHY a field is unreliable from --schema alone.
			permitsTotalDesc := fieldDescription(t, out, "permits_total")
			if !strings.Contains(permitsTotalDesc, "0 means no permit data") {
				t.Errorf("%s: permits_total description should explain 0 => no data, got %q", want, permitsTotalDesc)
			}
			if !strings.Contains(permitsTotalDesc, "not distinguishable") {
				t.Errorf("%s: permits_total description should note the two sub-causes are not distinguishable, got %q", want, permitsTotalDesc)
			}

			fillPctDesc := fieldDescription(t, out, "fill_pct")
			if !strings.Contains(fillPctDesc, "permits_total > 0") {
				t.Errorf("%s: fill_pct description should explain the field-not-sourced cause, got %q", want, fillPctDesc)
			}
			if !strings.Contains(fillPctDesc, "does not populate") {
				t.Errorf("%s: fill_pct description should say the source jurisdiction does not populate the field, got %q", want, fillPctDesc)
			}

			tierDesc := fieldDescription(t, out, "tier")
			if !strings.Contains(tierDesc, "absent field is reliable") {
				t.Errorf("%s: tier description should state an absent field is reliable, got %q", want, tierDesc)
			}
		})
	}
}

// TestSchemaCoverageViaFlag verifies the `--schema` flag on a coverage
// subcommand resolves without a GEO_ID positional arg or auth.
func TestSchemaCoverageViaFlag(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "zipcodes", "coverage", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without arg/auth, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	if out.Command != "zipcodes coverage" {
		t.Errorf("expected schema for zipcodes coverage, got %q", out.Command)
	}
}

// fieldDescription extracts the description string from a response field.
func fieldDescription(t *testing.T, out schemaOutput, field string) string {
	t.Helper()
	raw, ok := out.ResponseFields[field]
	if !ok {
		t.Fatalf("response_fields missing %q", field)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("field %q is not an object", field)
	}
	desc, ok := m["description"].(string)
	if !ok {
		t.Fatalf("field %q has no description string", field)
	}
	return desc
}

// fieldUnit extracts the unit string from a response field.
func fieldUnit(t *testing.T, out schemaOutput, field string) string {
	t.Helper()
	raw, ok := out.ResponseFields[field]
	if !ok {
		t.Fatalf("response_fields missing %q", field)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("field %q is not an object", field)
	}
	unit, _ := m["unit"].(string)
	return unit
}

// filterUnit extracts the unit string from a filter entry.
func filterUnit(t *testing.T, out schemaOutput, filter string) string {
	t.Helper()
	raw, ok := out.Filters[filter]
	if !ok {
		t.Fatalf("filters missing %q", filter)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("filter %q is not an object", filter)
	}
	unit, _ := m["unit"].(string)
	return unit
}

// mustSchema runs `<args> --schema` and returns stdout, failing on non-zero exit.
func mustSchema(t *testing.T, env []string, args ...string) string {
	t.Helper()
	result := runCLIWithEnv(t, env, append(args, "--schema")...)
	if result.ExitCode != 0 {
		t.Fatalf("schema for %v failed: exit %d; stderr: %s", args, result.ExitCode, result.Stderr)
	}
	return result.Stdout
}

// fieldIndexSet returns the field_index entries as a lookup set.
func fieldIndexSet(out schemaOutput) map[string]bool {
	index := make(map[string]bool, len(out.FieldIndex))
	for _, f := range out.FieldIndex {
		index[f] = true
	}
	return index
}

// assertDataFieldIndex verifies the field_index advertises each given response
// field under the data[]. prefix that the runtime envelope nests items under.
func assertDataFieldIndex(t *testing.T, out schemaOutput, fields ...string) {
	t.Helper()
	index := fieldIndexSet(out)
	for _, f := range fields {
		entry := "data[]." + f
		if !index[entry] {
			t.Errorf("%s --schema field_index missing %q", out.Command, entry)
		}
	}
}

// assertSearchMeta verifies a paginated search --schema field index documents
// the search envelope: count, has_more, and credits. Search responses page
// through cursors, so has_more is part of the convention.
func assertSearchMeta(t *testing.T, out schemaOutput) {
	t.Helper()
	index := fieldIndexSet(out)
	for _, mf := range []string{"meta.count", "meta.has_more", "meta.credits_used", "meta.credits_remaining"} {
		if !index[mf] {
			t.Errorf("%s --schema field_index missing %q", out.Command, mf)
		}
	}
	if index["meta.missing"] {
		t.Errorf("%s --schema field_index should not advertise meta.missing (search is not a batch lookup)", out.Command)
	}
}

// assertBatchGetMeta verifies a batch-get --schema field index documents the
// batch envelope: count, missing, and credits present, has_more absent.
func assertBatchGetMeta(t *testing.T, out schemaOutput) {
	t.Helper()
	index := fieldIndexSet(out)
	for _, mf := range []string{"meta.count", "meta.missing", "meta.credits_used", "meta.credits_remaining"} {
		if !index[mf] {
			t.Errorf("%s --schema field_index missing %q", out.Command, mf)
		}
	}
	if index["meta.has_more"] {
		t.Errorf("%s --schema field_index should not advertise meta.has_more", out.Command)
	}
}

// =======================================================================
// Properties schema: nested trust expansion and meta fields
// =======================================================================

// propertiesTrustChildren are the eight direct PropertyTrust children, which
// both properties commands document as nested paths under trust.
var propertiesTrustChildren = []string{
	"trust.unresolved_rate",
	"trust.coverage_tier",
	"trust.data_horizon",
	"trust.horizon_basis",
	"trust.trust_jurisdiction_basis",
	"trust.trust_jurisdiction_error_bar",
	"trust.footprint_basis",
	"trust.flags",
}

// propertiesTrustSummaryFields are the four TrustSummary children, which the
// search command documents under meta_fields.
var propertiesTrustSummaryFields = []string{
	"trust_summaries[].rows_flagged",
	"trust_summaries[].row_weighted_unresolved_rate",
	"trust_summaries[].expected_miss_rate",
	"trust_summaries[].suppressed_scopes",
}

// TestSchemaPropertiesOfflineWithoutAuth verifies both properties commands
// answer --schema with no API key configured, no positional arguments, and
// without touching the API. The server answers 500 so that a request slipping
// through fails the command as well as the hit count.
func TestSchemaPropertiesOfflineWithoutAuth(t *testing.T) {
	for _, sub := range []string{"search", "get"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(500)
				w.Write([]byte(`{"detail":"schema output must not reach the API"}`))
			}))
			defer srv.Close()

			env := withIsolatedConfigNoAuth(t)

			result := runCLIWithEnv(t, env, "--base-url", srv.URL, "properties", sub, "--schema")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := parseSchema(t, result.Stdout)
			want := "properties " + sub
			if out.Command != want {
				t.Errorf("expected command %q, got %q", want, out.Command)
			}
			if out.SchemaVersion != 1 {
				t.Errorf("expected schema_version 1, got %d", out.SchemaVersion)
			}
			if len(out.ResponseFields) == 0 {
				t.Error("expected non-empty response_fields")
			}
			if got := hits.Load(); got != 0 {
				t.Errorf("expected zero API requests, got %d", got)
			}
		})
	}
}

// TestSchemaPropertiesTrustFieldsExpanded verifies both properties commands
// document every direct PropertyTrust child as a typed, described nested path
// indexed under data[], alongside the retained parent trust object.
func TestSchemaPropertiesTrustFieldsExpanded(t *testing.T) {
	for _, sub := range []string{"search", "get"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			out := parseSchema(t, mustSchema(t, nil, "properties", sub))

			parent, ok := out.ResponseFields["trust"].(map[string]any)
			if !ok {
				t.Fatalf("response_fields missing the parent trust object, got %v", out.ResponseFields["trust"])
			}
			if got, _ := parent["type"].(string); got != "object" {
				t.Errorf("parent trust has type %q, want object", got)
			}

			for _, child := range propertiesTrustChildren {
				field, ok := out.ResponseFields[child].(map[string]any)
				if !ok {
					t.Errorf("response_fields missing nested field %q", child)
					continue
				}
				if got, _ := field["type"].(string); got == "" {
					t.Errorf("nested field %q has no type", child)
				}
				if got, _ := field["description"].(string); got == "" {
					t.Errorf("nested field %q has no description", child)
				}
			}

			assertDataFieldIndex(t, out, append([]string{"trust"}, propertiesTrustChildren...)...)
		})
	}
}

// TestSchemaPropertiesTrustExpansionStopsAtDepthOne verifies the expansion
// documents the direct children of trust and nothing deeper: a grandchild
// path would mean the generator recursed into a child's own references, and a
// trust path outside the eight would mean it documented something the
// PropertyTrust schema does not carry.
func TestSchemaPropertiesTrustExpansionStopsAtDepthOne(t *testing.T) {
	wantChildren := make(map[string]bool, len(propertiesTrustChildren))
	for _, child := range propertiesTrustChildren {
		wantChildren[child] = true
	}

	for _, sub := range []string{"search", "get"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			out := parseSchema(t, mustSchema(t, nil, "properties", sub))

			for name := range out.ResponseFields {
				if depth := strings.Count(name, "."); depth > 1 {
					t.Errorf("response field %q nests %d levels; expansion documents direct children only", name, depth+1)
				}
				if strings.HasPrefix(name, "trust.") && !wantChildren[name] {
					t.Errorf("response field %q is not a PropertyTrust child", name)
				}
			}

			for _, entry := range out.FieldIndex {
				path := strings.TrimPrefix(strings.TrimPrefix(entry, "data[]."), "meta.")
				if depth := strings.Count(path, "."); depth > 1 {
					t.Errorf("field_index entry %q nests %d levels; expansion documents direct children only", entry, depth+1)
				}
			}

			for name := range out.MetaFields {
				if depth := strings.Count(name, "."); depth > 1 {
					t.Errorf("meta field %q nests %d levels; expansion documents direct children only", name, depth+1)
				}
			}
		})
	}
}

// TestSchemaPropertiesSearchMetaFieldsDocumentTrustSummaries verifies the
// search schema documents every TrustSummary child under the trust_summaries[]
// naming and indexes each one under meta, and documents nothing else there.
func TestSchemaPropertiesSearchMetaFieldsDocumentTrustSummaries(t *testing.T) {
	out := parseSchema(t, mustSchema(t, nil, "properties", "search"))

	index := fieldIndexSet(out)
	for _, name := range propertiesTrustSummaryFields {
		field, ok := out.MetaFields[name].(map[string]any)
		if !ok {
			t.Errorf("meta_fields missing %q", name)
			continue
		}
		if got, _ := field["type"].(string); got == "" {
			t.Errorf("meta field %q has no type", name)
		}
		if got, _ := field["description"].(string); got == "" {
			t.Errorf("meta field %q has no description", name)
		}
		if entry := "meta." + name; !index[entry] {
			t.Errorf("field_index missing %q", entry)
		}
	}

	if len(out.MetaFields) != len(propertiesTrustSummaryFields) {
		t.Errorf("meta_fields has %d entries, want the %d TrustSummary children: %v",
			len(out.MetaFields), len(propertiesTrustSummaryFields), out.MetaFields)
	}
}

// TestSchemaMetaFieldsOmittedWhenCommandAddsNone verifies meta_fields is
// additive: a command whose meta carries nothing beyond the standard envelope
// omits the key entirely rather than emitting an empty map. properties get is
// included because it returns no trust_summary to collect.
func TestSchemaMetaFieldsOmittedWhenCommandAddsNone(t *testing.T) {
	commands := [][]string{
		{"properties", "get"},
		{"permits", "search"},
		{"permits", "get"},
		{"decisions", "search"},
		{"contractors", "get"},
		{"cities", "coverage"},
		{"tags", "list"},
	}

	for _, args := range commands {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var raw map[string]any
			stdout := mustSchema(t, nil, args...)
			if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
				t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
			}
			if _, ok := raw["meta_fields"]; ok {
				t.Errorf("schema output should omit meta_fields entirely, got %v", raw["meta_fields"])
			}
		})
	}
}

// TestSchemaPropertiesGetSharesSearchResponseFields verifies both properties
// commands describe the same row shape: one OpenAPI schema serves both
// endpoints, so an agent can read either schema for the field set.
func TestSchemaPropertiesGetSharesSearchResponseFields(t *testing.T) {
	search := parseSchema(t, mustSchema(t, nil, "properties", "search"))
	get := parseSchema(t, mustSchema(t, nil, "properties", "get"))

	for name := range search.ResponseFields {
		if _, ok := get.ResponseFields[name]; !ok {
			t.Errorf("properties get missing response field %q present on search", name)
		}
	}
	for name := range get.ResponseFields {
		if _, ok := search.ResponseFields[name]; !ok {
			t.Errorf("properties search missing response field %q present on get", name)
		}
	}
}

// TestSchemaPropertiesGetTrustFieldsMarkedSearchOnly verifies every trust path
// on the batch command states where trust is actually populated. The fields
// stay in the schema so the shape is discoverable from either command, which
// makes saying "not here" load-bearing.
func TestSchemaPropertiesGetTrustFieldsMarkedSearchOnly(t *testing.T) {
	const note = "search absence queries only — never returned by properties get"

	out := parseSchema(t, mustSchema(t, nil, "properties", "get"))

	for _, path := range append([]string{"trust"}, propertiesTrustChildren...) {
		desc := fieldDescription(t, out, path)
		if !strings.Contains(desc, note) {
			t.Errorf("properties get field %q should state %q, got: %s", path, note, desc)
		}
	}

	// The same paths on search must not carry the get-only disclaimer, which
	// would tell an agent absence data is unavailable where it is the point.
	searchOut := parseSchema(t, mustSchema(t, nil, "properties", "search"))
	for _, path := range append([]string{"trust"}, propertiesTrustChildren...) {
		if desc := fieldDescription(t, searchOut, path); strings.Contains(desc, note) {
			t.Errorf("properties search field %q should not carry the get-only note, got: %s", path, desc)
		}
	}
}

// TestSchemaPropertiesMoneyFieldsUseCents verifies the integer-cents fields
// and the market-value filters carry the unit, since a bare integer market
// value reads as dollars otherwise.
func TestSchemaPropertiesMoneyFieldsUseCents(t *testing.T) {
	for _, sub := range []string{"search", "get"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			out := parseSchema(t, mustSchema(t, nil, "properties", sub))
			for _, field := range []string{"total_job_value", "assess_market_value"} {
				if unit := fieldUnit(t, out, field); unit != "cents" {
					t.Errorf("field %q has unit %q, want cents", field, unit)
				}
			}
		})
	}

	search := parseSchema(t, mustSchema(t, nil, "properties", "search"))
	for _, filter := range []string{"--property-min-market-value", "--property-max-market-value"} {
		if unit := filterUnit(t, search, filter); unit != "cents" {
			t.Errorf("filter %q has unit %q, want cents", filter, unit)
		}
	}
}

// TestSchemaNestedExpansionIsOptIn verifies nested expansion and meta envelope
// arrays reach only the commands that ask for them: every other command's
// --schema prints a flat response_fields map, a field index whose entries stay
// one level under data[], and no meta entry beyond the endpoint ceiling. The
// ceiling is the one entry a command earns without asking, and
// TestSchemaCappedSearchDocumentsTheCapAndTheMissingCursor is what reads it.
func TestSchemaNestedExpansionIsOptIn(t *testing.T) {
	expanded := map[string]bool{"properties search": true, "properties get": true}

	for _, path := range registeredSchemaCommands(t) {
		if expanded[path] {
			continue
		}
		path := path
		t.Run(path, func(t *testing.T) {
			stdout := mustSchema(t, nil, strings.Split(path, " ")...)

			out := parseSchema(t, stdout)
			for name := range out.MetaFields {
				if name != schemaCapMetaKey {
					t.Errorf("meta field %q appeared on %q, which never opted in", name, path)
				}
			}
			for name := range out.ResponseFields {
				if strings.Contains(name, ".") {
					t.Errorf("nested field %q appeared on %q, which never opted in", name, path)
				}
			}
			for _, entry := range out.FieldIndex {
				if strings.Count(entry, ".") > 1 {
					t.Errorf("nested field_index entry %q appeared on %q, which never opted in", entry, path)
				}
			}
		})
	}
}

// TestSchemaNestedObjectRefsOutsidePropertiesStayNamed pins the specific
// fields that would flatten if expansion ever became global: a permit's
// address and geo_ids, and a contractor's address are object references just
// like a property's trust.
func TestSchemaNestedObjectRefsOutsidePropertiesStayNamed(t *testing.T) {
	cases := []struct {
		args     []string
		field    string
		wantType string
	}{
		{[]string{"permits", "search"}, "address", "AddressesRead"},
		{[]string{"permits", "search"}, "geo_ids", "GeoIdsRead"},
		{[]string{"permits", "get"}, "address", "AddressesRead"},
		{[]string{"permits", "get"}, "geo_ids", "GeoIdsRead"},
		{[]string{"contractors", "search"}, "address", "AddressesEmbedded"},
		{[]string{"contractors", "get"}, "address", "AddressesEmbedded"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(strings.Join(tc.args, " ")+" "+tc.field, func(t *testing.T) {
			out := parseSchema(t, mustSchema(t, nil, tc.args...))

			field, ok := out.ResponseFields[tc.field].(map[string]any)
			if !ok {
				t.Fatalf("response_fields missing %q", tc.field)
			}
			if got, _ := field["type"].(string); got != tc.wantType {
				t.Errorf("field %q has type %q, want the unexpanded %q", tc.field, got, tc.wantType)
			}
			for name := range out.ResponseFields {
				if strings.HasPrefix(name, tc.field+".") {
					t.Errorf("field %q was expanded into %q", tc.field, name)
				}
			}
		})
	}
}

// TestSchemaPropertiesSearchAdvertisesEveryFilter verifies the search schema
// documents exactly the flags the command registers. The names are spelled
// out here so renaming a flag fails this test rather than moving on both
// sides at once.
func TestSchemaPropertiesSearchAdvertisesEveryFilter(t *testing.T) {
	want := []string{
		"--geo-id", "--legal-owner",
		"--permit-tags", "--permit-status", "--permit-from", "--permit-tags-unfinaled",
		"--property-type",
		"--property-min-market-value", "--property-max-market-value",
		"--property-min-lot-size", "--property-max-lot-size",
		"--property-min-building-area", "--property-max-building-area",
		"--property-min-unit-count", "--property-max-unit-count",
		"--property-min-year-built", "--property-max-year-built",
		"--include-count",
		// No --permit-to: the API rejects permit_to and points callers at
		// permits search, so the extra-entry check below must keep it out.
	}

	out := parseSchema(t, mustSchema(t, nil, "properties", "search"))

	wanted := make(map[string]bool, len(want))
	for _, filter := range want {
		wanted[filter] = true
		if _, ok := out.Filters[filter]; !ok {
			t.Errorf("filters missing %q", filter)
		}
	}
	for filter := range out.Filters {
		if !wanted[filter] {
			t.Errorf("filters carry an undocumented extra entry %q", filter)
		}
	}
}

// TestSchemaPropertiesGetTakesPositionalIDs verifies the batch command's
// schema describes its positional argument rather than a flag.
func TestSchemaPropertiesGetTakesPositionalIDs(t *testing.T) {
	out := parseSchema(t, mustSchema(t, nil, "properties", "get"))

	if _, ok := out.Filters["ID"]; !ok {
		t.Errorf("properties get filters should document the positional ID, got %v", out.Filters)
	}
	assertBatchGetMeta(t, out)
}

// TestSchemaPropertiesSearchMetaConvention verifies the search schema keeps
// the paginated envelope convention even with its extra meta fields.
func TestSchemaPropertiesSearchMetaConvention(t *testing.T) {
	assertSearchMeta(t, parseSchema(t, mustSchema(t, nil, "properties", "search")))
}

// =======================================================================
// Capped searches: the pagination contract on the offline surface
// =======================================================================

// schemaCapMetaKey is the meta envelope key a capped search's schema documents
// its endpoint's ceiling under.
const schemaCapMetaKey = "server_capped"

// noCursorClause is the part of the disclosure that keeps a ceiling from
// reading as a cursor someone has exhausted.
const noCursorClause = "exposes no continuation cursor"

// capDescription returns the description of the cap entry a schema documents
// under meta.
func capDescription(t *testing.T, out schemaOutput) string {
	t.Helper()

	entry, ok := out.MetaFields[schemaCapMetaKey].(map[string]any)
	if !ok {
		t.Fatalf("%s meta_fields missing %q, got %v", out.Command, schemaCapMetaKey, out.MetaFields)
	}
	desc, ok := entry["description"].(string)
	if !ok {
		t.Fatalf("%s meta field %q has no description string", out.Command, schemaCapMetaKey)
	}
	return desc
}

// assertCapDisclosure verifies a schema publishes the given ceiling and says the
// endpoint has no cursor to follow past it. The ceiling alone leaves an agent
// free to read a has_more of false as a cursor it has exhausted and plan the
// loop anyway, so both halves are the disclosure.
func assertCapDisclosure(t *testing.T, out schemaOutput, ceiling int) {
	t.Helper()

	desc := capDescription(t, out)
	if want := fmt.Sprintf("at most %d records", ceiling); !strings.Contains(desc, want) {
		t.Errorf("%s cap description should state %q, got: %s", out.Command, want, desc)
	}
	if !strings.Contains(desc, noCursorClause) {
		t.Errorf("%s cap description should state the endpoint %s, got: %s", out.Command, noCursorClause, desc)
	}
}

// TestSchemaCappedSearchDocumentsTheCapAndTheMissingCursor verifies every capped
// search's --schema documents its endpoint's ceiling under meta, indexed as the
// envelope key the runtime emits, with the missing cursor stated alongside the
// number.
func TestSchemaCappedSearchDocumentsTheCapAndTheMissingCursor(t *testing.T) {
	for _, search := range cappedSearches() {
		search := search
		t.Run(search.name, func(t *testing.T) {
			out := parseSchema(t, mustSchema(t, nil, strings.Fields(search.name)...))

			assertCapDisclosure(t, out, search.cap)

			if entry := "meta." + schemaCapMetaKey; !fieldIndexSet(out)[entry] {
				t.Errorf("field_index missing %q", entry)
			}
		})
	}
}

// TestSchemaCursorPaginatedSearchDocumentsNoCap verifies a search that follows a
// cursor keeps the envelope an agent can page through: has_more indexed under
// meta, and no ceiling entry beside it to say the cursor cannot be followed.
func TestSchemaCursorPaginatedSearchDocumentsNoCap(t *testing.T) {
	out := parseSchema(t, mustSchema(t, nil, "permits", "search"))

	index := fieldIndexSet(out)

	if !index["meta.has_more"] {
		t.Error("permits search field_index should document meta.has_more")
	}
	if entry := "meta." + schemaCapMetaKey; index[entry] {
		t.Errorf("permits search field_index should not document %q", entry)
	}
	if _, ok := out.MetaFields[schemaCapMetaKey]; ok {
		t.Errorf("permits search meta_fields should not document %q, got %v", schemaCapMetaKey, out.MetaFields)
	}
}

// TestSchemaCappedSearchDisclosesCapWithoutAPIKey verifies the disclosure is
// reachable on the credit-free pre-flight it exists for: with no key configured
// the ceiling still prints and no endpoint is touched. The stub answers 500 so a
// request slipping through fails the command as well as the hit count.
func TestSchemaCappedSearchDisclosesCapWithoutAPIKey(t *testing.T) {
	for _, search := range cappedSearches() {
		search := search
		t.Run(search.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(500)
				w.Write([]byte(`{"detail":"schema output must not reach the API"}`))
			}))
			defer srv.Close()

			env := withIsolatedConfigNoAuth(t)
			args := append([]string{"--base-url", srv.URL}, strings.Fields(search.name)...)

			result := runCLIWithEnv(t, env, append(args, "--schema")...)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			assertCapDisclosure(t, parseSchema(t, result.Stdout), search.cap)

			if got := hits.Load(); got != 0 {
				t.Errorf("expected zero API requests, got %d", got)
			}
		})
	}
}

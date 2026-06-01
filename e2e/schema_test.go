//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// schemaOutput mirrors the JSON structure of schema command output.
type schemaOutput struct {
	SchemaVersion  int            `json:"schema_version"`
	Command        string         `json:"command"`
	ResponseFields map[string]any `json:"response_fields"`
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
	expected := []string{"permits search", "permits get", "contractors search", "tags list", "cities metrics current"}
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
	for _, resource := range []string{"permits", "decisions", "contractors"} {
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

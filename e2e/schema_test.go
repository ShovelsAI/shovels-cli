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

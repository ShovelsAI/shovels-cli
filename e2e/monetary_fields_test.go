//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// =======================================================================
// Happy paths
// =======================================================================

// TestPermitsSearchSchemaJobValueDescriptionContainsCents verifies that
// the job_value response field description includes unit information
// so agents can determine the unit from description text alone.
func TestPermitsSearchSchemaJobValueDescriptionContainsCents(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	assertFieldDescContains(t, out, "job_value", "in cents (divide by 100 for dollars)")
}

// TestPermitsSearchSchemaFeesDescriptionContainsCents verifies fees field.
func TestPermitsSearchSchemaFeesDescriptionContainsCents(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)
	assertFieldDescContains(t, out, "fees", "in cents (divide by 100 for dollars)")
}

// TestAllMonetaryFieldsHaveCentsInDescription verifies that every monetary
// response field across all command schemas contains the unit phrase in its
// description, so agents never need to consult the separate unit metadata.
func TestAllMonetaryFieldsHaveCentsInDescription(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	// All command paths that have monetary response fields.
	commands := []struct {
		args   []string
		fields []string
	}{
		{[]string{"permits", "search"}, []string{"job_value", "fees"}},
		{[]string{"permits", "get"}, []string{"job_value", "fees"}},
		{[]string{"contractors", "search"}, []string{"avg_job_value", "total_job_value"}},
		{[]string{"contractors", "get"}, []string{"avg_job_value", "total_job_value"}},
		{[]string{"contractors", "permits"}, []string{"job_value", "fees"}},
		{[]string{"contractors", "metrics"}, []string{"avg_job_value", "total_job_value"}},
		{[]string{"cities", "metrics", "current"}, []string{"total_job_value"}},
		{[]string{"cities", "metrics", "monthly"}, []string{"total_job_value"}},
		{[]string{"counties", "metrics", "current"}, []string{"total_job_value"}},
		{[]string{"counties", "metrics", "monthly"}, []string{"total_job_value"}},
		{[]string{"jurisdictions", "metrics", "current"}, []string{"total_job_value"}},
		{[]string{"jurisdictions", "metrics", "monthly"}, []string{"total_job_value"}},
		{[]string{"addresses", "metrics", "current"}, []string{"total_job_value"}},
		{[]string{"addresses", "metrics", "monthly"}, []string{"total_job_value"}},
	}

	for _, tc := range commands {
		cmdName := strings.Join(tc.args, " ")
		t.Run(cmdName, func(t *testing.T) {
			schemaArgs := append([]string{"schema"}, tc.args...)
			result := runCLIWithEnv(t, env, schemaArgs...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := parseSchema(t, result.Stdout)

			for _, field := range tc.fields {
				assertFieldDescContains(t, out, field, "in cents (divide by 100 for dollars)")
			}
		})
	}
}

// =======================================================================
// Edge cases
// =======================================================================

// TestMonetaryFieldsRetainUnitMetadata verifies that the unit: "cents"
// metadata field remains unchanged for programmatic consumers even after
// descriptions were enriched with unit text.
func TestMonetaryFieldsRetainUnitMetadata(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	for _, field := range []string{"job_value", "fees"} {
		raw, ok := out.ResponseFields[field]
		if !ok {
			t.Errorf("response_fields should contain %s", field)
			continue
		}

		fieldMap, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("field %s should be a JSON object", field)
			continue
		}

		unit, _ := fieldMap["unit"].(string)
		if unit != "cents" {
			t.Errorf("field %s should retain unit 'cents' metadata, got %q", field, unit)
		}
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

// TestNoMonetaryResponseFieldMissesUnitInDescription scans all schema
// outputs for any field with unit: "cents" whose description lacks the
// unit phrase, catching future regressions when new monetary fields are added.
func TestNoMonetaryResponseFieldMissesUnitInDescription(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	// Get all command paths from schema.
	listResult := runCLIWithEnv(t, env, "schema")
	if listResult.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", listResult.ExitCode, listResult.Stderr)
	}

	var paths []string
	if err := json.Unmarshal([]byte(listResult.Stdout), &paths); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			args := append([]string{"schema"}, strings.Fields(path)...)
			result := runCLIWithEnv(t, env, args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := parseSchema(t, result.Stdout)

			for fieldName, raw := range out.ResponseFields {
				fieldMap, ok := raw.(map[string]any)
				if !ok {
					continue
				}

				unit, _ := fieldMap["unit"].(string)
				if unit != "cents" {
					continue
				}

				desc, _ := fieldMap["description"].(string)
				if !strings.Contains(desc, "in cents (divide by 100 for dollars)") {
					t.Errorf("field %s has unit 'cents' but description lacks unit phrase: %q", fieldName, desc)
				}
			}
		})
	}
}

// assertFieldDescContains checks that a response field's description
// contains the expected substring.
func assertFieldDescContains(t *testing.T, schema schemaOutput, fieldName, substr string) {
	t.Helper()

	raw, ok := schema.ResponseFields[fieldName]
	if !ok {
		t.Errorf("response_fields should contain %s", fieldName)
		return
	}

	fieldMap, ok := raw.(map[string]any)
	if !ok {
		t.Errorf("field %s should be a JSON object", fieldName)
		return
	}

	desc, _ := fieldMap["description"].(string)
	if !strings.Contains(desc, substr) {
		t.Errorf("field %s description should contain %q, got %q", fieldName, substr, desc)
	}
}

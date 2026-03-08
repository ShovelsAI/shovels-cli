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

// TestPermitsSearchHelpShowsMonetaryFiltersInCents verifies that
// `shovels permits search --help` describes monetary flags using "cents"
// with a conversion example, not "dollars".
func TestPermitsSearchHelpShowsMonetaryFiltersInCents(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Each monetary flag's description line must say "in cents".
	// Description lines start with the flag name preceded by whitespace
	// (as opposed to example lines which contain the flag as a CLI argument).
	monetaryFlags := []string{
		"--min-job-value",
		"--min-fees",
		"--property-min-market-value",
		"--contractor-min-total-job-value",
	}

	for _, flag := range monetaryFlags {
		found := false
		for _, line := range strings.Split(out, "\n") {
			trimmed := strings.TrimSpace(line)
			// Flag description lines start with the flag name.
			if !strings.HasPrefix(trimmed, flag) {
				continue
			}
			found = true
			if !strings.Contains(line, "in cents") {
				t.Errorf("flag %s description should contain 'in cents', got %q", flag, trimmed)
			}
		}
		if !found {
			t.Errorf("help should contain flag description for %s", flag)
		}
	}

	// None should say "in dollars".
	if strings.Contains(out, "in dollars") {
		t.Error("help text should not contain 'in dollars' for monetary flags")
	}
}

// TestContractorsSearchSchemaShowsMonetaryFiltersInCents verifies that
// `shovels contractors search --schema` describes monetary filters using
// "cents" with unit metadata.
func TestContractorsSearchSchemaShowsMonetaryFiltersInCents(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "contractors", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	monetaryFilters := []string{
		"--min-job-value",
		"--min-fees",
		"--property-min-market-value",
		"--contractor-min-total-job-value",
	}

	for _, name := range monetaryFilters {
		raw, ok := out.Filters[name]
		if !ok {
			t.Errorf("schema should contain filter %s", name)
			continue
		}

		filterMap, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("filter %s should be a JSON object", name)
			continue
		}

		desc, _ := filterMap["description"].(string)
		if !strings.Contains(desc, "in cents") {
			t.Errorf("filter %s description should say 'in cents', got %q", name, desc)
		}

		unit, _ := filterMap["unit"].(string)
		if unit != "cents" {
			t.Errorf("filter %s should have unit 'cents', got %q", name, unit)
		}
	}
}

// TestPermitsSearchHelpExampleAnnotatesMonetaryValue verifies that
// help text examples with monetary filter values include a unit annotation.
func TestPermitsSearchHelpExampleAnnotatesMonetaryValue(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// The example using --min-job-value should use a cents-scale value
	// and the annotation should include a dollar-equivalent conversion.
	if !strings.Contains(out, "--min-job-value 5000000") {
		t.Error("help example should use --min-job-value 5000000 (cents), not 50000 (dollars)")
	}
	if !strings.Contains(out, "in cents") {
		t.Error("help example with monetary filter should be annotated with 'in cents'")
	}
	if !strings.Contains(out, "= $50,000") {
		t.Error("help example should include dollar-equivalent annotation (e.g. '5000000 = $50,000')")
	}
}

// =======================================================================
// Edge cases
// =======================================================================

// TestContractorsSearchSchemaNoTalliesWarning verifies that the --no-tallies
// filter description warns that tallies are the only search-filtered fields.
func TestContractorsSearchSchemaNoTalliesWarning(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "contractors", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	raw, ok := out.Filters["--no-tallies"]
	if !ok {
		t.Fatal("contractors search schema should contain --no-tallies filter")
	}

	filterMap, ok := raw.(map[string]any)
	if !ok {
		t.Fatal("--no-tallies filter should be a JSON object")
	}

	desc, _ := filterMap["description"].(string)
	if !strings.Contains(strings.ToLower(desc), "tallies are the only") {
		t.Errorf("--no-tallies description should warn tallies are the only filtered fields, got %q", desc)
	}
}

// TestPermitsSearchSchemaDoesNotHaveNoTallies verifies that --no-tallies
// does NOT appear in the permits search schema (it's contractors-only).
func TestPermitsSearchSchemaDoesNotHaveNoTallies(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "permits", "search", "--schema")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseSchema(t, result.Stdout)

	if _, ok := out.Filters["--no-tallies"]; ok {
		t.Error("permits search schema should NOT contain --no-tallies (contractors-only)")
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

// TestNoMonetaryFilterSaysDollarsInSearchflags verifies that zero
// monetary filter descriptions in the help text say "in dollars".
func TestNoMonetaryFilterSaysDollarsInSearchflags(t *testing.T) {
	commands := [][]string{
		{"permits", "search", "--help"},
		{"contractors", "search", "--help"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			result := runCLI(t, args...)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d", result.ExitCode)
			}
			if strings.Contains(result.Stdout, "in dollars") {
				t.Errorf("%s should not contain 'in dollars'", strings.Join(args, " "))
			}
		})
	}
}

// TestNoMonetaryFilterSaysDollarsInSchema verifies that zero monetary
// filter descriptions in schema output say "in dollars".
func TestNoMonetaryFilterSaysDollarsInSchema(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	commands := [][]string{
		{"permits", "search", "--schema"},
		{"contractors", "search", "--schema"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			result := runCLIWithEnv(t, env, args...)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d", result.ExitCode)
			}

			// Parse the raw JSON and check all filter descriptions.
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(result.Stdout), &raw); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}

			if strings.Contains(string(raw["filters"]), "in dollars") {
				t.Error("schema filters should not contain 'in dollars'")
			}
		})
	}
}

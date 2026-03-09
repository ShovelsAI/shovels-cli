//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// allValidClassifications lists every API-accepted contractor classification
// in the canonical order matching validClassifications in searchflags.go.
var allValidClassifications = []string{
	"concrete_and_paving", "demolition_and_excavation", "electrical",
	"fencing_and_glazing", "framing_and_carpentry", "general_building_contractor",
	"general_engineering_contractor", "hvac", "landscaping_and_outdoor_work",
	"other", "plumbing", "roofing", "specialty_trades",
}

// --- Happy paths ---

func TestClassificationAllThirteenValuesAccepted(t *testing.T) {
	for _, cls := range allValidClassifications {
		t.Run(cls, func(t *testing.T) {
			handler, queries := makeContractorSearchHandler(2, 2, 9998)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"contractors", "search",
				"--geo-id", "90210",
				"--permit-from", "2024-01-01",
				"--permit-to", "2024-12-31",
				"--contractor-classification", cls,
			)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0 for classification %q, got %d; stderr: %s", cls, result.ExitCode, result.Stderr)
			}

			if len(*queries) < 1 {
				t.Fatal("expected at least 1 API request")
			}
			q := (*queries)[0]
			vals := q["contractor_classification_derived"]
			if len(vals) != 1 || vals[0] != cls {
				t.Errorf("expected contractor_classification_derived=[%s], got %v", cls, vals)
			}
		})
	}
}

func TestClassificationCommaSeparatedAccepted(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "electrical,plumbing",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	vals := q["contractor_classification_derived"]
	if len(vals) != 2 {
		t.Fatalf("expected 2 classification values, got %d: %v", len(vals), vals)
	}
	if vals[0] != "electrical" || vals[1] != "plumbing" {
		t.Errorf("expected [electrical, plumbing], got %v", vals)
	}
}

func TestClassificationExclusionPrefixAccepted(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "-electrical",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	vals := q["contractor_classification_derived"]
	if len(vals) != 1 || vals[0] != "-electrical" {
		t.Errorf("expected contractor_classification_derived=[-electrical] (preserved), got %v", vals)
	}
}

func TestClassificationHelpListsAllThirteenValues(t *testing.T) {
	commands := [][]string{
		{"permits", "search", "--help"},
		{"contractors", "search", "--help"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			result := runCLI(t, args...)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			for _, cls := range allValidClassifications {
				if !strings.Contains(result.Stdout, cls) {
					t.Errorf("%s --help should list classification %q", strings.Join(args[:len(args)-1], " "), cls)
				}
			}
		})
	}
}

func TestClassificationSchemaListsAllThirteenValues(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	commands := [][]string{
		{"permits", "search", "--schema"},
		{"contractors", "search", "--schema"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			result := runCLIWithEnv(t, env, args...)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			var schema map[string]any
			if err := json.Unmarshal([]byte(result.Stdout), &schema); err != nil {
				t.Fatalf("stdout is not valid JSON: %v", err)
			}

			filters, ok := schema["filters"].(map[string]any)
			if !ok {
				t.Fatal("schema missing filters object")
			}

			ccFilter, ok := filters["--contractor-classification"].(map[string]any)
			if !ok {
				t.Fatal("schema filters missing --contractor-classification")
			}

			desc, ok := ccFilter["description"].(string)
			if !ok {
				t.Fatal("--contractor-classification filter missing description")
			}

			for _, cls := range allValidClassifications {
				if !strings.Contains(desc, cls) {
					t.Errorf("--contractor-classification schema description should contain %q, got: %s", cls, desc)
				}
			}
		})
	}
}

// --- Edge cases ---

func TestClassificationMixedIncludeExcludeAccepted(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "-electrical,plumbing",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	vals := q["contractor_classification_derived"]
	if len(vals) != 2 {
		t.Fatalf("expected 2 classification values, got %d: %v", len(vals), vals)
	}
	if vals[0] != "-electrical" {
		t.Errorf("expected first value to be \"-electrical\" (preserved with dash), got %q", vals[0])
	}
	if vals[1] != "plumbing" {
		t.Errorf("expected second value to be \"plumbing\", got %q", vals[1])
	}
}

func TestClassificationGeneralBuildingContractorExactMatch(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "general_building_contractor",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	vals := q["contractor_classification_derived"]
	if len(vals) != 1 || vals[0] != "general_building_contractor" {
		t.Errorf("expected contractor_classification_derived=[general_building_contractor], got %v", vals)
	}
}

// --- Error conditions ---

func TestClassificationInvalidValueErrors(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "bogus",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, `"bogus"`) {
		t.Errorf("expected error to mention the invalid value, got: %s", p.Error)
	}
	for _, cls := range allValidClassifications {
		if !strings.Contains(p.Error, cls) {
			t.Errorf("expected error to list valid option %q, got: %s", cls, p.Error)
		}
	}
}

func TestClassificationOneValidOneInvalidRejectsOnInvalid(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "electrical,bogus",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, `"bogus"`) {
		t.Errorf("expected error to mention the invalid value \"bogus\", got: %s", p.Error)
	}
	for _, cls := range allValidClassifications {
		if !strings.Contains(p.Error, cls) {
			t.Errorf("expected error to list valid option %q, got: %s", cls, p.Error)
		}
	}
}

// --- Boundary conditions ---

func TestClassificationOldGeneralBuildingRejectsNow(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "general_building",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for old value \"general_building\", got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, `"general_building"`) {
		t.Errorf("expected error to mention the invalid value, got: %s", p.Error)
	}
}

func TestClassificationExclusionPrefixOnInvalidStillRejects(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "-bogus",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for \"-bogus\", got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
}

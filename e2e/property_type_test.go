//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// allValidPropertyTypes lists every API-accepted property type in the canonical order.
var allValidPropertyTypes = []string{
	"residential", "commercial", "industrial",
	"agricultural", "vacant land", "exempt",
	"miscellaneous", "office", "recreational",
}

// --- Happy paths ---

func TestPropertyTypeResidentialAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "residential",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "residential" {
		t.Errorf("expected property_type=residential, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeCommercialAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "commercial",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "commercial" {
		t.Errorf("expected property_type=commercial, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeIndustrialAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "industrial",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "industrial" {
		t.Errorf("expected property_type=industrial, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeAgriculturalAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "agricultural",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "agricultural" {
		t.Errorf("expected property_type=agricultural, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeVacantLandAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "vacant land",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "vacant land" {
		t.Errorf("expected property_type=\"vacant land\", got %q", q["property_type"][0])
	}
}

func TestPropertyTypeExemptAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "exempt",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "exempt" {
		t.Errorf("expected property_type=exempt, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeMiscellaneousAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "miscellaneous",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "miscellaneous" {
		t.Errorf("expected property_type=miscellaneous, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeOfficeAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "office",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "office" {
		t.Errorf("expected property_type=office, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeRecreationalAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "recreational",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["property_type"][0] != "recreational" {
		t.Errorf("expected property_type=recreational, got %q", q["property_type"][0])
	}
}

func TestPropertyTypeSchemaListsAll9Values(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	// Verify schema output for commands with --property-type filter.
	commands := [][]string{
		{"permits", "search", "--schema"},
		{"contractors", "search", "--schema"},
		{"cities", "metrics", "current", "--schema"},
		{"cities", "metrics", "monthly", "--schema"},
		{"counties", "metrics", "current", "--schema"},
		{"counties", "metrics", "monthly", "--schema"},
		{"jurisdictions", "metrics", "current", "--schema"},
		{"jurisdictions", "metrics", "monthly", "--schema"},
		{"contractors", "metrics", "--schema"},
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

			ptFilter, ok := filters["--property-type"].(map[string]any)
			if !ok {
				t.Fatal("schema filters missing --property-type")
			}

			desc, ok := ptFilter["description"].(string)
			if !ok {
				t.Fatal("--property-type filter missing description")
			}

			for _, pt := range allValidPropertyTypes {
				if !strings.Contains(desc, pt) {
					t.Errorf("--property-type schema description should contain %q, got: %s", pt, desc)
				}
			}
		})
	}
}

// --- Edge cases ---

func TestPropertyTypeOmittedProceeds(t *testing.T) {
	handler, queries := makePermitSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if _, exists := q["property_type"]; exists {
		t.Error("property_type should not be sent when --property-type is omitted")
	}
}

// --- Error conditions ---

func TestPropertyTypeInvalidValueErrors(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "bogus",
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
	for _, valid := range allValidPropertyTypes {
		if !strings.Contains(p.Error, valid) {
			t.Errorf("expected error to list valid option %q, got: %s", valid, p.Error)
		}
	}
}

// --- Boundary conditions ---

func TestPropertyTypeWrongCaseErrors(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "RESIDENTIAL",
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
	if !strings.Contains(p.Error, `"RESIDENTIAL"`) {
		t.Errorf("expected error to mention the invalid value, got: %s", p.Error)
	}
	for _, valid := range allValidPropertyTypes {
		if !strings.Contains(p.Error, valid) {
			t.Errorf("expected error to list valid option %q, got: %s", valid, p.Error)
		}
	}
}

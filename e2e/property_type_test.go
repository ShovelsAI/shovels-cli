//go:build e2e

package e2e

import (
	"net/http/httptest"
	"strings"
	"testing"
)

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
		"--property-type", "mansion",
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
	if !strings.Contains(p.Error, `"mansion"`) {
		t.Errorf("expected error to mention the invalid value, got: %s", p.Error)
	}
	for _, valid := range []string{"residential", "commercial", "industrial"} {
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
	for _, valid := range []string{"residential", "commercial", "industrial"} {
		if !strings.Contains(p.Error, valid) {
			t.Errorf("expected error to list valid option %q, got: %s", valid, p.Error)
		}
	}
}
